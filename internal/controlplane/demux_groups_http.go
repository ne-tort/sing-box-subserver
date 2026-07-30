//go:build with_controlplane

package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/demuxgroups"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

func (s *Service) handleDemuxGroupsList(w http.ResponseWriter, r *http.Request) {
	lang := requestLang(r)
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	out := make([]any, 0)
	for _, g := range demuxgroups.All() {
		if statusFilter != "" && !strings.EqualFold(g.Status, statusFilter) {
			continue
		}
		title, desc := g.ResolveI18n(lang)
		meta := demuxgroups.BuildGroupMatchMeta(g)
		slots := make([]any, 0, len(g.Slots))
		for _, slot := range g.Slots {
			slots = append(slots, demuxgroups.EnrichSlotAPI(slot))
		}
		out = append(out, map[string]any{
			"tag":                 g.Tag,
			"short_name":          g.ShortName,
			"status":              g.Status,
			"title":               title,
			"description":         desc,
			"suggested_port":      g.SuggestedPort,
			"networks":            g.Networks,
			"scores":              g.Scores,
			"slots":               slots,
			"slot_count":          len(g.Slots),
			"separation_summary":  meta.SeparationSummary,
		})
	}
	okJSONETag(w, r, out)
}

func (s *Service) handleDemuxGroupsGet(w http.ResponseWriter, r *http.Request) {
	lang := requestLang(r)
	g, err := demuxgroups.Get(r.PathValue("tag"))
	if err != nil {
		failJSON(w, 404, "not_found", err.Error())
		return
	}
	title, desc := g.ResolveI18n(lang)
	meta := demuxgroups.BuildGroupMatchMeta(g)
	slots := make([]any, 0, len(g.Slots))
	for _, slot := range g.Slots {
		slots = append(slots, demuxgroups.EnrichSlotAPI(slot))
	}
	okJSONETag(w, r, map[string]any{
		"tag":                g.Tag,
		"short_name":         g.ShortName,
		"status":             g.Status,
		"title":              title,
		"description":        desc,
		"suggested_port":     g.SuggestedPort,
		"networks":           g.Networks,
		"scores":             g.Scores,
		"notes":              g.Notes,
		"slots":              slots,
		"i18n":               g.I18n,
		"separation_summary": meta.SeparationSummary,
		"match_plan":         meta.Plan,
	})
}

func (s *Service) handleDemuxGroupsSubstitutions(w http.ResponseWriter, r *http.Request) {
	view, err := demuxgroups.Substitutions(r.PathValue("tag"), requestLang(r))
	if err != nil {
		failJSON(w, 404, "not_found", err.Error())
		return
	}
	okJSONETag(w, r, view)
}

// POST /v1/controlplane/sets/from-demux-group
// Body: demuxgroups.InstallRequest + optional activate/replace bool
func (s *Service) handleSetsFromDemuxGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		demuxgroups.InstallRequest
		Activate bool `json:"activate"`
		Replace  bool `json:"replace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if body.ListenPort == 0 {
		g, gerr := demuxgroups.Get(body.GroupTag)
		if gerr != nil {
			failJSON(w, 404, "not_found", gerr.Error())
			return
		}
		port, perr := suggestListenPort(sets, g.Networks)
		if perr != nil {
			failJSON(w, 409, "cp_port_exhausted", perr.Error())
			return
		}
		body.ListenPort = port
	}
	setName := strings.TrimSpace(body.SetName)
	if setName == "" {
		if g, gerr := demuxgroups.Get(body.GroupTag); gerr == nil {
			setName = g.Tag
		}
	}
	body.SetName = setName
	sets, err = s.replaceOrConflictSet(r.Context(), sets, setName, body.Replace)
	if err != nil {
		code, ec := 409, "cp_name_conflict"
		if strings.Contains(err.Error(), "cp_conflict_active") {
			ec = "cp_conflict_active"
		}
		failJSON(w, code, ec, err.Error())
		return
	}
	used := collectUsedPorts(sets)
	if hub, err := s.store.LoadWgHub(); err == nil && hub.ListenPort != 0 {
		used[hub.ListenPort] = struct{}{}
	}
	res, err := demuxgroups.BuildInstall(body.InstallRequest, used)
	if err != nil {
		code, ec := 400, "cp_invalid_demux_group"
		msg := err.Error()
		if strings.Contains(msg, "unknown demux group") {
			code, ec = 404, "not_found"
		}
		if strings.Contains(msg, "cp_invalid_slot") {
			ec = "cp_invalid_slot"
		}
		if strings.Contains(msg, "cp_port_exhausted") {
			code, ec = 409, "cp_port_exhausted"
		}
		failJSON(w, code, ec, msg)
		return
	}
	set := res.Set
	syncBindingSNI(&set)
	now := time.Now().UTC()
	set.CreatedAt = now
	set.UpdatedAt = now
	if err := s.validateSet(set, sets); err != nil {
		code, ec := validateSetHTTP(err)
		failJSON(w, code, ec, err.Error())
		return
	}
	sets = append(sets, set)
	if err := s.store.SaveSets(sets); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	resp := map[string]any{
		"set":          s.setPublicView(set, false),
		"member_ports": res.MemberPorts,
		"slot_snis":    res.SlotSNIs,
		"warnings":     res.Warnings,
		"activated":    false,
		"set_persisted": true,
	}
	if body.Activate {
		if err := s.activateSetByName(r.Context(), set.Name); err != nil {
			failJSONData(w, 422, activateErrorCode(err), fmt.Sprintf("set %q saved but activate failed: %v", set.Name, err), map[string]any{
				"set_persisted":       true,
				"dataplane_unchanged": true,
				"activated_sets":      []string{},
				"failed_at":           set.Name,
			})
			return
		}
		resp["activated"] = true
		resp["set"] = s.setPublicView(set, true)
	}
	okJSON(w, 201, resp)
}

// replaceOrConflictSet removes an existing set with the same name when replace=true.
// Active sets must be deactivated first (or replace deactivates them).
func (s *Service) replaceOrConflictSet(ctx context.Context, sets []domain.InboundSet, name string, replace bool) ([]domain.InboundSet, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return sets, nil
	}
	idx := -1
	for i := range sets {
		if sets[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return sets, nil
	}
	if !replace {
		return sets, fmt.Errorf("set name exists")
	}
	st, err := s.store.LoadState()
	if err != nil {
		return sets, err
	}
	if contains(st.ActiveSets, name) {
		if err := s.deactivateSetByName(ctx, name); err != nil {
			return sets, fmt.Errorf("cp_conflict_active: deactivate existing set before replace: %w", err)
		}
	}
	out := make([]domain.InboundSet, 0, len(sets)-1)
	for i, set := range sets {
		if i == idx {
			continue
		}
		out = append(out, set)
	}
	if err := s.store.SaveSets(out); err != nil {
		return sets, err
	}
	return out, nil
}

// POST /v1/controlplane/sets/from-presets — install one or more single-inbound sets with port policy.
func (s *Service) handleSetsFromPresets(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []struct {
			Name       string            `json:"name"`
			Preset     string            `json:"preset"`
			ListenPort uint16            `json:"listen_port"`
			Listen     string            `json:"listen,omitempty"`
			Params     map[string]string `json:"params,omitempty"`
		} `json:"items"`
		Activate bool `json:"activate"`
		Replace  bool `json:"replace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if len(body.Items) == 0 {
		failJSON(w, 400, "bad_request", "items required")
		return
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	created := make([]domain.InboundSet, 0, len(body.Items))
	now := time.Now().UTC()
	for i, it := range body.Items {
		preset := strings.TrimSpace(it.Preset)
		name := strings.TrimSpace(it.Name)
		if name == "" {
			name = preset
		}
		if name == "" || preset == "" {
			failJSON(w, 400, "bad_request", "each item needs preset (and optional name)")
			return
		}
		var rerr error
		sets, rerr = s.replaceOrConflictSet(r.Context(), sets, name, body.Replace)
		if rerr != nil {
			code, ec := 409, "cp_name_conflict"
			if strings.Contains(rerr.Error(), "cp_conflict_active") {
				ec = "cp_conflict_active"
			}
			failJSON(w, code, ec, fmt.Sprintf("items[%d]: %v", i, rerr))
			return
		}
		for _, o := range created {
			if o.Name == name {
				failJSON(w, 409, "cp_name_conflict", fmt.Sprintf("items[%d]: set name exists", i))
				return
			}
		}
		listenPort := it.ListenPort
		if listenPort == 0 {
			pmeta, err := presets.Get(preset)
			if err != nil {
				failJSON(w, 400, "cp_unknown_preset", fmt.Sprintf("items[%d]: %v", i, err))
				return
			}
			need := networksFromTraits(pmeta.Traits)
			others := append(append([]domain.InboundSet{}, sets...), created...)
			if hub, err := s.store.LoadWgHub(); err == nil && hub.ListenPort != 0 {
				others = append(others, domain.InboundSet{
					Name: "__wg_hub__", ListenPort: hub.ListenPort,
					DemuxTemplate: map[string]any{"network": []any{"udp"}},
				})
			}
			picked, err := suggestListenPort(others, need)
			if err != nil {
				failJSON(w, 409, "cp_port_exhausted", fmt.Sprintf("items[%d]: %v", i, err))
				return
			}
			listenPort = picked
		}
		listen := strings.TrimSpace(it.Listen)
		if listen == "" {
			listen = "::"
		}
		set := domain.InboundSet{
			Name:       name,
			Listen:     listen,
			ListenPort: listenPort,
			Presets:    []string{preset},
			Bindings:   []domain.SetBinding{{Preset: preset, Params: it.Params}},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		syncBindingSNI(&set)
		others := append(append([]domain.InboundSet{}, sets...), created...)
		if err := s.validateSet(set, others); err != nil {
			code, ec := validateSetHTTP(err)
			failJSON(w, code, ec, fmt.Sprintf("items[%d]: %v", i, err))
			return
		}
		created = append(created, set)
	}
	sets = append(sets, created...)
	if err := s.store.SaveSets(sets); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	views := make([]any, 0, len(created))
	for _, set := range created {
		views = append(views, s.setPublicView(set, false))
	}
	resp := map[string]any{
		"sets":           views,
		"activated":      false,
		"activated_sets": []string{},
		"set_persisted":  true,
	}
	if body.Activate {
		activated := make([]string, 0, len(created))
		for _, set := range created {
			if err := s.activateSetByName(r.Context(), set.Name); err != nil {
				for _, name := range activated {
					_ = s.deactivateSetByName(r.Context(), name)
				}
				failJSONData(w, 422, activateErrorCode(err), fmt.Sprintf("sets saved (%d); activate failed at %q; rolled back %d active: %v", len(created), set.Name, len(activated), err), map[string]any{
					"set_persisted":       true,
					"dataplane_unchanged": true,
					"activated_sets":      []string{},
					"failed_at":           set.Name,
					"rolled_back":         activated,
				})
				return
			}
			activated = append(activated, set.Name)
		}
		activeViews := make([]any, 0, len(created))
		for _, set := range created {
			activeViews = append(activeViews, s.setPublicView(set, true))
		}
		resp["sets"] = activeViews
		resp["activated_sets"] = activated
		resp["activated"] = true
	}
	okJSON(w, 201, resp)
}

// activateSetByName mirrors handleSetsActivate without HTTP.
func (s *Service) activateSetByName(ctx context.Context, name string) error {
	sets, err := s.store.LoadSets()
	if err != nil {
		return err
	}
	var found *domain.InboundSet
	for i := range sets {
		if sets[i].Name == name {
			found = &sets[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("set %q not found", name)
	}
	if found.HasDemux() && !demuxInBinary {
		return fmt.Errorf("demux set requires binary built with with_demux")
	}
	wasOwner := s.cfg.Owner != nil && s.cfg.Owner.Owner() == configowner.ModeControlplane
	if s.cfg.Owner != nil {
		if err := s.claimOwnership(configowner.ModeControlplane, "activate", name); err != nil {
			return err
		}
	}
	s.scrubStaleActiveSets()
	st, err := s.store.LoadState()
	if err != nil {
		s.rollbackFirstActivate(wasOwner, nil, name)
		return err
	}
	prev := append([]string{}, st.ActiveSets...)
	if !contains(st.ActiveSets, name) {
		st.ActiveSets = append(st.ActiveSets, name)
		if err := s.store.SaveState(st); err != nil {
			s.rollbackFirstActivate(wasOwner, prev, name)
			return err
		}
	}
	if err := s.rematerialize(ctx); err != nil {
		if cur, loadErr := s.store.LoadState(); loadErr == nil {
			cur.ActiveSets = prev
			_ = s.store.SaveState(cur)
		}
		s.rollbackFirstActivate(wasOwner, prev, name)
		return err
	}
	return nil
}

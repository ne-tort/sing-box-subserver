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
	out := make([]any, 0)
	for _, g := range demuxgroups.All() {
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
	okJSON(w, 200, out)
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
	okJSON(w, 200, map[string]any{
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
	view, err := demuxgroups.Substitutions(r.PathValue("tag"))
	if err != nil {
		failJSON(w, 404, "not_found", err.Error())
		return
	}
	okJSON(w, 200, view)
}

// POST /v1/controlplane/sets/from-demux-group
// Body: demuxgroups.InstallRequest + optional activate bool
func (s *Service) handleSetsFromDemuxGroup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		demuxgroups.InstallRequest
		Activate bool `json:"activate"`
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
	used := collectUsedPorts(sets)
	if hub, err := s.store.LoadWgHub(); err == nil && hub.ListenPort != 0 {
		used[hub.ListenPort] = struct{}{}
	}
	res, err := demuxgroups.BuildInstall(body.InstallRequest, used)
	if err != nil {
		code, ec := 400, "cp_invalid_demux_group"
		if strings.Contains(err.Error(), "unknown demux group") {
			code, ec = 404, "not_found"
		}
		failJSON(w, code, ec, err.Error())
		return
	}
	set := res.Set
	syncBindingSNI(&set)
	now := time.Now().UTC()
	set.CreatedAt = now
	set.UpdatedAt = now
	if err := s.validateSet(set, sets); err != nil {
		code, ec := 400, "cp_invalid_set"
		if strings.Contains(err.Error(), "cp_port_conflict") {
			code, ec = 409, "cp_port_conflict"
		}
		failJSON(w, code, ec, err.Error())
		return
	}
	sets = append(sets, set)
	if err := s.store.SaveSets(sets); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	resp := map[string]any{
		"set":          set,
		"member_ports": res.MemberPorts,
		"slot_snis":    res.SlotSNIs,
		"warnings":     res.Warnings,
		"activated":    false,
	}
	if body.Activate {
		if err := s.activateSetByName(r.Context(), set.Name); err != nil {
			resp["activate_error"] = err.Error()
			resp["activate_error_code"] = "cp_activate_failed"
			// Set is persisted; client must not treat install as live without activated=true.
			okJSON(w, 201, resp)
			return
		}
		resp["activated"] = true
	}
	okJSON(w, 201, resp)
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
				failJSON(w, 409, "cp_port_conflict", fmt.Sprintf("items[%d]: %v", i, err))
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
			code, ec := 400, "cp_invalid_set"
			if strings.Contains(err.Error(), "cp_port_conflict") {
				code, ec = 409, "cp_port_conflict"
			}
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
	// activated is always bool (same contract as from-demux-group); activated_sets lists successes.
	resp := map[string]any{"sets": created, "activated": false, "activated_sets": []string{}}
	if body.Activate {
		activated := make([]string, 0, len(created))
		for _, set := range created {
			if err := s.activateSetByName(r.Context(), set.Name); err != nil {
				resp["activate_error"] = err.Error()
				resp["activate_error_code"] = "cp_activate_failed"
				resp["activated_sets"] = activated
				resp["activated"] = false
				okJSON(w, 201, resp)
				return
			}
			activated = append(activated, set.Name)
		}
		resp["activated_sets"] = activated
		resp["activated"] = len(activated) == len(created)
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

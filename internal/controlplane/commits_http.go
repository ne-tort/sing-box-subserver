//go:build with_controlplane

package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/demuxgroups"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
)

func canonicalSHA256(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func headsDigest(h domain.HeadsFile) string {
	ids := make([]string, 0, len(h.Blocks))
	for id := range h.Blocks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id+":"+h.Blocks[id].SHA256)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func newCommitID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "c_" + hex.EncodeToString(b[:])
}

func (s *Service) handleHeadsGet(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.LoadHeads()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	st, _ := s.store.LoadState()
	if h.MaterializeSHA256 == "" {
		h.MaterializeSHA256 = st.LastMaterializeSHA256
	}
	meta, _ := s.store.LoadCommitMeta()
	okJSONETag(w, r, map[string]any{
		"blocks":              h.Blocks,
		"materialize_sha256":  h.MaterializeSHA256,
		"pending_commit_id":   nullIfEmpty(meta.PendingID),
		"heads_digest":        headsDigest(h),
	})
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Service) handleCommitsList(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	list, err := s.store.ListRecentCommits(limit)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{"commits": list})
}

func (s *Service) handleCommitsGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	c, err := s.store.LoadCommit(id)
	if errors.Is(err, store.ErrNotFound) {
		failJSON(w, 404, "not_found", "commit not found")
		return
	}
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, c)
}

func (s *Service) handleCommitsPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string                        `json:"source"`
		Base   *domain.CommitBase            `json:"base"`
		Blocks map[string]domain.CommitBlock `json:"blocks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if len(body.Blocks) == 0 {
		failJSON(w, 400, "bad_request", "blocks required")
		return
	}

	heads, err := s.store.LoadHeads()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	st, _ := s.store.LoadState()
	if heads.MaterializeSHA256 == "" {
		heads.MaterializeSHA256 = st.LastMaterializeSHA256
	}

	meta, err := s.store.LoadCommitMeta()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if meta.PendingID != "" {
		failJSONData(w, 409, "apply_in_progress", "another commit is applying", map[string]any{
			"pending_commit_id": meta.PendingID,
			"heads": map[string]any{
				"blocks":             heads.Blocks,
				"materialize_sha256": heads.MaterializeSHA256,
			},
		})
		return
	}

	if body.Base != nil {
		if body.Base.MaterializeSHA256 != "" && heads.MaterializeSHA256 != "" &&
			body.Base.MaterializeSHA256 != heads.MaterializeSHA256 {
			failJSONData(w, 409, "commit_conflict", "materialize sha mismatch", map[string]any{
				"heads": map[string]any{
					"blocks":             heads.Blocks,
					"materialize_sha256": heads.MaterializeSHA256,
				},
			})
			return
		}
		for id, want := range body.Base.Blocks {
			got, ok := heads.Blocks[id]
			if want == "" {
				if ok {
					failJSONData(w, 409, "commit_conflict", fmt.Sprintf("block %q unexpectedly present", id), map[string]any{
						"heads": map[string]any{"blocks": heads.Blocks, "materialize_sha256": heads.MaterializeSHA256},
					})
					return
				}
				continue
			}
			if !ok || got.SHA256 != want {
				failJSONData(w, 409, "commit_conflict", fmt.Sprintf("block %q sha mismatch", id), map[string]any{
					"heads": map[string]any{"blocks": heads.Blocks, "materialize_sha256": heads.MaterializeSHA256},
				})
				return
			}
		}
	}

	normalized := make(map[string]domain.CommitBlock, len(body.Blocks))
	blockShas := make(map[string]string, len(body.Blocks))
	for id, blk := range body.Blocks {
		id = strings.TrimSpace(id)
		if id == "" || blk.Body == nil {
			failJSON(w, 400, "bad_request", "each block needs id and body")
			return
		}
		sum, err := canonicalSHA256(blk.Body)
		if err != nil {
			failJSON(w, 400, "bad_request", err.Error())
			return
		}
		if blk.SHA256 != "" && !strings.EqualFold(blk.SHA256, sum) {
			failJSON(w, 400, "bad_block_hash", fmt.Sprintf("block %q hash mismatch", id))
			return
		}
		blk.SHA256 = sum
		normalized[id] = blk
		blockShas[id] = sum
	}

	ctx := r.Context()
	for id, blk := range normalized {
		if err := s.persistCommitBlock(ctx, id, blk.Body); err != nil {
			code, ec := 400, "bad_request"
			msg := err.Error()
			switch {
			case strings.Contains(msg, "not_found"):
				code, ec = 404, "not_found"
			case strings.Contains(msg, "cp_"):
				code, ec = 400, firstToken(msg)
			case strings.Contains(msg, "conflict"):
				code, ec = 409, "cp_name_conflict"
			}
			failJSON(w, code, ec, fmt.Sprintf("%s: %v", id, err))
			return
		}
	}

	now := time.Now().UTC()
	if heads.Blocks == nil {
		heads.Blocks = map[string]domain.BlockHead{}
	}
	for id, blk := range normalized {
		if isTombstone(blk.Body) {
			delete(heads.Blocks, id)
		} else {
			heads.Blocks[id] = domain.BlockHead{SHA256: blk.SHA256, UpdatedAt: now}
		}
	}
	if err := s.store.SaveHeads(heads); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}

	id := newCommitID()
	src := strings.TrimSpace(body.Source)
	if src == "" {
		src = "client"
	}
	stSnap, _ := s.store.LoadState()
	commit := domain.Commit{
		ID:             id,
		Status:         domain.CommitAccepted,
		CreatedAt:      now,
		Source:         src,
		Base:           body.Base,
		Blocks:         normalized,
		PrevActiveSets: append([]string{}, stSnap.ActiveSets...),
	}
	if err := s.store.SaveCommit(commit); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	meta.PendingID = id
	meta.Recent = prependRecent(meta.Recent, id, domain.MaxRecentCommits)
	if err := s.store.SaveCommitMeta(meta); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}

	okJSON(w, 202, map[string]any{
		"id":         id,
		"status":     domain.CommitAccepted,
		"block_shas": blockShas,
	})

	go s.runCommitApply(id)
}

func firstToken(msg string) string {
	if i := strings.IndexAny(msg, ": "); i > 0 {
		return msg[:i]
	}
	return msg
}

func prependRecent(recent []string, id string, max int) []string {
	out := make([]string, 0, max)
	out = append(out, id)
	for _, x := range recent {
		if x == id {
			continue
		}
		out = append(out, x)
		if len(out) >= max {
			break
		}
	}
	return out
}

func isTombstone(body map[string]any) bool {
	v, ok := body["deleted"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func (s *Service) runCommitApply(id string) {
	ctx := context.Background()
	c, err := s.store.LoadCommit(id)
	if err != nil {
		return
	}
	c.Status = domain.CommitApplying
	_ = s.store.SaveCommit(c)

	prev := append([]string{}, c.PrevActiveSets...)
	applyErr := s.activateCommitSets(ctx, c)
	if applyErr == nil {
		applyErr = s.rematerializeForce(ctx, true)
	}
	now := time.Now().UTC()
	result := &domain.CommitResult{FinishedAt: &now}
	if applyErr != nil {
		c.Status = domain.CommitFailed
		result.Error = applyErr.Error()
		result.ErrorCode = materializeErrorCode(applyErr)
		// Roll back active_sets to pre-commit snapshot.
		if cur, loadErr := s.store.LoadState(); loadErr == nil {
			cur.ActiveSets = prev
			_ = s.store.SaveState(cur)
		}
	} else {
		c.Status = domain.CommitApplied
		st, _ := s.store.LoadState()
		result.MaterializeSHA256 = st.LastMaterializeSHA256
		if s.cfg.Supervisor != nil {
			snap := s.cfg.Supervisor.Status()
			result.SupervisorRevision = snap.Revision
		}
		heads, _ := s.store.LoadHeads()
		heads.MaterializeSHA256 = st.LastMaterializeSHA256
		_ = s.store.SaveHeads(heads)
	}
	c.Result = result
	_ = s.store.SaveCommit(c)

	meta, _ := s.store.LoadCommitMeta()
	if meta.PendingID == id {
		meta.PendingID = ""
		_ = s.store.SaveCommitMeta(meta)
	}
}

// activateCommitSets updates ActiveSets from commit blocks without rematerialize.
// Non-tombstone demux/preset blocks are activated; tombstones are deactivated.
// Unrelated active sets are preserved.
func (s *Service) activateCommitSets(ctx context.Context, c domain.Commit) error {
	activate := map[string]bool{}
	deactivate := map[string]bool{}
	for id, blk := range c.Blocks {
		switch {
		case id == "demux":
			name := "cp-client"
			if v, ok := blk.Body["name"].(string); ok && strings.TrimSpace(v) != "" {
				name = strings.TrimSpace(v)
			}
			if isTombstone(blk.Body) {
				deactivate[name] = true
			} else {
				activate[name] = true
			}
		case strings.HasPrefix(id, "preset:"):
			tag := strings.TrimPrefix(id, "preset:")
			name := tag
			if v, ok := blk.Body["name"].(string); ok && strings.TrimSpace(v) != "" {
				name = strings.TrimSpace(v)
			}
			if isTombstone(blk.Body) {
				deactivate[name] = true
			} else {
				activate[name] = true
			}
		}
	}
	if len(activate) == 0 && len(deactivate) == 0 {
		return nil
	}
	// Claim ownership if we activate anything.
	if len(activate) > 0 && s.cfg.Owner != nil {
		if err := s.claimOwnership(configowner.ModeControlplane, "commit", c.ID); err != nil {
			return err
		}
	}
	st, err := s.store.LoadState()
	if err != nil {
		return err
	}
	next := make([]string, 0, len(st.ActiveSets)+len(activate))
	for _, name := range st.ActiveSets {
		if deactivate[name] {
			continue
		}
		next = append(next, name)
	}
	for name := range activate {
		if contains(next, name) {
			continue
		}
		next = append(next, name)
	}
	st.ActiveSets = next
	if err := s.store.SaveState(st); err != nil {
		return err
	}
	if len(next) == 0 {
		hub, _ := s.store.LoadWgHub()
		if !hub.Enabled && s.cfg.Owner != nil {
			_ = s.claimOwnership(configowner.ModeIdle, "commit_clear", c.ID)
		}
	}
	_ = ctx
	return nil
}

func (s *Service) commitStatusProjection() map[string]any {
	heads, err := s.store.LoadHeads()
	if err != nil {
		return nil
	}
	st, _ := s.store.LoadState()
	if heads.MaterializeSHA256 == "" {
		heads.MaterializeSHA256 = st.LastMaterializeSHA256
	}
	meta, _ := s.store.LoadCommitMeta()
	return map[string]any{
		"pending_id":         nullIfEmpty(meta.PendingID),
		"heads_digest":       headsDigest(heads),
		"materialize_sha256": heads.MaterializeSHA256,
	}
}

// persistCommitBlock writes desired state for one block without rematerialize.
func (s *Service) persistCommitBlock(ctx context.Context, id string, body map[string]any) error {
	switch {
	case id == "demux":
		return s.persistDemuxBlock(ctx, body)
	case strings.HasPrefix(id, "preset:"):
		return s.persistPresetBlock(ctx, strings.TrimPrefix(id, "preset:"), body)
	case strings.HasPrefix(id, "ssl:"):
		return s.persistSSLBlock(strings.TrimPrefix(id, "ssl:"), body)
	case id == "reality":
		return s.persistRealityBlock(body)
	case id == "wg":
		return s.persistWgBlock(body)
	case id == "dns":
		return s.persistFragmentBlock(configFragmentDNS, body)
	case id == "route":
		return s.persistRouteBlock(body)
	case id == "outbounds":
		return s.persistFragmentBlock(configFragmentOutbounds, body)
	default:
		return fmt.Errorf("unknown block %q", id)
	}
}

func removeSetByName(sets []domain.InboundSet, name string) []domain.InboundSet {
	name = strings.TrimSpace(name)
	if name == "" {
		return sets
	}
	out := make([]domain.InboundSet, 0, len(sets))
	for _, set := range sets {
		if set.Name == name {
			continue
		}
		out = append(out, set)
	}
	return out
}

func (s *Service) persistDemuxBlock(ctx context.Context, body map[string]any) error {
	_ = ctx
	if isTombstone(body) {
		name := "cp-client"
		if v, ok := body["name"].(string); ok && strings.TrimSpace(v) != "" {
			name = strings.TrimSpace(v)
		}
		return s.deleteSetByName(name)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var req demuxgroups.InstallRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return err
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		return err
	}
	setName := strings.TrimSpace(req.SetName)
	if setName == "" {
		setName = "cp-client"
		req.SetName = setName
	}
	sets = removeSetByName(sets, setName)
	if req.ListenPort == 0 {
		g, gerr := demuxgroups.Get(req.GroupTag)
		if gerr != nil {
			return fmt.Errorf("not_found: %w", gerr)
		}
		port, perr := suggestListenPort(sets, g.Networks)
		if perr != nil {
			return perr
		}
		req.ListenPort = port
	}
	used := collectUsedPorts(sets)
	if hub, err := s.store.LoadWgHub(); err == nil && hub.ListenPort != 0 {
		used[hub.ListenPort] = struct{}{}
	}
	if len(req.SNIPool) == 0 {
		if rc, err := s.loadRealityConfig(); err == nil {
			pool := make([]string, 0, len(rc.Profiles))
			for _, ep := range rc.Profiles {
				if sn := strings.TrimSpace(ep.SNI); sn != "" {
					pool = append(pool, sn)
				}
			}
			req.SNIPool = pool
		}
	}
	res, err := demuxgroups.BuildInstall(req, used)
	if err != nil {
		return err
	}
	set := res.Set
	s.syncBindingSNI(&set)
	now := time.Now().UTC()
	set.CreatedAt = now
	set.UpdatedAt = now
	if err := s.validateSet(set, sets); err != nil {
		return err
	}
	sets = append(sets, set)
	return s.store.SaveSets(sets)
}

func (s *Service) persistPresetBlock(ctx context.Context, tag string, body map[string]any) error {
	_ = ctx
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return fmt.Errorf("empty preset tag")
	}
	name := tag
	if v, ok := body["name"].(string); ok && strings.TrimSpace(v) != "" {
		name = strings.TrimSpace(v)
	}
	if isTombstone(body) {
		return s.deleteSetByName(name)
	}
	preset := tag
	if v, ok := body["preset"].(string); ok && strings.TrimSpace(v) != "" {
		preset = strings.TrimSpace(v)
	}
	listenPort := uint16FromAny(body["listen_port"])
	listen := "::"
	if v, ok := body["listen"].(string); ok && strings.TrimSpace(v) != "" {
		listen = strings.TrimSpace(v)
	}
	params := stringMapFromAny(body["params"])
	variants := stringSliceFromAny(body["enabled_user_variants"])
	profiles := stringSliceFromAny(body["enabled_client_profiles"])

	sets, err := s.store.LoadSets()
	if err != nil {
		return err
	}
	sets = removeSetByName(sets, name)
	if listenPort == 0 {
		pmeta, err := presets.Get(preset)
		if err != nil {
			return fmt.Errorf("cp_unknown_preset: %w", err)
		}
		need := networksFromTraits(pmeta.Traits)
		picked, err := suggestListenPort(sets, need)
		if err != nil {
			return err
		}
		listenPort = picked
	}
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name:       name,
		Listen:     listen,
		ListenPort: listenPort,
		Presets:    []string{preset},
		Bindings: []domain.SetBinding{{
			Preset:                preset,
			Params:                params,
			EnabledUserVariants:   variants,
			EnabledClientProfiles: profiles,
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.syncBindingSNI(&set)
	if err := s.validateSet(set, sets); err != nil {
		return err
	}
	sets = append(sets, set)
	return s.store.SaveSets(sets)
}

func (s *Service) deleteSetByName(name string) error {
	sets, err := s.store.LoadSets()
	if err != nil {
		return err
	}
	out := make([]domain.InboundSet, 0, len(sets))
	for _, set := range sets {
		if set.Name == name {
			continue
		}
		out = append(out, set)
	}
	return s.store.SaveSets(out)
}

func (s *Service) persistSSLBlock(id string, body map[string]any) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("empty ssl id")
	}
	if isTombstone(body) {
		if id == defaultSSLProfileID {
			return fmt.Errorf("cp_ssl_delete_refused: cannot delete default")
		}
		return s.deleteSSLProfileID(id)
	}
	existing, ok, err := s.findSSLProfile(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("not_found: ssl profile not found")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var next domain.SSLProfile
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	next.ID = id
	next.CreatedAt = existing.CreatedAt
	if next.CreatedAt.IsZero() {
		next.CreatedAt = time.Now().UTC()
	}
	next.UpdatedAt = time.Now().UTC()
	if strings.TrimSpace(next.Name) == "" {
		next.Name = existing.Name
	}
	next = next.Normalize()
	if next.Type == domain.SSLTypeSelfSigned && next.Domain == "" {
		// draft ok
	} else if err := next.Validate(); err != nil {
		return fmt.Errorf("cp_invalid_ssl_profile: %w", err)
	}
	if sslProfileIdentityChanged(existing, next) {
		if next.IsACME() || existing.IsACME() {
			_ = s.clearSSLACMEMaterial(next.ID)
		} else {
			s.clearSSLLeaf(next.ID)
		}
	}
	next, err = s.ensureSSLProfileMaterial(next, false)
	if err != nil {
		return fmt.Errorf("cp_ssl_material_failed: %w", err)
	}
	return s.upsertSSLProfile(next)
}

func (s *Service) deleteSSLProfileID(id string) error {
	list, _, err := s.store.LoadSSLProfiles()
	if err != nil {
		return err
	}
	out := make([]domain.SSLProfile, 0, len(list.Profiles))
	for _, p := range list.Profiles {
		if p.ID == id {
			continue
		}
		out = append(out, p)
	}
	list.Profiles = out
	return s.store.SaveSSLProfiles(list)
}

func (s *Service) persistRealityBlock(body map[string]any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var parsed struct {
		Profiles []domain.RealityEndpoint `json:"profiles"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return err
	}
	accepted := make([]domain.RealityEndpoint, 0, len(parsed.Profiles))
	for _, p := range parsed.Profiles {
		ep, err := normalizeRealityEndpoint(p)
		if err != nil {
			continue
		}
		accepted = append(accepted, ep)
	}
	if len(parsed.Profiles) > 0 && len(accepted) == 0 {
		return fmt.Errorf("cp_invalid_reality: all profiles rejected")
	}
	cfg, err := s.loadRealityConfig()
	if err != nil {
		return err
	}
	cfg.Profiles = accepted
	now := time.Now().UTC()
	cfg.UpdatedAt = &now
	return s.store.SaveRealityConfig(cfg)
}

func (s *Service) persistWgBlock(body map[string]any) error {
	h, err := s.store.LoadWgHub()
	if err != nil {
		return err
	}
	prevEnabled := h.Enabled
	if v, ok := body["enabled"].(bool); ok {
		h.Enabled = v
	}
	if v, ok := body["profile"].(string); ok && strings.TrimSpace(v) != "" {
		h.Profile = strings.TrimSpace(v)
	}
	if v, ok := body["subnet"].(string); ok && strings.TrimSpace(v) != "" {
		h.Subnet = strings.TrimSpace(v)
	}
	if p := uint16FromAny(body["listen_port"]); p > 0 {
		h.ListenPort = p
	}
	if v, ok := body["system"].(bool); ok {
		h.System = v
	}
	if v, ok := body["peer_relay"].(bool); ok {
		h.PeerRelay = v
	}
	if v, ok := body["internet_allow"].(bool); ok {
		h.InternetAllow = &v
	}
	if v, ok := body["name"].(string); ok {
		h.Name = strings.TrimSpace(v)
	}
	if v, ok := body["mtu"].(float64); ok {
		h.MTU = int(v)
	}
	h.Normalize()
	if err := h.Validate(); err != nil {
		return fmt.Errorf("cp_invalid_wg: %w", err)
	}
	if h.Enabled {
		if s.cfg.Owner != nil {
			if err := s.claimOwnership(configowner.ModeControlplane, "wg_enable", "wg"); err != nil {
				return err
			}
		}
	}
	if err := s.store.SaveWgHub(h); err != nil {
		return err
	}
	if !h.Enabled && !prevEnabled {
		return nil
	}
	return nil
}

func (s *Service) persistFragmentBlock(kind configFragmentKind, body map[string]any) error {
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		return err
	}
	if isTombstone(body) {
		setFragment(&f, kind, nil)
		return s.store.SaveConfigFragments(f)
	}
	key := string(kind)
	rawFrag, ok := body[key]
	if !ok {
		// allow body itself to be the fragment
		rawFrag = body
	}
	raw, err := json.Marshal(rawFrag)
	if err != nil {
		return err
	}
	switch kind {
	case configFragmentDNS:
		if err := domain.ValidateDNSFragment(raw); err != nil {
			return fmt.Errorf("cp_invalid_config: %w", err)
		}
	case configFragmentOutbounds:
		if err := domain.ValidateOutboundsFragment(raw); err != nil {
			return fmt.Errorf("cp_invalid_config: %w", err)
		}
	}
	setFragment(&f, kind, raw)
	return s.store.SaveConfigFragments(f)
}

func (s *Service) persistRouteBlock(body map[string]any) error {
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		return err
	}
	if isTombstone(body) {
		setFragment(&f, configFragmentRoute, nil)
		_ = s.store.ClearRulesets()
		return s.store.SaveConfigFragments(f)
	}
	routeRaw, ok := body["route"]
	if !ok {
		routeRaw = body
	}
	raw, err := json.Marshal(routeRaw)
	if err != nil {
		return err
	}
	raw, err = normalizeRouteRulesetPaths(raw)
	if err != nil {
		return err
	}
	if err := domain.ValidateRouteFragment(raw); err != nil {
		return fmt.Errorf("cp_invalid_config: %w", err)
	}
	files := map[string][]byte{}
	if rs, ok := body["rulesets"].([]any); ok {
		for i, item := range rs {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := m["filename"].(string)
			b64, _ := m["content_base64"].(string)
			safe, err := store.SafeRulesetFilename(fn)
			if err != nil {
				return fmt.Errorf("rulesets[%d]: %w", i, err)
			}
			data, err := decodeRulesetBase64(b64)
			if err != nil {
				return fmt.Errorf("rulesets[%d]: %w", i, err)
			}
			files[safe] = data
		}
	}
	setFragment(&f, configFragmentRoute, raw)
	if err := s.store.SaveConfigFragments(f); err != nil {
		return err
	}
	if len(files) > 0 {
		return s.store.WriteRulesets(files)
	}
	return nil
}

func decodeRulesetBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty content_base64")
	}
	return base64.StdEncoding.DecodeString(s)
}

func uint16FromAny(v any) uint16 {
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return uint16(t)
		}
	case int:
		if t > 0 {
			return uint16(t)
		}
	case json.Number:
		n, _ := t.Int64()
		if n > 0 {
			return uint16(n)
		}
	}
	return 0
}

func stringMapFromAny(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

func stringSliceFromAny(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

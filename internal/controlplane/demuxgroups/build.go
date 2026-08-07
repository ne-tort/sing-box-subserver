//go:build with_controlplane

package demuxgroups

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

// InstallRequest selects presets per slot and optional listen port.
type InstallRequest struct {
	GroupTag    string            `json:"group"`
	SetName     string            `json:"name"`
	Listen      string            `json:"listen,omitempty"`
	ListenPort  uint16            `json:"listen_port,omitempty"`
	SlotPreset  map[string]string `json:"slot_presets,omitempty"` // slot_id → preset tag
	// DisabledSlots lists slot IDs to skip entirely (not installed).
	// Omitted / empty string in slot_presets still means default_preset — use this field to disable.
	DisabledSlots []string `json:"disabled_slots,omitempty"`
	// SlotSNI optionally overrides demux_sni per slot.
	// Empty → auto-assign unique random SNIs from SNIPool / Reality defaults.
	SlotSNI map[string]string `json:"slot_sni,omitempty"` // slot_id → sni
	// SNIPool is the Reality SNI list used for auto demux_sni (unique per slot).
	// Empty → domain.DefaultRealitySNIs().
	SNIPool []string `json:"sni_pool,omitempty"`
	// SlotParams merges extra bindings[].params per slot (e.g. carrier room).
	// Keys already set by demux (demux_sni / demux_alpn) win over SlotParams.
	SlotParams map[string]map[string]string `json:"slot_params,omitempty"` // slot_id → params
	// SlotUserVariants / SlotClientProfiles control which client subscription
	// variants/profiles are materialized for each slot binding (no query tags).
	SlotUserVariants   map[string][]string `json:"slot_user_variants,omitempty"`
	SlotClientProfiles map[string][]string `json:"slot_client_profiles,omitempty"`
	// AllowLab permits demux_compat=demux_lab presets (TrustTunnel, ShadowQUIC, transport Reality).
	// Default false → only demux_compat=full.
	AllowLab    bool   `json:"allow_lab,omitempty"`
	Description string `json:"description,omitempty"`
}

// InstallResult is a ready inbound set + allocated member ports / SNIs.
type InstallResult struct {
	Set         domain.InboundSet
	MemberPorts map[string]uint16 // preset → private port
	SlotSNIs    map[string]string // slot_id → demux SNI
	Warnings    []string
}

// Demux TLS/Reality/QUIC slot SNIs come from InstallRequest.SNIPool
// (live Reality list) or domain.DefaultRealitySNIs(). No .local synthetics.

// BuildInstall materializes an InboundSet from a demux group + slot choices.
// usedPorts are public+private ports already taken (from other sets / WG).
func BuildInstall(req InstallRequest, usedPorts map[uint16]struct{}) (InstallResult, error) {
	g, err := Get(req.GroupTag)
	if err != nil {
		return InstallResult{}, err
	}
	name := strings.TrimSpace(req.SetName)
	if name == "" {
		name = g.Tag
	}
	listen := strings.TrimSpace(req.Listen)
	if listen == "" {
		listen = "::"
	}
	port := req.ListenPort
	if port == 0 {
		port = g.SuggestedPort
	}
	if port == 0 {
		port = 443
	}

	slotPreset := map[string]string{}
	disabled := map[string]struct{}{}
	for _, id := range req.DisabledSlots {
		id = strings.TrimSpace(id)
		if id != "" {
			disabled[id] = struct{}{}
		}
	}
	for _, slot := range g.Slots {
		if _, skip := disabled[slot.ID]; skip {
			continue
		}
		chosen := strings.TrimSpace(req.SlotPreset[slot.ID])
		if chosen == "" {
			chosen = slot.DefaultPreset
		}
		if !slot.AllowsPreset(chosen) {
			return InstallResult{}, fmt.Errorf("cp_invalid_slot: slot %q does not allow preset %q", slot.ID, chosen)
		}
		if inv, err := presets.GetInvariant(chosen); err == nil {
			looks := ""
			if inv.DemuxHints != nil {
				if !inv.DemuxHints.CompatibleWithDemux {
					return InstallResult{}, fmt.Errorf("cp_invalid_slot: preset %q is incompatible with demux (slot %q)", chosen, slot.ID)
				}
				looks = inv.DemuxHints.LooksLike
			}
			if !FitsInterchange(slot, inv.Traits, looks) {
				return InstallResult{}, fmt.Errorf("cp_invalid_slot: preset %q does not fit interchange of slot %q", chosen, slot.ID)
			}
		}
		compat := demuxCompatForPreset(chosen, slot.Role, slot.MatchHint)
		if compat == "demux_unsupported" {
			return InstallResult{}, fmt.Errorf("cp_invalid_slot: preset %q is demux_unsupported (slot %q)", chosen, slot.ID)
		}
		// Stable groups require allow_lab for demux_lab presets; lab groups may default to them.
		if compat == "demux_lab" && !req.AllowLab && !strings.EqualFold(g.Status, "lab") {
			return InstallResult{}, fmt.Errorf("cp_invalid_slot: preset %q is demux_lab (pass allow_lab:true to install, slot %q)", chosen, slot.ID)
		}
		slotPreset[slot.ID] = chosen
	}
	if len(slotPreset) == 0 {
		return InstallResult{}, fmt.Errorf("cp_invalid_demux_group: all slots disabled for group %q", g.Tag)
	}

	// When a TLS slot runs Naive with H3 enabled, it claims QUIC matching — drop separate QUIC members.
	if tlsSlotClaimsQUIC(g, slotPreset) {
		for _, slot := range g.Slots {
			if slot.Role == RoleQUIC {
				delete(slotPreset, slot.ID)
			}
		}
		if len(slotPreset) == 0 {
			return InstallResult{}, fmt.Errorf("cp_invalid_demux_group: all slots disabled for group %q", g.Tag)
		}
	}

	// Unique presets across slots (same preset twice is ambiguous for tags).
	seenPreset := map[string]string{}
	for sid, pn := range slotPreset {
		if prev, ok := seenPreset[pn]; ok {
			return InstallResult{}, fmt.Errorf("cp_invalid_slot: preset %q used by slots %q and %q", pn, prev, sid)
		}
		seenPreset[pn] = sid
	}

	pool := normalizeSNIPool(req.SNIPool)
	snis, warn, err := assignSlotSNIs(g, slotPreset, req.SlotSNI, pool)
	if err != nil {
		return InstallResult{}, err
	}
	memberPorts, err := allocateMemberPorts(slotPreset, usedPorts)
	if err != nil {
		return InstallResult{}, err
	}

	bindings := make([]domain.SetBinding, 0, len(slotPreset))
	presetList := make([]string, 0, len(slotPreset))
	for _, slot := range g.Slots {
		pn := slotPreset[slot.ID]
		if pn == "" {
			continue
		}
		presetList = append(presetList, pn)
		params := map[string]string{}
		if extra := req.SlotParams[slot.ID]; len(extra) > 0 {
			for k, v := range extra {
				k = strings.TrimSpace(k)
				v = strings.TrimSpace(v)
				if k == "" || v == "" {
					continue
				}
				params[k] = v
			}
		}
		if sni := snis[slot.ID]; sni != "" {
			params["demux_sni"] = sni
		}
		if len(slot.PreferredALPN) > 0 {
			params["demux_alpn"] = strings.Join(slot.PreferredALPN, ",")
		}
		bindings = append(bindings, domain.SetBinding{
			Preset:                pn,
			Params:                params,
			EnabledUserVariants:   append([]string{}, req.SlotUserVariants[slot.ID]...),
			EnabledClientProfiles: append([]string{}, req.SlotClientProfiles[slot.ID]...),
		})
	}

	tmpl, err := buildDemuxTemplate(g, slotPreset, memberPorts, snis)
	if err != nil {
		return InstallResult{}, err
	}

	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		_, desc = g.ResolveI18n("ru")
	}

	set := domain.InboundSet{
		Name:          name,
		Description:   desc,
		Listen:        listen,
		ListenPort:    port,
		Presets:       presetList,
		Bindings:      bindings,
		DemuxTemplate: tmpl,
		MemberPorts:   memberPorts,
		SlotSNIs:      snis,
		DemuxGroup:    g.Tag,
	}
	return InstallResult{Set: set, MemberPorts: memberPorts, SlotSNIs: snis, Warnings: warn}, nil
}

func normalizeSNIPool(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || strings.HasSuffix(s, ".local") {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		for _, s := range domain.DefaultRealitySNIs() {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	// Fisher–Yates shuffle for random unique assignment across slots.
	for i := len(out) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := i
		if err == nil {
			j = int(n.Int64())
		}
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func assignSlotSNIs(g Group, slotPreset map[string]string, overrides map[string]string, pool []string) (map[string]string, []string, error) {
	out := map[string]string{}
	used := map[string]struct{}{}
	var warnings []string
	idx := 0
	nextFrom := func() (string, error) {
		for idx < len(pool) {
			s := pool[idx]
			idx++
			if _, ok := used[s]; ok {
				continue
			}
			used[s] = struct{}{}
			return s, nil
		}
		return "", fmt.Errorf("cp_sni_pool_exhausted: need unique Reality SNI for demux slot (pool size %d)", len(pool))
	}
	// Apply explicit overrides first so auto-assigned SNIs stay unique.
	for _, slot := range g.Slots {
		if strings.TrimSpace(slotPreset[slot.ID]) == "" {
			continue
		}
		ov := strings.ToLower(strings.TrimSpace(overrides[slot.ID]))
		if ov == "" {
			continue
		}
		out[slot.ID] = ov
		used[ov] = struct{}{}
	}
	for _, slot := range g.Slots {
		if strings.TrimSpace(slotPreset[slot.ID]) == "" {
			continue
		}
		if out[slot.ID] != "" {
			continue
		}
		need := false
		switch slot.Role {
		case RoleTCPReality:
			need = true
		case RoleTCPTLS:
			if slot.MatchHint == "alpn" || slot.MatchHint == "sni_pool" || slot.MatchHint == "tls_and_quic" || slot.MatchHint == "" {
				need = true
			}
		case RoleQUIC:
			if slot.MatchHint == "sni_pool" {
				need = true
			}
		}
		if !need {
			continue
		}
		sni, err := nextFrom()
		if err != nil {
			return nil, warnings, err
		}
		out[slot.ID] = sni
	}
	return out, warnings, nil
}

func allocateMemberPorts(slotPreset map[string]string, used map[uint16]struct{}) (map[string]uint16, error) {
	if used == nil {
		used = map[uint16]struct{}{}
	}
	out := map[string]uint16{}
	for _, pn := range slotPreset {
		p, err := pickPrivatePort(used)
		if err != nil {
			return nil, err
		}
		used[p] = struct{}{}
		out[pn] = p
	}
	return out, nil
}

func pickPrivatePort(used map[uint16]struct{}) (uint16, error) {
	const lo, hi = 41000, 60000
	for attempt := 0; attempt < 5000; attempt++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(hi-lo)))
		if err != nil {
			// fallback deterministic-ish
			var b [2]byte
			_, _ = rand.Read(b[:])
			p := uint16(lo) + binary.BigEndian.Uint16(b[:])%uint16(hi-lo)
			if _, ok := used[p]; !ok {
				return p, nil
			}
			continue
		}
		p := uint16(lo) + uint16(n.Int64())
		if _, ok := used[p]; ok {
			continue
		}
		return p, nil
	}
	return 0, fmt.Errorf("cp_port_exhausted: no free private ports in %d-%d", lo, hi)
}

func buildDemuxTemplate(g Group, slotPreset map[string]string, ports map[string]uint16, snis map[string]string) (map[string]any, error) {
	networks := g.Networks
	if len(networks) == 0 {
		networks = []string{"tcp", "udp"}
	}
	rules := make([]any, 0, len(g.Slots)+2)

	// Order: specific SNI/ALPN → protocol class → plain catch-all → reject
	var plainSlot *Slot
	var quicProtocolOnly []Slot
	naiveQUICClaimed := false
	for i := range g.Slots {
		slot := g.Slots[i]
		if strings.TrimSpace(slotPreset[slot.ID]) == "" {
			continue
		}
		pn := slotPreset[slot.ID]
		switch {
		case slot.MatchHint == "tls_and_quic" || (slot.Role == RoleTCPTLS && naivePresetEnablesQUIC(pn)):
			port := ports[pn]
			sni := snis[slot.ID]
			if sni == "" {
				return nil, fmt.Errorf("slot %q requires SNI for demux match", slot.ID)
			}
			rules = append(rules, map[string]any{
				"name":   "tls-" + slot.ID,
				"match":  map[string]any{"tls": map[string]any{"sni": []any{sni}}},
				"action": dialAction(port),
			})
			// Only advertise QUIC when the chosen preset still enables H3.
			if naivePresetEnablesQUIC(pn) {
				naiveQUICClaimed = true
				rules = append(rules, map[string]any{
					"name":   "quic-" + slot.ID,
					"match":  map[string]any{"protocol": "quic"},
					"action": dialAction(port),
				})
			}
		case slot.Role == RoleTCPPlain || slot.MatchHint == "always_plain":
			plainSlot = &g.Slots[i]
		case slot.Role == RoleQUIC && slot.MatchHint == "protocol_only":
			quicProtocolOnly = append(quicProtocolOnly, slot)
		default:
			rule, err := slotRule(slot, pn, ports, snis)
			if err != nil {
				return nil, err
			}
			rules = append(rules, rule)
		}
	}
	// One protocol:quic rule (first protocol_only QUIC slot) — demux first-match.
	// Skip when Naive H2+H3 already claimed protocol=quic.
	if !naiveQUICClaimed {
		if len(quicProtocolOnly) > 1 {
			return nil, fmt.Errorf("cp_invalid_group: at most one protocol_only QUIC slot allowed (got %d)", len(quicProtocolOnly))
		}
		if len(quicProtocolOnly) > 0 {
			slot := quicProtocolOnly[0]
			pn := slotPreset[slot.ID]
			port := ports[pn]
			rules = append(rules, map[string]any{
				"name":   "quic-" + slot.ID,
				"match":  map[string]any{"protocol": "quic"},
				"action": dialAction(port),
			})
		}
	}
	if plainSlot != nil {
		pn := slotPreset[plainSlot.ID]
		port := ports[pn]
		rules = append(rules, map[string]any{
			"name":   "plain-" + plainSlot.ID,
			"match":  map[string]any{"always": true},
			"action": dialAction(port),
		})
	} else {
		rules = append(rules, map[string]any{
			"name":   "reject-rest",
			"match":  map[string]any{"always": true},
			"action": map[string]any{"reject": true},
		})
	}

	return map[string]any{
		"network": networks,
		"rules":   rules,
	}, nil
}

func slotRule(slot Slot, preset string, ports map[string]uint16, snis map[string]string) (map[string]any, error) {
	port, ok := ports[preset]
	if !ok || port == 0 {
		return nil, fmt.Errorf("missing private port for preset %q", preset)
	}
	name := string(slot.Role) + "-" + slot.ID
	match := map[string]any{}
	switch {
	case slot.MatchHint == "alpn" && len(slot.PreferredALPN) > 0:
		alpn := make([]any, 0, len(slot.PreferredALPN))
		for _, a := range slot.PreferredALPN {
			alpn = append(alpn, a)
		}
		match["tls"] = map[string]any{"alpn": alpn}
	case slot.Role == RoleQUIC:
		sni := snis[slot.ID]
		if sni != "" {
			match["sni"] = []any{sni}
			match["protocol"] = "quic"
		} else {
			match["protocol"] = "quic"
		}
	default:
		sni := snis[slot.ID]
		if sni == "" {
			return nil, fmt.Errorf("slot %q requires SNI for demux match", slot.ID)
		}
		// Prefer SNI-only demux match; PreferredALPN is applied to inbound tls.alpn via demux_alpn.
		match["tls"] = map[string]any{"sni": []any{sni}}
	}
	return map[string]any{
		"name":   name,
		"match":  match,
		"action": dialAction(port),
	}, nil
}

func dialAction(port uint16) map[string]any {
	return map[string]any{
		"dial": map[string]any{
			"address": "127.0.0.1",
			"port":    port,
		},
	}
}

// naivePresetEnablesQUIC reports whether a naive (or dual-capable) preset still listens UDP/H3.
func naivePresetEnablesQUIC(preset string) bool {
	inv, err := presets.GetInvariant(preset)
	if err != nil || inv.ParamMeta == nil {
		return false
	}
	meta, ok := inv.ParamMeta["network"]
	if !ok {
		return false
	}
	def := strings.ToLower(strings.TrimSpace(meta.Default))
	switch def {
	case "udp", "quic", "h3", "tcp,udp", "tcp+udp", "both", "dual":
		return true
	default:
		return strings.Contains(def, "udp") || strings.Contains(def, "quic")
	}
}

func tlsSlotClaimsQUIC(g Group, slotPreset map[string]string) bool {
	for _, slot := range g.Slots {
		pn := strings.TrimSpace(slotPreset[slot.ID])
		if pn == "" {
			continue
		}
		if slot.MatchHint == "tls_and_quic" || slot.Role == RoleTCPTLS {
			if naivePresetEnablesQUIC(pn) {
				return true
			}
		}
	}
	return false
}

// SubstitutionsView is API payload for UI slot picker.
type SubstitutionsView struct {
	GroupTag string             `json:"group"`
	Slots    []SlotSubstitution `json:"slots"`
}

type SlotSubstitution struct {
	ID               string         `json:"id"`
	Role             Role           `json:"role"`
	DefaultPreset    string         `json:"default_preset"`
	Presets          []string       `json:"presets"`
	Options          []PresetOption `json:"options,omitempty"`
	MatchHint        string         `json:"match_hint,omitempty"`
	PreferredALPN    []string       `json:"preferred_alpn,omitempty"`
	SeparationTags   []string       `json:"separation_tags,omitempty"`
	InterchangeTags  []string       `json:"interchange_tags,omitempty"`
	MatchShape       string         `json:"match_shape,omitempty"`
	MatchPriority    int            `json:"match_priority,omitempty"`
}

// PresetOption is UI metadata for a substitute preset.
type PresetOption struct {
	Tag             string         `json:"tag"`
	Title           string         `json:"title,omitempty"`
	Description     string         `json:"description,omitempty"`
	ShortName       string         `json:"short_name,omitempty"`
	Status          string         `json:"status,omitempty"`
	Traits          []string       `json:"traits,omitempty"`
	Scores          map[string]int `json:"scores,omitempty"`
	// DemuxCompat: full | demux_lab | demux_unsupported
	DemuxCompat     string         `json:"demux_compat,omitempty"`
	LooksLike       string         `json:"looks_like,omitempty"`
	DemuxHints      any            `json:"demux_hints,omitempty"`
	FitsInterchange bool           `json:"fits_interchange"`
}

// Substitutions builds UI view for a group with preset metadata for pickers.
func Substitutions(tag string, lang string) (SubstitutionsView, error) {
	g, err := Get(tag)
	if err != nil {
		return SubstitutionsView{}, err
	}
	if strings.TrimSpace(lang) == "" {
		lang = "ru"
	}
	slots := make([]SlotSubstitution, 0, len(g.Slots))
	for _, s := range g.Slots {
		meta := DeriveSlotMatchMeta(s)
		presetsList := s.AllPresets()
		options := make([]PresetOption, 0, len(presetsList))
		for _, pn := range presetsList {
			opt := PresetOption{Tag: pn, DemuxCompat: demuxCompatForPreset(pn, s.Role, s.MatchHint), FitsInterchange: true}
			if inv, err := presets.GetInvariant(pn); err == nil {
				title, desc := "", ""
				if t, d := resolvePresetI18n(inv.I18n, lang); t != "" || d != "" {
					title, desc = t, d
				}
				opt.Title = title
				opt.Description = desc
				opt.ShortName = inv.ShortName
				opt.Status = inv.Status
				opt.Traits = append([]string{}, inv.Traits...)
				if inv.DemuxHints != nil {
					opt.LooksLike = inv.DemuxHints.LooksLike
					opt.DemuxHints = inv.DemuxHints
					opt.FitsInterchange = inv.DemuxHints.CompatibleWithDemux &&
						FitsInterchange(s, inv.Traits, inv.DemuxHints.LooksLike)
				} else {
					opt.FitsInterchange = FitsInterchange(s, inv.Traits, "")
				}
				if inv.Scores != nil {
					scores := map[string]int{}
					if inv.Scores.DPI != nil {
						scores["dpi"] = *inv.Scores.DPI
					}
					if inv.Scores.Speed != nil {
						scores["speed"] = *inv.Scores.Speed
					}
					if inv.Scores.Mobile != nil {
						scores["mobile"] = *inv.Scores.Mobile
					}
					if inv.Scores.Setup != nil {
						scores["setup"] = *inv.Scores.Setup
					}
					if len(scores) > 0 {
						opt.Scores = scores
					}
				}
			}
			options = append(options, opt)
		}
		slots = append(slots, SlotSubstitution{
			ID:              s.ID,
			Role:            s.Role,
			DefaultPreset:   s.DefaultPreset,
			Presets:         presetsList,
			Options:         options,
			MatchHint:       s.MatchHint,
			PreferredALPN:   append([]string{}, s.PreferredALPN...),
			SeparationTags:  meta.SeparationTags,
			InterchangeTags: meta.InterchangeTags,
			MatchShape:      meta.MatchShape,
			MatchPriority:   meta.MatchPriority,
		})
	}
	return SubstitutionsView{GroupTag: g.Tag, Slots: slots}, nil
}

func resolvePresetI18n(m map[string]domain.LocalizedText, lang string) (string, string) {
	if m == nil {
		return "", ""
	}
	if v, ok := m[lang]; ok {
		return v.Title, v.Description
	}
	if v, ok := m["en"]; ok {
		return v.Title, v.Description
	}
	for _, v := range m {
		return v.Title, v.Description
	}
	return "", ""
}

func demuxCompatForPreset(tag string, role Role, matchHint string) string {
	t := strings.ToLower(tag)
	if inv, err := presets.GetInvariant(tag); err == nil && inv.DemuxHints != nil && !inv.DemuxHints.CompatibleWithDemux {
		return "demux_unsupported"
	}
	switch {
	case strings.HasPrefix(t, "trusttunnel"):
		return "demux_lab"
	case strings.HasPrefix(t, "shadowquic"):
		// JLS + demux dial (with_shadowquic in tags.server.controlplane).
		return "demux_lab"
	case t == "vless_ws_reality":
		// Wave E matrix (2026-07-30): member + demux front OK with allow_lab path;
		// promote so stable installs can use it without allow_lab.
		return "full"
	case strings.Contains(t, "_ws_reality"),
		strings.Contains(t, "_grpc_reality"),
		strings.Contains(t, "_http_reality"),
		strings.Contains(t, "_httpupgrade_reality"):
		// Other transport Reality variants remain demux_lab until matrix covers them.
		return "demux_lab"
	default:
		_ = role
		_ = matchHint
		return "full"
	}
}

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
	// SlotSNI optionally overrides demux_sni (and params.sni for ACME) per slot.
	// Empty → auto pool + per-slot self-signed as before.
	SlotSNI     map[string]string `json:"slot_sni,omitempty"` // slot_id → sni
	Description string            `json:"description,omitempty"`
}

// InstallResult is a ready inbound set + allocated member ports / SNIs.
type InstallResult struct {
	Set         domain.InboundSet
	MemberPorts map[string]uint16 // preset → private port
	SlotSNIs    map[string]string // slot_id → demux SNI
	Warnings    []string
}

// defaultSNIPool unique hostnames for TLS demux differentiation.
var defaultSNIPool = []string{
	"www.microsoft.com",
	"www.apple.com",
	"www.amazon.com",
	"gateway.icloud.com",
	"www.bing.com",
	"www.wikipedia.org",
	"github.com",
	"stackoverflow.com",
	"www.mozilla.org",
	"ubuntu.com",
	"www.debian.org",
	"www.kernel.org",
	"www.python.org",
	"nodejs.org",
	"www.php.net",
	"www.mysql.com",
	"www.postgresql.org",
	"www.docker.com",
	"kubernetes.io",
	"www.hashicorp.com",
	"www.atlassian.com",
	"www.jetbrains.com",
	"www.adobe.com",
	"www.autodesk.com",
	"www.oracle.com",
	"www.ibm.com",
	"www.amd.com",
	"www.nvidia.com",
	"www.dell.com",
	"www.lenovo.com",
	"www.asus.com",
	"www.samsung.com",
	"www.sony.com",
	"www.lg.com",
	"www.qualcomm.com",
	"www.broadcom.com",
	"www.ericsson.com",
	"www.nokia.com",
	"www.siemens.com",
	"www.bosch.com",
	"www.honeywell.com",
	"www.salesforce.com",
	"www.sap.com",
	"www.servicenow.com",
	"www.workday.com",
	"slack.com",
	"www.notion.so",
	"www.figma.com",
	"www.dropbox.com",
	"www.box.com",
	"asana.com",
	"monday.com",
	"www.shopify.com",
	"stripe.com",
	"www.paypal.com",
	"www.square.com",
	"www.mastercard.com",
	"www.americanexpress.com",
	"www.netflix.com",
	"www.disney.com",
	"www.twitch.tv",
	"www.imdb.com",
	"www.nytimes.com",
	"www.theguardian.com",
	"www.reuters.com",
	"www.bloomberg.com",
	"www.forbes.com",
	"www.wsj.com",
	"www.ft.com",
	"www.economist.com",
	"www.ieee.org",
	"www.acm.org",
	"www.ted.com",
	"www.duolingo.com",
	"www.edx.org",
	"udemy.com",
	"www.khanacademy.org",
	"wordpress.org",
	"wordpress.com",
	"www.wired.com",
	"techcrunch.com",
	"www.theverge.com",
	"arstechnica.com",
	"www.npr.org",
	"www.pbs.org",
	"www.nationalgeographic.com",
	"time.com",
	"www.ap.org",
	"www.nike.com",
	"www.adidas.com",
	"www.ikea.com",
	"www.uniqlo.com",
	"www.zara.com",
	"www.target.com",
	"www.walmart.com",
	"www.costco.com",
	"www.bestbuy.com",
	"www.homedepot.com",
	"www.ebay.com",
	"www.etsy.com",
	"www.booking.com",
	"www.airbnb.com",
	"www.expedia.com",
	"www.tripadvisor.com",
	"www.marriott.com",
	"www.hilton.com",
	"www.uber.com",
	"www.lyft.com",
	"www.agoda.com",
	"www.kayak.com",
	"www.toyota.com",
	"www.honda.com",
	"www.bmw.com",
	"www.mercedes-benz.com",
	"www.audi.com",
	"www.ford.com",
	"www.tesla.com",
	"www.boeing.com",
	"www.airbus.com",
	"www.emirates.com",
	"www.qatarairways.com",
	"www.singaporeair.com",
	"www.lufthansa.com",
	"www.airfrance.com",
	"www.klm.com",
	"www.britishairways.com",
	"www.united.com",
	"www.aa.com",
	"www.verizon.com",
	"www.att.com",
	"www.tmobile.com",
	"www.vodafone.com",
	"www.orange.com",
	"www.bt.com",
	"www.hsbc.com",
	"www.barclays.co.uk",
	"www.jpmorgan.com",
	"www.goldmansachs.com",
	"www.morganstanley.com",
	"www.bankofamerica.com",
	"www.wellsfargo.com",
	"www.chase.com",
	"www.citi.com",
	"www.nasa.gov",
	"www.nih.gov",
	"www.cdc.gov",
	"www.who.int",
	"www.un.org",
	"www.imf.org",
	"www.worldbank.org",
}

// realitySNIPool prefers hosts that pass CP Reality validation (no CDN-edge heuristics).
var realitySNIPool = []string{
	"www.microsoft.com",
	"www.apple.com",
	"www.amazon.com",
	"gateway.icloud.com",
	"www.bing.com",
	"www.wikipedia.org",
	"github.com",
	"stackoverflow.com",
	"www.mozilla.org",
	"ubuntu.com",
	"www.debian.org",
	"www.kernel.org",
	"www.python.org",
	"nodejs.org",
	"www.php.net",
	"www.mysql.com",
	"www.postgresql.org",
	"www.docker.com",
	"kubernetes.io",
	"www.hashicorp.com",
	"www.atlassian.com",
	"www.jetbrains.com",
	"www.adobe.com",
	"www.autodesk.com",
	"www.oracle.com",
	"www.ibm.com",
	"www.amd.com",
	"www.nvidia.com",
	"www.dell.com",
	"www.lenovo.com",
	"www.asus.com",
	"www.samsung.com",
	"www.sony.com",
	"www.lg.com",
	"www.qualcomm.com",
	"www.broadcom.com",
	"www.ericsson.com",
	"www.nokia.com",
	"www.siemens.com",
	"www.bosch.com",
	"www.honeywell.com",
	"www.salesforce.com",
	"www.sap.com",
	"www.servicenow.com",
	"www.workday.com",
	"slack.com",
	"www.notion.so",
	"www.figma.com",
	"www.dropbox.com",
	"www.box.com",
	"asana.com",
	"monday.com",
	"www.shopify.com",
	"stripe.com",
	"www.paypal.com",
	"www.square.com",
	"www.mastercard.com",
	"www.americanexpress.com",
	"www.netflix.com",
	"www.disney.com",
	"www.twitch.tv",
	"www.imdb.com",
	"www.nytimes.com",
	"www.theguardian.com",
	"www.reuters.com",
	"www.bloomberg.com",
	"www.forbes.com",
	"www.wsj.com",
	"www.ft.com",
	"www.economist.com",
	"www.ieee.org",
	"www.acm.org",
	"www.ted.com",
	"www.duolingo.com",
	"www.edx.org",
	"udemy.com",
	"www.khanacademy.org",
	"wordpress.org",
	"wordpress.com",
	"www.wired.com",
	"techcrunch.com",
	"www.theverge.com",
	"arstechnica.com",
	"www.npr.org",
	"www.pbs.org",
	"www.nationalgeographic.com",
	"time.com",
	"www.ap.org",
	"www.nike.com",
	"www.adidas.com",
	"www.ikea.com",
	"www.uniqlo.com",
	"www.zara.com",
	"www.target.com",
	"www.walmart.com",
	"www.costco.com",
	"www.bestbuy.com",
	"www.homedepot.com",
	"www.ebay.com",
	"www.etsy.com",
	"www.booking.com",
	"www.airbnb.com",
	"www.expedia.com",
	"www.tripadvisor.com",
	"www.marriott.com",
	"www.hilton.com",
	"www.uber.com",
	"www.lyft.com",
	"www.agoda.com",
	"www.kayak.com",
	"www.toyota.com",
	"www.honda.com",
	"www.bmw.com",
	"www.mercedes-benz.com",
	"www.audi.com",
	"www.ford.com",
	"www.tesla.com",
	"www.boeing.com",
	"www.airbus.com",
	"www.emirates.com",
	"www.qatarairways.com",
	"www.singaporeair.com",
	"www.lufthansa.com",
	"www.airfrance.com",
	"www.klm.com",
	"www.britishairways.com",
	"www.united.com",
	"www.aa.com",
	"www.verizon.com",
	"www.att.com",
	"www.tmobile.com",
	"www.vodafone.com",
	"www.orange.com",
	"www.bt.com",
	"www.hsbc.com",
	"www.barclays.co.uk",
	"www.jpmorgan.com",
	"www.goldmansachs.com",
	"www.morganstanley.com",
	"www.bankofamerica.com",
	"www.wellsfargo.com",
	"www.chase.com",
	"www.citi.com",
	"www.nasa.gov",
	"www.nih.gov",
	"www.cdc.gov",
	"www.who.int",
	"www.un.org",
	"www.imf.org",
	"www.worldbank.org",
}

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
	for _, slot := range g.Slots {
		chosen := strings.TrimSpace(req.SlotPreset[slot.ID])
		if chosen == "" {
			chosen = slot.DefaultPreset
		}
		if !slot.AllowsPreset(chosen) {
			return InstallResult{}, fmt.Errorf("cp_invalid_slot: slot %q does not allow preset %q", slot.ID, chosen)
		}
		slotPreset[slot.ID] = chosen
	}

	// Unique presets across slots (same preset twice is ambiguous for tags).
	seenPreset := map[string]string{}
	for sid, pn := range slotPreset {
		if prev, ok := seenPreset[pn]; ok {
			return InstallResult{}, fmt.Errorf("cp_invalid_slot: preset %q used by slots %q and %q", pn, prev, sid)
		}
		seenPreset[pn] = sid
	}

	snis, warn := assignSlotSNIs(g, slotPreset, req.SlotSNI)
	memberPorts, err := allocateMemberPorts(slotPreset, usedPorts)
	if err != nil {
		return InstallResult{}, err
	}

	bindings := make([]domain.SetBinding, 0, len(g.Slots))
	presetList := make([]string, 0, len(g.Slots))
	for _, slot := range g.Slots {
		pn := slotPreset[slot.ID]
		presetList = append(presetList, pn)
		params := map[string]string{}
		if sni := snis[slot.ID]; sni != "" {
			params["demux_sni"] = sni
			// Explicit slot_sni also sets params.sni for cert-manager (TLS only; never Reality).
			if ov := strings.TrimSpace(req.SlotSNI[slot.ID]); ov != "" {
				if p, err := presets.Get(pn); err == nil {
					isReality := false
					for _, t := range p.Traits {
						if t == "reality" {
							isReality = true
							break
						}
					}
					if !isReality {
						params[domain.BindingParamSNI] = strings.ToLower(ov)
					}
				}
			}
		}
		if len(slot.PreferredALPN) > 0 {
			params["demux_alpn"] = strings.Join(slot.PreferredALPN, ",")
		}
		bindings = append(bindings, domain.SetBinding{
			Preset: pn,
			Params: params,
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
		DemuxGroup:    g.Tag,
	}
	return InstallResult{Set: set, MemberPorts: memberPorts, SlotSNIs: snis, Warnings: warn}, nil
}

func assignSlotSNIs(g Group, slotPreset map[string]string, overrides map[string]string) (map[string]string, []string) {
	out := map[string]string{}
	used := map[string]struct{}{}
	var warnings []string
	tlsIdx, realityIdx := 0, 0
	nextFrom := func(pool []string, idx *int) string {
		for *idx < len(pool) {
			s := pool[*idx]
			*idx++
			if _, ok := used[s]; ok {
				continue
			}
			used[s] = struct{}{}
			return s
		}
		for _, s := range defaultSNIPool {
			if _, ok := used[s]; ok {
				continue
			}
			used[s] = struct{}{}
			return s
		}
		s := fmt.Sprintf("cp-slot-%d.local", len(used)+1)
		used[s] = struct{}{}
		warnings = append(warnings, "sni pool exhausted; using synthetic "+s)
		return s
	}
	// Apply explicit overrides first so auto-assigned SNIs stay unique.
	for _, slot := range g.Slots {
		ov := strings.ToLower(strings.TrimSpace(overrides[slot.ID]))
		if ov == "" {
			continue
		}
		out[slot.ID] = ov
		used[ov] = struct{}{}
	}
	for _, slot := range g.Slots {
		if out[slot.ID] != "" {
			continue
		}
		switch slot.Role {
		case RoleTCPReality:
			out[slot.ID] = nextFrom(realitySNIPool, &realityIdx)
		case RoleTCPTLS:
			if slot.MatchHint == "alpn" || slot.MatchHint == "sni_pool" || slot.MatchHint == "" {
				out[slot.ID] = nextFrom(defaultSNIPool, &tlsIdx)
			}
		case RoleQUIC:
			if slot.MatchHint == "sni_pool" {
				out[slot.ID] = nextFrom(defaultSNIPool, &tlsIdx)
			}
		}
		_ = slotPreset
	}
	return out, warnings
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
	for i := range g.Slots {
		slot := g.Slots[i]
		switch {
		case slot.Role == RoleTCPPlain || slot.MatchHint == "always_plain":
			plainSlot = &g.Slots[i]
		case slot.Role == RoleQUIC && slot.MatchHint == "protocol_only":
			quicProtocolOnly = append(quicProtocolOnly, slot)
		default:
			rule, err := slotRule(slot, slotPreset[slot.ID], ports, snis)
			if err != nil {
				return nil, err
			}
			rules = append(rules, rule)
		}
	}
	// One protocol:quic rule (first protocol_only QUIC slot) — demux first-match.
	if len(quicProtocolOnly) > 0 {
		slot := quicProtocolOnly[0]
		pn := slotPreset[slot.ID]
		port := ports[pn]
		rules = append(rules, map[string]any{
			"name":   "quic-" + slot.ID,
			"match":  map[string]any{"protocol": "quic"},
			"action": dialAction(port),
		})
		for _, extra := range quicProtocolOnly[1:] {
			// Additional protocol_only QUIC cannot be reached after first quic rule.
			_ = extra
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
func Substitutions(tag string) (SubstitutionsView, error) {
	g, err := Get(tag)
	if err != nil {
		return SubstitutionsView{}, err
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
				if t, d := resolvePresetI18n(inv.I18n, "ru"); t != "" || d != "" {
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
					opt.FitsInterchange = FitsInterchange(s, inv.Traits, inv.DemuxHints.LooksLike)
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
	switch {
	case strings.HasPrefix(t, "trusttunnel"):
		return "demux_lab"
	case strings.HasPrefix(t, "shadowquic"):
		// JLS SNI must stay aligned with demux_sni (materialize + harness).
		// sni_pool still lab until matrix proves parallel SQ+other QUIC.
		if matchHint == "sni_pool" && role == RoleQUIC {
			return "demux_lab"
		}
		return "full"
	default:
		return "full"
	}
}

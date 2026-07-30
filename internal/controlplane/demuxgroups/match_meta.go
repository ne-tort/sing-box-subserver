//go:build with_controlplane

package demuxgroups

import (
	"sort"
	"strings"
)

// Stable tag vocabulary for client match metadata (bootstrap demux_match_tag_vocab).
var MatchTagVocab = []string{
	"tcp", "udp", "tls", "quic", "reality", "sni", "alpn", "plain", "catch_all",
}

// SlotMatchMeta explains how a slot is separated from siblings and why substitutes are interchangeable.
type SlotMatchMeta struct {
	SeparationTags  []string `json:"separation_tags"`
	InterchangeTags []string `json:"interchange_tags"`
	MatchShape      string   `json:"match_shape"`
	MatchPriority   int      `json:"match_priority"`
}

// MatchPlanStep is one first-match step in the demux plan for a group.
type MatchPlanStep struct {
	SlotID         string   `json:"slot_id"`
	Role           Role     `json:"role"`
	MatchShape     string   `json:"match_shape"`
	SeparationTags []string `json:"separation_tags"`
	MatchPriority  int      `json:"match_priority"`
	Note           string   `json:"note,omitempty"`
}

// GroupMatchMeta is group-level match summary for API clients.
type GroupMatchMeta struct {
	SeparationSummary []string        `json:"separation_summary"`
	Plan              []MatchPlanStep `json:"plan"`
}

// DeriveSlotMatchMeta maps Role + MatchHint (+ ALPN) to client-facing match tags.
// Priority mirrors buildDemuxTemplate first-match order (lower = earlier).
func DeriveSlotMatchMeta(slot Slot) SlotMatchMeta {
	hint := strings.TrimSpace(slot.MatchHint)
	switch {
	case slot.Role == RoleTCPPlain || hint == "always_plain":
		return SlotMatchMeta{
			SeparationTags:  []string{"tcp", "plain", "catch_all"},
			InterchangeTags: []string{"tcp_plain"},
			MatchShape:      "always",
			MatchPriority:   300,
		}
	case slot.Role == RoleQUIC && hint == "protocol_only":
		return SlotMatchMeta{
			SeparationTags:  []string{"udp", "quic"},
			InterchangeTags: []string{"quic", "udp"},
			MatchShape:      "protocol.quic",
			MatchPriority:   200,
		}
	case slot.Role == RoleQUIC && hint == "sni_pool":
		return SlotMatchMeta{
			SeparationTags:  []string{"udp", "quic", "sni"},
			InterchangeTags: []string{"quic", "quic_sni"},
			MatchShape:      "protocol.quic+sni",
			MatchPriority:   100,
		}
	case slot.Role == RoleQUIC:
		// Default QUIC without explicit hint: protocol class.
		return SlotMatchMeta{
			SeparationTags:  []string{"udp", "quic"},
			InterchangeTags: []string{"quic", "udp"},
			MatchShape:      "protocol.quic",
			MatchPriority:   200,
		}
	case slot.Role == RoleTCPTLS && hint == "alpn" && len(slot.PreferredALPN) > 0:
		return SlotMatchMeta{
			SeparationTags:  []string{"tcp", "tls", "alpn"},
			InterchangeTags: []string{"tcp_tls", "tls_alpn"},
			MatchShape:      "tls.alpn",
			MatchPriority:   50,
		}
	case slot.Role == RoleTCPReality:
		return SlotMatchMeta{
			SeparationTags:  []string{"tcp", "tls", "reality", "sni"},
			InterchangeTags: []string{"tcp_reality", "tls_clienthello"},
			MatchShape:      "tls.sni",
			MatchPriority:   100,
		}
	case slot.Role == RoleTCPTLS:
		return SlotMatchMeta{
			SeparationTags:  []string{"tcp", "tls", "sni"},
			InterchangeTags: []string{"tcp_tls", "tls_clienthello"},
			MatchShape:      "tls.sni",
			MatchPriority:   100,
		}
	default:
		return SlotMatchMeta{
			SeparationTags:  []string{"tcp"},
			InterchangeTags: []string{string(slot.Role)},
			MatchShape:      "unknown",
			MatchPriority:   250,
		}
	}
}

// BuildGroupMatchMeta computes separation summary + ordered match plan for a group.
func BuildGroupMatchMeta(g Group) GroupMatchMeta {
	plan := make([]MatchPlanStep, 0, len(g.Slots))
	seen := map[string]struct{}{}
	var summary []string
	for _, slot := range g.Slots {
		m := DeriveSlotMatchMeta(slot)
		for _, t := range m.SeparationTags {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			summary = append(summary, t)
		}
		plan = append(plan, MatchPlanStep{
			SlotID:         slot.ID,
			Role:           slot.Role,
			MatchShape:     m.MatchShape,
			SeparationTags: append([]string{}, m.SeparationTags...),
			MatchPriority:  m.MatchPriority,
			Note:           matchNote(slot, m),
		})
	}
	sort.SliceStable(plan, func(i, j int) bool {
		if plan[i].MatchPriority != plan[j].MatchPriority {
			return plan[i].MatchPriority < plan[j].MatchPriority
		}
		return plan[i].SlotID < plan[j].SlotID
	})
	sort.Strings(summary)
	return GroupMatchMeta{SeparationSummary: summary, Plan: plan}
}

func matchNote(slot Slot, m SlotMatchMeta) string {
	switch m.MatchShape {
	case "tls.sni":
		if slot.Role == RoleTCPReality {
			return "TLS ClientHello SNI from Reality pool"
		}
		return "TLS ClientHello SNI from demux pool"
	case "tls.alpn":
		return "TLS ClientHello ALPN " + strings.Join(slot.PreferredALPN, ",")
	case "protocol.quic":
		return "any QUIC Initial (UDP)"
	case "protocol.quic+sni":
		return "QUIC Initial matched by SNI"
	case "always":
		return "catch-all remaining TCP (plain)"
	default:
		return ""
	}
}

// EnrichSlotAPI returns a slot map with base fields + match metadata for HTTP list/get.
func EnrichSlotAPI(slot Slot) map[string]any {
	m := DeriveSlotMatchMeta(slot)
	item := map[string]any{
		"id":               slot.ID,
		"role":             slot.Role,
		"default_preset":   slot.DefaultPreset,
		"substitutes":      slot.AllPresets(), // legacy alias
		"presets":          slot.AllPresets(),
		"match_hint":       slot.MatchHint,
		"separation_tags":  m.SeparationTags,
		"interchange_tags": m.InterchangeTags,
		"match_shape":      m.MatchShape,
		"match_priority":   m.MatchPriority,
	}
	if len(slot.PreferredALPN) > 0 {
		item["preferred_alpn"] = append([]string{}, slot.PreferredALPN...)
	}
	return item
}

// FitsInterchange reports whether preset traits/looks_like are compatible with the slot interchange class.
func FitsInterchange(slot Slot, traits []string, looksLike string) bool {
	m := DeriveSlotMatchMeta(slot)
	traitSet := map[string]struct{}{}
	for _, t := range traits {
		traitSet[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	looksLike = strings.ToLower(strings.TrimSpace(looksLike))

	has := func(t string) bool {
		_, ok := traitSet[t]
		return ok
	}

	switch slot.Role {
	case RoleTCPReality:
		return has("reality") && has("tls") && (looksLike == "" || looksLike == "tls_clienthello")
	case RoleTCPTLS:
		if has("reality") {
			return false
		}
		if !has("tls") && !has("tls_custom") {
			return false
		}
		if m.MatchShape == "tls.alpn" {
			return true // ALPN enforced by demux rule; preset may omit alpn list
		}
		return looksLike == "" || looksLike == "tls_clienthello"
	case RoleQUIC:
		if has("obfs") {
			return false // salamander etc. hide QUIC from demux classifiers
		}
		return (has("quic") || has("udp")) && (looksLike == "" || looksLike == "quic")
	case RoleTCPPlain:
		if has("tls") || has("tls_custom") || has("quic") || has("reality") {
			return false
		}
		return true
	default:
		return true
	}
}

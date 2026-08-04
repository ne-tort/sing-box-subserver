//go:build with_controlplane

package demuxgroups

import (
	"fmt"
	"strings"
)

// Role classifies a demux slot for substitution UI.
type Role string

const (
	RoleTCPReality Role = "tcp_reality" // TLS ClientHello + Reality
	RoleTCPTLS     Role = "tcp_tls"     // classic TLS (SNI/ALPN demux)
	RoleTCPPlain   Role = "tcp_plain"   // non-TLS TCP (sudoku/mieru/ssh)
	RoleQUIC       Role = "quic"        // QUIC Initial (hy2/tuic/shadowquic/…)
)

// Slot is one demux member with a default preset and allowed substitutes.
type Slot struct {
	ID            string   `json:"id"`
	Role          Role     `json:"role"`
	DefaultPreset string   `json:"default_preset"`
	Substitutes   []string `json:"substitutes"` // includes default; UI may pick any
	// MatchHint guides rule generation: sni_pool | alpn | protocol_only | always_plain
	MatchHint string `json:"match_hint,omitempty"`
	// PreferredALPN optional ALPN fingerprints for this slot (tcp_tls).
	PreferredALPN []string `json:"preferred_alpn,omitempty"`
}

// Group is a first-class demux catalog entity (installable on one public port).
type Group struct {
	Tag           string            `json:"tag"`
	ShortName     string            `json:"short_name"`
	Status        string            `json:"status"` // lab | stable
	SuggestedPort uint16            `json:"suggested_port,omitempty"`
	Networks      []string          `json:"networks"` // tcp / udp
	I18n          map[string]I18n   `json:"i18n"`
	Slots         []Slot            `json:"slots"`
	Scores        map[string]int    `json:"scores,omitempty"`
	Notes         string            `json:"notes,omitempty"`
}

type I18n struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ResolveI18n picks lang with en, then ru fallback.
func (g Group) ResolveI18n(lang string) (title, desc string) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		lang = "en"
	}
	if e, ok := g.I18n[lang]; ok {
		return e.Title, e.Description
	}
	// pt-br / zh_cn aliases
	if alt := strings.ReplaceAll(lang, "_", "-"); alt != lang {
		if e, ok := g.I18n[alt]; ok {
			return e.Title, e.Description
		}
	}
	if e, ok := g.I18n["en"]; ok {
		return e.Title, e.Description
	}
	if e, ok := g.I18n["ru"]; ok {
		return e.Title, e.Description
	}
	return g.ShortName, g.Notes
}

// brandI18n fills app locales; missing langs fall back to English description.
func brandI18n(title string, desc map[string]string) map[string]I18n {
	en := strings.TrimSpace(desc["en"])
	langs := []string{"en", "ru", "ar", "es", "fa", "fr", "id", "pt-BR", "tr", "zh-CN", "zh-TW"}
	out := make(map[string]I18n, len(langs))
	for _, lang := range langs {
		d := strings.TrimSpace(desc[lang])
		if d == "" {
			d = en
		}
		out[lang] = I18n{Title: title, Description: d}
	}
	return out
}

// SlotByID returns a slot or error.
func (g Group) SlotByID(id string) (Slot, error) {
	for _, s := range g.Slots {
		if s.ID == id {
			return s, nil
		}
	}
	return Slot{}, fmt.Errorf("unknown slot %q in group %q", id, g.Tag)
}

// AllowsPreset reports whether preset is default or substitute for the slot.
func (s Slot) AllowsPreset(preset string) bool {
	preset = strings.TrimSpace(preset)
	if preset == s.DefaultPreset {
		return true
	}
	for _, p := range s.Substitutes {
		if p == preset {
			return true
		}
	}
	return false
}

// AllPresets returns unique default+substitutes.
func (s Slot) AllPresets() []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 1+len(s.Substitutes))
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	add(s.DefaultPreset)
	for _, p := range s.Substitutes {
		add(p)
	}
	return out
}

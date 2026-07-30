//go:build with_controlplane

package domain

import (
	"strings"
	"time"
)

// User is a local controlplane account.
type User struct {
	ID                    string                    `json:"id"`
	Name                  string                    `json:"name"`
	Enabled               bool                      `json:"enabled"`
	CreatedAt             time.Time                 `json:"created_at"`
	UpdatedAt             time.Time                 `json:"updated_at"`
	ExpiresAt             *time.Time                `json:"expires_at,omitempty"`
	TrafficLimitBytes     *uint64                   `json:"traffic_limit_bytes,omitempty"`
	TrafficUsedBytes      uint64                    `json:"traffic_used_bytes"`
	TrafficResetAt        *time.Time                `json:"traffic_reset_at,omitempty"`
	TrafficResetPeriodSec *uint64                   `json:"traffic_reset_period_sec,omitempty"`
	SpeedUpBytesPerSec    int64                     `json:"speed_up_bytes_per_sec,omitempty"`
	SpeedDownBytesPerSec  int64                     `json:"speed_down_bytes_per_sec,omitempty"`
	SubToken              string                    `json:"sub_token"`
	Creds                 map[string]map[string]any `json:"creds"`
}

// Eligible reports whether the user may appear in materialize / fetch a sub.
func (u User) Eligible(now time.Time) bool {
	if !u.Enabled {
		return false
	}
	if u.ExpiresAt != nil && !now.Before(*u.ExpiresAt) {
		return false
	}
	if u.TrafficLimitBytes != nil && u.TrafficUsedBytes >= *u.TrafficLimitBytes {
		return false
	}
	return true
}

// InboundSet is a named listen + presets (+ optional demux).
type InboundSet struct {
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Listen        string            `json:"listen"`
	ListenPort    uint16            `json:"listen_port"`
	Presets       []string          `json:"presets"`
	Bindings      []SetBinding      `json:"bindings,omitempty"`
	DemuxTemplate map[string]any    `json:"demux_template,omitempty"`
	// PeerSecrets holds set/preset-level secrets (not per-user), keyed as
	// "canonicalPreset/field" — e.g. SS2022 inbound server password.
	PeerSecrets map[string]string   `json:"peer_secrets,omitempty"`
	// MemberPorts maps canonical preset → private loopback port when demux uses dial/forward.
	MemberPorts map[string]uint16 `json:"member_ports,omitempty"`
	// DemuxGroup is the catalog tag this set was installed from (optional).
	DemuxGroup string `json:"demux_group,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// HasDemux reports whether the set uses a demux front.
func (s InboundSet) HasDemux() bool {
	return len(s.DemuxTemplate) > 0
}

// EffectiveBindings returns logical bindings with backward compatibility.
// Old `presets[]` are treated as bindings with default variant policy.
func (s InboundSet) EffectiveBindings() []SetBinding {
	if len(s.Bindings) > 0 {
		out := make([]SetBinding, 0, len(s.Bindings))
		for _, b := range s.Bindings {
			if b.Preset == "" {
				continue
			}
			out = append(out, b)
		}
		if len(out) > 0 {
			return out
		}
	}
	out := make([]SetBinding, 0, len(s.Presets))
	for _, pn := range s.Presets {
		if pn == "" {
			continue
		}
		out = append(out, SetBinding{Preset: pn})
	}
	return out
}

// MaterializeStatus tracks the latest controlplane materialize/apply attempt.
type MaterializeStatus struct {
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	LastErrorCode string     `json:"last_error_code,omitempty"`
	LastApplyNoop bool       `json:"last_apply_noop,omitempty"`
}

// OwnerTransition records a config_mode ownership change initiated by controlplane.
type OwnerTransition struct {
	From    string    `json:"from"`
	To      string    `json:"to"`
	At      time.Time `json:"at"`
	Reason  string    `json:"reason,omitempty"`
	Trigger string    `json:"trigger,omitempty"`
	Success bool      `json:"success"`
	Error   string    `json:"error,omitempty"`
}

// State is persisted runtime for active sets.
type State struct {
	ActiveSets            []string            `json:"active_sets"`
	LastMaterializeSHA256 string              `json:"last_materialize_sha256,omitempty"`
	LastMaterializeAt     *time.Time          `json:"last_materialize_at,omitempty"`
	Materialize           *MaterializeStatus  `json:"materialize,omitempty"`
	OwnerTransitions      []OwnerTransition   `json:"owner_transitions,omitempty"`
}

// ProtocolPreset is an embedded catalog entry (compat view of an invariant).
// Name is the canonical tag (snake_case). Lookups also accept Aliases.
type ProtocolPreset struct {
	Name                   string            `json:"name"`
	Protocol               string            `json:"protocol"`
	Description            string            `json:"description"`
	Traits                 []string          `json:"traits"`
	InboundTemplate        map[string]any    `json:"inbound_template"`
	OutboundTemplate       map[string]any    `json:"outbound_template"`
	EndpointTemplate       map[string]any    `json:"endpoint_template,omitempty"`
	CredFields             []string          `json:"cred_fields"` // e.g. uuid, password
	CredGenerators         map[string]string `json:"cred_generators,omitempty"`
	PeerSecretFields       map[string]string `json:"peer_secret_fields,omitempty"` // inbound top-level secrets: field→generator
	// ParamFields lists required keys in SetBinding.Params (e.g. carrier room URL).
	ParamFields            []string          `json:"param_fields,omitempty"`
	Aliases                []string          `json:"aliases,omitempty"`
	ShortName              string            `json:"short_name,omitempty"`
	Status                 string            `json:"status,omitempty"`
	Scores                 *PresetScores     `json:"scores,omitempty"`
	DemuxHints             *DemuxHints       `json:"demux_hints,omitempty"`
	Requirements           *PresetReqs       `json:"requirements,omitempty"`
	DefaultUserVariants    []string          `json:"default_user_variants,omitempty"`
	DefaultClientProfiles  []string          `json:"default_client_profiles,omitempty"`
}

// PresetScores are subjective 0–10 ratings (see docs/guides/controlplane-presets).
type PresetScores struct {
	DPI    *int `json:"dpi,omitempty"`
	Speed  *int `json:"speed,omitempty"`
	Mobile *int `json:"mobile,omitempty"`
	Setup  *int `json:"setup,omitempty"`
}

// DemuxHints describe pre-TLS / first-packet characteristics for demux composition.
type DemuxHints struct {
	Network             []string `json:"network,omitempty"`
	LooksLike           string   `json:"looks_like,omitempty"` // tls_clienthello|quic|ssh_banner|http|raw_tcp
	ALPN                []string `json:"alpn,omitempty"`
	SNIRequired         bool     `json:"sni_required,omitempty"`
	FirstBytes          string   `json:"first_bytes,omitempty"`
	CompatibleWithDemux bool     `json:"compatible_with_demux"`
}

// PresetReqs lists materialize dependencies for an invariant.
type PresetReqs struct {
	TLSProfile         bool   `json:"tls_profile,omitempty"`
	RealityAssignment  bool   `json:"reality_assignment,omitempty"`
	UDP                bool   `json:"udp,omitempty"`
	QUIC               bool   `json:"quic,omitempty"`
	BuildTag           string `json:"build_tag,omitempty"`
}

// LocalizedText is title+description for one language.
type LocalizedText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ProtocolMeta is protocol-folder metadata (protocol.json).
type ProtocolMeta struct {
	SchemaVersion      int                       `json:"schema_version"`
	Tag                string                    `json:"tag"`
	SingBoxType        string                    `json:"singbox_type"`
	ShortName          string                    `json:"short_name"`
	Status             string                    `json:"status"` // stable|lab|planned
	I18n               map[string]LocalizedText  `json:"i18n"`
	DefaultCredFields  []string                  `json:"default_cred_fields,omitempty"`
	Notes              map[string]string         `json:"notes,omitempty"`
	InvariantTags      []string                  `json:"-"` // filled by loader
}

// InvariantPreset is a full invariant JSON file.
type InvariantPreset struct {
	SchemaVersion         int                      `json:"schema_version"`
	Tag                   string                   `json:"tag"`
	Protocol              string                   `json:"protocol"`
	Aliases               []string                 `json:"aliases,omitempty"`
	ShortName             string                   `json:"short_name"`
	Status                string                   `json:"status"`
	I18n                  map[string]LocalizedText `json:"i18n"`
	Traits                []string                 `json:"traits"`
	DemuxHints            *DemuxHints              `json:"demux_hints,omitempty"`
	Scores                *PresetScores            `json:"scores,omitempty"`
	Requirements          *PresetReqs              `json:"requirements,omitempty"`
	CredFields            []string                 `json:"cred_fields"`
	CredGenerators        map[string]string        `json:"cred_generators,omitempty"`
	PeerSecretFields      map[string]string        `json:"peer_secret_fields,omitempty"`
	ParamFields           []string                 `json:"param_fields,omitempty"`
	DefaultUserVariants   []string                 `json:"default_user_variants,omitempty"`
	DefaultClientProfiles []string                 `json:"default_client_profiles,omitempty"`
	InboundTemplate       map[string]any           `json:"inbound_template,omitempty"`
	OutboundTemplate      map[string]any           `json:"outbound_template,omitempty"`
	EndpointTemplate      map[string]any           `json:"endpoint_template,omitempty"`
	ClientNotes           map[string]string        `json:"client_notes,omitempty"`
}

// ToProtocolPreset builds the compat view with description resolved for lang (ru default).
func (inv InvariantPreset) ToProtocolPreset(lang string) ProtocolPreset {
	title, desc := ResolveI18n(inv.I18n, lang)
	if title == "" {
		title = inv.ShortName
	}
	_ = title
	var gens map[string]string
	if len(inv.CredGenerators) > 0 {
		gens = make(map[string]string, len(inv.CredGenerators))
		for k, v := range inv.CredGenerators {
			gens[k] = v
		}
	}
	var peer map[string]string
	if len(inv.PeerSecretFields) > 0 {
		peer = make(map[string]string, len(inv.PeerSecretFields))
		for k, v := range inv.PeerSecretFields {
			peer[k] = v
		}
	}
	return ProtocolPreset{
		Name:                  inv.Tag,
		Protocol:              inv.Protocol,
		Description:           desc,
		Traits:                append([]string{}, inv.Traits...),
		InboundTemplate:       inv.InboundTemplate,
		OutboundTemplate:      inv.OutboundTemplate,
		EndpointTemplate:      inv.EndpointTemplate,
		CredFields:            append([]string{}, inv.CredFields...),
		CredGenerators:        gens,
		PeerSecretFields:      peer,
		ParamFields:           append([]string{}, inv.ParamFields...),
		Aliases:               append([]string{}, inv.Aliases...),
		ShortName:             inv.ShortName,
		Status:                inv.Status,
		Scores:                inv.Scores,
		DemuxHints:            inv.DemuxHints,
		Requirements:          inv.Requirements,
		DefaultUserVariants:   append([]string{}, inv.DefaultUserVariants...),
		DefaultClientProfiles: append([]string{}, inv.DefaultClientProfiles...),
	}
}

// ResolveI18n picks lang, then ru, then en, then any.
func ResolveI18n(i18n map[string]LocalizedText, lang string) (title, description string) {
	lang = NormalizeLang(lang)
	try := []string{lang, "ru", "en"}
	for _, k := range try {
		if k == "" {
			continue
		}
		if t, ok := i18n[k]; ok && (t.Title != "" || t.Description != "") {
			return t.Title, t.Description
		}
	}
	for _, t := range i18n {
		if t.Title != "" || t.Description != "" {
			return t.Title, t.Description
		}
	}
	return "", ""
}

// NormalizeLang maps ru-RU → ru, en_US → en.
func NormalizeLang(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		return "ru"
	}
	if i := strings.IndexAny(lang, "-_"); i > 0 {
		lang = lang[:i]
	}
	return lang
}

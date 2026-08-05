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
	// SlotSNIs maps demux slot id → SNI used for match / Reality (persisted for list/get after reload).
	SlotSNIs map[string]string `json:"slot_snis,omitempty"`
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
	// OptionalParamFields lists optional binding keys exposed in params_schema.
	OptionalParamFields    []string          `json:"optional_param_fields,omitempty"`
	// ParamMeta per-field schema v2 + UX (title/help/required_guide) for thin clients.
	ParamMeta              map[string]ParamFieldMeta `json:"param_meta,omitempty"`
	// CustomPreset marks a full user-driven constructor invariant ({protocol}_custom).
	CustomPreset           bool              `json:"custom_preset,omitempty"`
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
	Status             string                    `json:"status"` // stable|lab|planned|deferred
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
	OptionalParamFields   []string                 `json:"optional_param_fields,omitempty"`
	ParamMeta             map[string]ParamFieldMeta `json:"param_meta,omitempty"`
	CustomPreset          bool                     `json:"custom_preset,omitempty"`
	DefaultUserVariants   []string                 `json:"default_user_variants,omitempty"`
	DefaultClientProfiles []string                 `json:"default_client_profiles,omitempty"`
	// ParamValues are explicit constructor knobs for ready presets (SQLite SoT).
	// Base/custom presets leave this empty and use ParamMeta defaults instead.
	ParamValues           map[string]string        `json:"param_values,omitempty"`
	InboundTemplate       map[string]any           `json:"inbound_template,omitempty"`
	OutboundTemplate      map[string]any           `json:"outbound_template,omitempty"`
	EndpointTemplate      map[string]any           `json:"endpoint_template,omitempty"`
	ClientNotes           map[string]string        `json:"client_notes,omitempty"`
}

// ParamFieldMeta is schema v2 + UX metadata for a preset param.
// Localized maps use language keys (ru/en); structural fields are language-agnostic.
type ParamFieldMeta struct {
	// Required overrides membership in param_fields when set.
	Required *bool `json:"required,omitempty"`
	// Type: string | uint16 | bool | enum | string_list (default string).
	Type string `json:"type,omitempty"`
	// Enum lists allowed values when Type is enum (or for select widgets).
	Enum []string `json:"enum,omitempty"`
	// EnumLabels maps value → localized label (lang → text). Prefer i18n locale files.
	EnumLabels map[string]map[string]string `json:"enum_labels,omitempty"`
	Default    string                       `json:"default,omitempty"`
	Placeholder string                      `json:"placeholder,omitempty"`
	Pattern    string                       `json:"pattern,omitempty"`
	Min        *float64                     `json:"min,omitempty"`
	Max        *float64                     `json:"max,omitempty"`
	UiGroup    string                       `json:"ui_group,omitempty"`
	UiOrder    int                          `json:"ui_order,omitempty"`
	// Widget: text | select | toggle | port | path (hint for thin clients).
	Widget string `json:"widget,omitempty"`
	// UiActions: optional app-bar actions on the param detail page (e.g. "randomize").
	UiActions []string `json:"ui_actions,omitempty"`
	// VisibleWhen: all conditions must match for the field to show.
	VisibleWhen []ParamCondition `json:"visible_when,omitempty"`
	// Requires: other param keys that must be non-empty / true.
	Requires []string `json:"requires,omitempty"`
	// ConflictsWith: mutually exclusive param keys (reject if both set).
	ConflictsWith []string `json:"conflicts_with,omitempty"`

	Title         map[string]string `json:"title,omitempty"`
	Description   map[string]string `json:"description,omitempty"`
	Help          *ParamHelpMeta    `json:"help,omitempty"`
	RequiredGuide *ParamGuideMeta   `json:"required_guide,omitempty"`
	// RequiredGuides are mutually exclusive branches selected by VisibleWhen (first match).
	RequiredGuides []ParamGuideMeta `json:"required_guides,omitempty"`
}

// ParamCondition is a machine-readable visibility / branch rule.
type ParamCondition struct {
	Key      string   `json:"key"`
	Equals   string   `json:"equals,omitempty"`
	In       []string `json:"in,omitempty"`
	NotEmpty bool     `json:"not_empty,omitempty"`
}

// ParamHelpMeta is the simple form shown next to a parameter field.
type ParamHelpMeta struct {
	Summary   map[string]string `json:"summary,omitempty"`
	InputHint map[string]string `json:"input_hint,omitempty"`
	Format    string            `json:"format,omitempty"`
}

// ParamGuideMeta is a step-by-step guide for required parameters.
type ParamGuideMeta struct {
	VisibleWhen []ParamCondition    `json:"visible_when,omitempty"`
	Title       map[string]string   `json:"title,omitempty"`
	Steps       []ParamGuideStepMeta `json:"steps,omitempty"`
}

// ParamGuideStepMeta is one guide step (optional URL).
type ParamGuideStepMeta struct {
	Text map[string]string `json:"text,omitempty"`
	URL  string            `json:"url,omitempty"`
}

func cloneParamMeta(in map[string]ParamFieldMeta) map[string]ParamFieldMeta {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ParamFieldMeta, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// PickLocalized returns lang, then en, then any non-empty value.
// Russian is not a universal fallback (catalog policy: missing → English).
func PickLocalized(m map[string]string, lang string) string {
	if len(m) == 0 {
		return ""
	}
	lang = NormalizeLang(lang)
	for _, k := range []string{lang, "en"} {
		if k == "" {
			continue
		}
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	for _, v := range m {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
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
		OptionalParamFields:   append([]string{}, inv.OptionalParamFields...),
		ParamMeta:             cloneParamMeta(inv.ParamMeta),
		CustomPreset:          inv.CustomPreset,
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

// ResolveI18n picks lang, then en, then any.
func ResolveI18n(i18n map[string]LocalizedText, lang string) (title, description string) {
	lang = NormalizeLang(lang)
	// Requested lang → English. Do not fall back to Russian for other langs.
	try := []string{lang, "en"}
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

// NormalizeLang maps BCP-47 / ISO tags to catalog locale keys.
// Known regional variants (Hiddify app locales): pt-BR, zh-CN, zh-TW.
// Others collapse to ISO 639-1 primary subtag (ru-RU→ru, en_US→en).
func NormalizeLang(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(lang))
	lang = strings.ReplaceAll(lang, "_", "-")
	if lang == "" || lang == "*" {
		return "ru"
	}
	switch {
	case lang == "pt-br" || lang == "ptbr" || strings.HasPrefix(lang, "pt-br"):
		return "pt-BR"
	case lang == "zh-cn" || lang == "zhcn" || lang == "zh-hans" || strings.HasPrefix(lang, "zh-cn") || strings.HasPrefix(lang, "zh-hans"):
		return "zh-CN"
	case lang == "zh-tw" || lang == "zhtw" || lang == "zh-hant" || strings.HasPrefix(lang, "zh-tw") || strings.HasPrefix(lang, "zh-hant"):
		return "zh-TW"
	case lang == "zh":
		return "zh-CN"
	case lang == "pt":
		return "pt-BR"
	}
	if i := strings.Index(lang, "-"); i > 0 {
		lang = lang[:i]
	}
	return lang
}

// CatalogLangs are locale directories shipped for controlplane catalog copy.
// Matches Hiddify client language picker; missing keys fall back to English.
var CatalogLangs = []string{
	"ar", "en", "es", "fa", "fr", "id", "pt-BR", "ru", "tr", "zh-CN", "zh-TW",
}

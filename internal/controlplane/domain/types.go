//go:build with_controlplane

package domain

import "time"

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
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Listen        string         `json:"listen"`
	ListenPort    uint16         `json:"listen_port"`
	Presets       []string       `json:"presets"`
	Bindings      []SetBinding   `json:"bindings,omitempty"`
	DemuxTemplate map[string]any `json:"demux_template,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
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

// ProtocolPreset is an embedded catalog entry.
type ProtocolPreset struct {
	Name             string         `json:"name"`
	Protocol         string         `json:"protocol"`
	Description      string         `json:"description"`
	Traits           []string       `json:"traits"`
	InboundTemplate  map[string]any `json:"inbound_template"`
	OutboundTemplate map[string]any `json:"outbound_template"`
	CredFields       []string       `json:"cred_fields"` // e.g. uuid, password
}

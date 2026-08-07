//go:build with_controlplane

package domain

import "time"

// ECHProfileMeta describes one ECH keypair managed by controlplane.
type ECHProfileMeta struct {
	ID         string `json:"id"`
	PublicName string `json:"public_name"`
}

// ECHConfig is the editable ECH bank (keys live under controlplane/ech/).
type ECHConfig struct {
	Profiles  []ECHProfileMeta `json:"profiles,omitempty"`
	DefaultID string           `json:"default_id,omitempty"`
	UpdatedAt *time.Time       `json:"updated_at,omitempty"`
}

//go:build with_controlplane

package domain

import "time"

// RealityEndpoint describes one reusable Reality server name profile.
type RealityEndpoint struct {
	SNI             string `json:"sni"`
	HandshakeServer string `json:"handshake_server,omitempty"`
	HandshakePort   uint16 `json:"handshake_port,omitempty"`
}

// RealityConfig stores user overrides and the currently effective validated pool.
type RealityConfig struct {
	UserProfiles       []RealityEndpoint `json:"user_profiles,omitempty"`
	EffectiveProfiles  []RealityEndpoint `json:"effective_profiles,omitempty"`
	UsingUserOverrides bool              `json:"using_user_overrides"`
	UpdatedAt          *time.Time        `json:"updated_at,omitempty"`
}

// RealityAssignment binds one inbound identity to generated key material
// and one chosen endpoint from the effective pool.
type RealityAssignment struct {
	InboundKey       string    `json:"inbound_key"`
	SNI              string    `json:"sni"`
	HandshakeServer  string    `json:"handshake_server"`
	HandshakePort    uint16    `json:"handshake_port"`
	PrivateKeyBase64 string    `json:"private_key_base64"`
	PublicKeyBase64  string    `json:"public_key_base64"`
	ShortID          string    `json:"short_id"`
	UpdatedAt        time.Time `json:"updated_at"`
}

//go:build with_controlplane

package domain

import "time"

// BindingParamRealitySNI optionally pins a Reality inbound to a pool SNI.
// Empty / absent means auto-pick from the server Reality profiles list.
const BindingParamRealitySNI = "reality_sni"

// RealityEndpoint describes one reusable Reality server name profile.
type RealityEndpoint struct {
	SNI             string `json:"sni"`
	HandshakeServer string `json:"handshake_server,omitempty"`
	HandshakePort   uint16 `json:"handshake_port,omitempty"`
}

// RealityConfig is the editable Reality SNI pool for the server.
// Seeded once from DefaultRealityProfiles on first start.
type RealityConfig struct {
	Profiles  []RealityEndpoint `json:"profiles,omitempty"`
	UpdatedAt *time.Time        `json:"updated_at,omitempty"`
}

// RealityAssignment binds one inbound identity to generated key material
// and one chosen endpoint from the profiles pool.
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

//go:build with_controlplane

package domain

import (
	"fmt"
	"net"
	"strings"
)

// TLSProfile is self-signed material used for mgmt safety PEMs and materialize fallbacks.
// Prefer SSL profiles (ssl_profiles.json) for inbound leaf selection.
type TLSProfile struct {
	SelfSigned *SelfSignedSpec `json:"self_signed"`
}

// SelfSignedSpec declares how controlplane generates PEM material.
type SelfSignedSpec struct {
	CommonName   string   `json:"common_name"`
	DNSSANs      []string `json:"dns_sans,omitempty"`
	IPSANs       []string `json:"ip_sans,omitempty"`
	KeyType      string   `json:"key_type"` // p256 (default), p384, rsa2048
	ValidDays    int      `json:"valid_days"`
	Organization string   `json:"organization,omitempty"`
}

// DefaultSelfSigned builds a profile from public_host.
func DefaultSelfSigned(publicHost string) TLSProfile {
	cn := publicHost
	if cn == "" {
		cn = "localhost"
	}
	spec := &SelfSignedSpec{
		CommonName:   cn,
		DNSSANs:      []string{"localhost"},
		KeyType:      "p256",
		ValidDays:    3650,
		Organization: "sing-box-subserver-controlplane",
	}
	if ip := net.ParseIP(cn); ip != nil {
		spec.IPSANs = []string{cn}
	} else if cn != "localhost" {
		spec.DNSSANs = append([]string{cn}, spec.DNSSANs...)
	}
	return TLSProfile{SelfSigned: spec}
}

// Validate checks self-signed profile invariants.
func (p TLSProfile) Validate() error {
	if p.SelfSigned == nil {
		return fmt.Errorf("self_signed spec required")
	}
	return p.SelfSigned.Validate()
}

func (s SelfSignedSpec) Validate() error {
	if strings.TrimSpace(s.CommonName) == "" {
		return fmt.Errorf("common_name required")
	}
	switch s.KeyType {
	case "", "p256", "p384", "rsa2048":
	default:
		return fmt.Errorf("unsupported key_type %q", s.KeyType)
	}
	if s.ValidDays < 0 {
		return fmt.Errorf("valid_days must be >= 0")
	}
	for _, ip := range s.IPSANs {
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid ip_sans entry %q", ip)
		}
	}
	return nil
}

// ServerNameForTLS returns default server_name for inbounds without ssl_profile.
func (p TLSProfile) ServerNameForTLS(fallbackHost string) string {
	if p.SelfSigned != nil && p.SelfSigned.CommonName != "" {
		return p.SelfSigned.CommonName
	}
	if fallbackHost != "" {
		return fallbackHost
	}
	return "localhost"
}

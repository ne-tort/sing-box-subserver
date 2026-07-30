//go:build with_controlplane

package domain

import (
	"fmt"
	"net"
	"strings"
)

// ProviderTag is the certificate_providers tag emitted for cert-manager ACME.
const TLSProviderTag = "cp-tls"

// BindingParamSNI is the optional inbound param that selects an ACME domain
// from CertManager (non-Reality TLS inbounds only).
const BindingParamSNI = "sni"

// TLSProfile is always self-signed material for default TLS inbounds / mgmt safety PEMs.
// ACME is managed separately via CertManager.
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

// CertManager is the ACME / sing-box certificate_providers configuration.
type CertManager struct {
	Email                   string         `json:"email,omitempty"`
	Domains                 []string       `json:"domains,omitempty"`
	Provider                string         `json:"provider,omitempty"` // letsencrypt default
	KeyType                 string         `json:"key_type,omitempty"`
	DisableHTTPChallenge    bool           `json:"disable_http_challenge,omitempty"`
	DisableTLSALPNChallenge bool           `json:"disable_tls_alpn_challenge,omitempty"`
	AlternativeHTTPPort     int            `json:"alternative_http_port,omitempty"`
	AlternativeTLSPort      int            `json:"alternative_tls_port,omitempty"`
	DNS01Challenge          map[string]any `json:"dns01_challenge,omitempty"`
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

// Enabled reports whether cert-manager has any domains configured.
func (c CertManager) Enabled() bool {
	return len(c.NormalizedDomains()) > 0
}

// NormalizedDomains returns trimmed non-empty domains (lowercased).
func (c CertManager) NormalizedDomains() []string {
	out := make([]string, 0, len(c.Domains))
	seen := map[string]struct{}{}
	for _, d := range c.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out
}

// HasDomain reports whether domain is in the cert-manager pool.
func (c CertManager) HasDomain(domain string) bool {
	want := strings.ToLower(strings.TrimSpace(domain))
	if want == "" {
		return false
	}
	for _, d := range c.NormalizedDomains() {
		if d == want {
			return true
		}
	}
	return false
}

// Validate checks cert-manager settings when domains are present.
func (c CertManager) Validate() error {
	domains := c.NormalizedDomains()
	if len(domains) == 0 {
		// Empty = disabled; OK.
		return nil
	}
	if strings.TrimSpace(c.Email) == "" {
		return fmt.Errorf("cert_manager.email required when domains are set")
	}
	hasIP := false
	hasName := false
	for _, d := range domains {
		if net.ParseIP(d) != nil {
			hasIP = true
		} else {
			hasName = true
		}
	}
	if hasIP && hasName {
		return fmt.Errorf("cert_manager.domains must not mix IP and DNS names")
	}
	prov := c.Provider
	if prov == "" {
		prov = "letsencrypt"
	}
	if hasIP {
		if len(domains) != 1 {
			return fmt.Errorf("cert_manager IP mode requires exactly one IP in domains")
		}
		if prov != "letsencrypt" {
			return fmt.Errorf("cert_manager IP only supports letsencrypt, got %q", prov)
		}
		if len(c.DNS01Challenge) > 0 {
			return fmt.Errorf("dns01_challenge not allowed for IP domains")
		}
	} else {
		if prov != "letsencrypt" && prov != "zerossl" && !strings.HasPrefix(prov, "https://") {
			return fmt.Errorf("unsupported acme provider %q", prov)
		}
	}
	if c.AlternativeHTTPPort < 0 || c.AlternativeHTTPPort > 65535 {
		return fmt.Errorf("alternative_http_port out of range")
	}
	if c.AlternativeTLSPort < 0 || c.AlternativeTLSPort > 65535 {
		return fmt.Errorf("alternative_tls_port out of range")
	}
	if c.DisableHTTPChallenge && c.DisableTLSALPNChallenge && len(c.DNS01Challenge) == 0 {
		return fmt.Errorf("all ACME challenge methods disabled")
	}
	return nil
}

// NeedsTLSReportsInsecure tells subscription outbounds to set tls.insecure
// when the inbound uses default self-signed material (no ACME params.sni).
func NeedsTLSReportsInsecure(bindingSNI string, cm CertManager) bool {
	sni := strings.TrimSpace(bindingSNI)
	if sni == "" {
		return true
	}
	return !cm.HasDomain(sni)
}

// ServerNameForTLS returns default server_name for inbounds without params.sni.
func (p TLSProfile) ServerNameForTLS(fallbackHost string) string {
	if p.SelfSigned != nil && p.SelfSigned.CommonName != "" {
		return p.SelfSigned.CommonName
	}
	if fallbackHost != "" {
		return fallbackHost
	}
	return "localhost"
}

// LegacyTLSProfileJSON is used only for migrating old tls_profile.json with mode/acme.
type LegacyTLSProfileJSON struct {
	Mode       string          `json:"mode"`
	SelfSigned *SelfSignedSpec `json:"self_signed,omitempty"`
	ACME       *CertManager    `json:"acme,omitempty"`
}

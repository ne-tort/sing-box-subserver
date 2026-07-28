//go:build with_controlplane

package domain

import (
	"fmt"
	"net"
	"strings"
)

// TLS modes for the single controlplane TLS profile.
const (
	TLSModeSelfSigned = "self_signed"
	TLSModeACMEDomain = "acme_domain"
	TLSModeACMEIP     = "acme_ip"
)

// ProviderTag is the certificate_providers tag emitted for ACME modes.
const TLSProviderTag = "cp-tls"

// TLSProfile is the active TLS configuration for all TLS inbounds.
type TLSProfile struct {
	Mode       string          `json:"mode"`
	SelfSigned *SelfSignedSpec `json:"self_signed,omitempty"`
	ACME       *ACMESpec       `json:"acme,omitempty"`
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

// ACMESpec is mapped into sing-box certificate_providers type=acme.
type ACMESpec struct {
	Email                   string         `json:"email"`
	Domains                 []string       `json:"domains"`
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
	return TLSProfile{Mode: TLSModeSelfSigned, SelfSigned: spec}
}

// Validate checks profile invariants.
func (p TLSProfile) Validate() error {
	switch p.Mode {
	case TLSModeSelfSigned:
		if p.SelfSigned == nil {
			return fmt.Errorf("self_signed spec required")
		}
		return p.SelfSigned.Validate()
	case TLSModeACMEDomain:
		if p.ACME == nil {
			return fmt.Errorf("acme spec required")
		}
		return p.ACME.ValidateDomain()
	case TLSModeACMEIP:
		if p.ACME == nil {
			return fmt.Errorf("acme spec required")
		}
		return p.ACME.ValidateIP()
	default:
		return fmt.Errorf("unknown tls mode %q", p.Mode)
	}
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

func (a ACMESpec) ValidateDomain() error {
	if strings.TrimSpace(a.Email) == "" {
		return fmt.Errorf("acme.email required")
	}
	if len(a.Domains) == 0 {
		return fmt.Errorf("acme.domains required")
	}
	for _, d := range a.Domains {
		if net.ParseIP(d) != nil {
			return fmt.Errorf("acme_domain must not use bare IP %q (use acme_ip)", d)
		}
		if strings.TrimSpace(d) == "" {
			return fmt.Errorf("empty domain")
		}
	}
	prov := a.Provider
	if prov == "" {
		prov = "letsencrypt"
	}
	if prov != "letsencrypt" && prov != "zerossl" && !strings.HasPrefix(prov, "https://") {
		return fmt.Errorf("unsupported acme provider %q", prov)
	}
	return nil
}

func (a ACMESpec) ValidateIP() error {
	if strings.TrimSpace(a.Email) == "" {
		return fmt.Errorf("acme.email required")
	}
	if len(a.Domains) != 1 {
		return fmt.Errorf("acme_ip requires exactly one IP in domains")
	}
	if net.ParseIP(a.Domains[0]) == nil {
		return fmt.Errorf("acme_ip domain must be an IP address")
	}
	prov := a.Provider
	if prov == "" {
		prov = "letsencrypt"
	}
	if prov != "letsencrypt" {
		return fmt.Errorf("acme_ip only supports letsencrypt, got %q", prov)
	}
	if len(a.DNS01Challenge) > 0 {
		return fmt.Errorf("dns01_challenge not allowed for acme_ip")
	}
	return nil
}

// NeedsTLSReportsInsecure tells subscription outbounds to set tls.insecure.
func (p TLSProfile) NeedsTLSReportsInsecure() bool {
	return p.Mode == TLSModeSelfSigned
}

// ServerNameForTLS returns SNI / server_name for inbounds and outbounds.
func (p TLSProfile) ServerNameForTLS(fallbackHost string) string {
	switch p.Mode {
	case TLSModeACMEDomain, TLSModeACMEIP:
		if p.ACME != nil && len(p.ACME.Domains) > 0 {
			return p.ACME.Domains[0]
		}
	case TLSModeSelfSigned:
		if p.SelfSigned != nil && p.SelfSigned.CommonName != "" {
			return p.SelfSigned.CommonName
		}
	}
	if fallbackHost != "" {
		return fallbackHost
	}
	return "localhost"
}

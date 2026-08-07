//go:build with_controlplane

package domain

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// BindingParamSSLProfile selects an SSL profile for PEM/ACME TLS inbounds.
const BindingParamSSLProfile = "ssl_profile"

// SSL profile types.
const (
	SSLTypeSelfSigned = "self_signed"
	SSLTypeACME       = "acme"
	SSLTypeACMEIP     = "acme_ip"
)

// SSL status states.
const (
	SSLStateReady   = "ready"
	SSLStatePending = "pending"
	SSLStateMissing = "missing"
	SSLStateExpired = "expired"
	SSLStateError   = "error"
)

// SSLProfile is a first-class leaf + handshake configuration.
type SSLProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // self_signed | acme | acme_ip

	Domain string `json:"domain,omitempty"` // ACME DNS or self_signed SNI
	IP     string `json:"ip,omitempty"`     // acme_ip

	Email                   string         `json:"email,omitempty"`
	Provider                string         `json:"provider,omitempty"`
	KeyType                 string         `json:"key_type,omitempty"`
	DisableHTTPChallenge    bool           `json:"disable_http_challenge,omitempty"`
	DisableTLSALPNChallenge bool           `json:"disable_tls_alpn_challenge,omitempty"`
	DNS01Challenge          map[string]any `json:"dns01_challenge,omitempty"`
	ExternalAccount         map[string]any `json:"external_account,omitempty"`
	ACMEProfile             string         `json:"acme_profile,omitempty"`
	AccountKey              string         `json:"account_key,omitempty"`
	DefaultServerName       string         `json:"default_server_name,omitempty"`

	ALPN              string `json:"alpn,omitempty"`
	MinVersion        string `json:"min_version,omitempty"`
	MaxVersion        string `json:"max_version,omitempty"`
	CipherSuites      string `json:"cipher_suites,omitempty"`
	CurvePreferences  string `json:"curve_preferences,omitempty"`
	SelfSignedValidDays int  `json:"self_signed_valid_days,omitempty"`

	ECHEnabled bool   `json:"ech_enabled,omitempty"`
	ECHSNI     string `json:"ech_sni,omitempty"`

	// Source marks bootstrap-managed profiles (e.g. free_dns:sslip). Empty = operator-owned.
	Source string `json:"source,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SSLProfileStatus is computed material state for API responses.
type SSLProfileStatus struct {
	State     string     `json:"state"`
	CertPath  string     `json:"cert_path,omitempty"`
	KeyPath   string     `json:"key_path,omitempty"`
	NotBefore *time.Time `json:"not_before,omitempty"`
	NotAfter  *time.Time `json:"not_after,omitempty"`
	SubjectCN string     `json:"subject_cn,omitempty"`
	SANs      []string   `json:"sans,omitempty"`
	Issuer    string     `json:"issuer,omitempty"`
	IsIP      bool       `json:"is_ip,omitempty"`
	ECHReady  bool       `json:"ech_ready,omitempty"`
	LastError string     `json:"last_error,omitempty"`
}

// SSLProfilesFile is the on-disk catalog.
type SSLProfilesFile struct {
	Profiles []SSLProfile `json:"profiles"`
}

// SSLProviderTag returns certificate_providers tag for an ACME profile.
func SSLProviderTag(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "cp-ssl"
	}
	return "cp-ssl-" + id
}

func (p SSLProfile) Normalize() SSLProfile {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	if p.Type == "" {
		p.Type = SSLTypeSelfSigned
	}
	p.Domain = strings.ToLower(strings.TrimSpace(p.Domain))
	p.IP = strings.TrimSpace(p.IP)
	p.Email = strings.TrimSpace(p.Email)
	if strings.EqualFold(p.Email, "auto") {
		p.Email = ""
	}
	p.Provider = strings.TrimSpace(p.Provider)
	p.ECHSNI = strings.ToLower(strings.TrimSpace(p.ECHSNI))
	p.Source = strings.TrimSpace(p.Source)
	p.ALPN = strings.TrimSpace(p.ALPN)
	p.MinVersion = strings.TrimSpace(p.MinVersion)
	p.MaxVersion = strings.TrimSpace(p.MaxVersion)
	p.CipherSuites = strings.TrimSpace(p.CipherSuites)
	p.CurvePreferences = strings.TrimSpace(p.CurvePreferences)
	return p
}

func (p SSLProfile) Validate() error {
	p = p.Normalize()
	if p.ID == "" {
		return fmt.Errorf("ssl profile id required")
	}
	if p.Name == "" {
		return fmt.Errorf("ssl profile name required")
	}
	switch p.Type {
	case SSLTypeSelfSigned:
		if p.Domain != "" && net.ParseIP(p.Domain) != nil {
			return fmt.Errorf("self_signed domain must not be an IP")
		}
	case SSLTypeACME:
		if p.Domain == "" {
			return fmt.Errorf("acme profile requires domain")
		}
		if net.ParseIP(p.Domain) != nil {
			return fmt.Errorf("acme domain must be DNS name, use type acme_ip for IPs")
		}
		if p.Email != "" && !looksLikeEmail(p.Email) {
			return fmt.Errorf("acme email invalid")
		}
	case SSLTypeACMEIP:
		if p.IP == "" {
			return fmt.Errorf("acme_ip profile requires ip")
		}
		if net.ParseIP(p.IP) == nil {
			return fmt.Errorf("acme_ip invalid ip %q", p.IP)
		}
		if p.Email != "" && !looksLikeEmail(p.Email) {
			return fmt.Errorf("acme email invalid")
		}
	default:
		return fmt.Errorf("unsupported ssl type %q", p.Type)
	}
	return nil
}

func looksLikeEmail(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	host := s[at+1:]
	return strings.Contains(host, ".") && !strings.ContainsAny(s, " \t\r\n")
}

// ACMEEmailAuto reports that the profile uses the default Reality-derived ACME email.
func (p SSLProfile) ACMEEmailAuto() bool {
	return p.Normalize().Email == ""
}

// PickAutoACMEEmail returns admin@<reality-sni>, stable for profileID (looks random across profiles).
// pool empty → DefaultRealitySNIs().
func PickAutoACMEEmail(profileID string, pool []string) string {
	cands := make([]string, 0, len(pool))
	seen := map[string]struct{}{}
	for _, s := range pool {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || net.ParseIP(s) != nil || strings.HasSuffix(s, ".local") {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		cands = append(cands, s)
	}
	if len(cands) == 0 {
		for _, s := range DefaultRealitySNIs() {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "" || net.ParseIP(s) != nil {
				continue
			}
			cands = append(cands, s)
		}
	}
	if len(cands) == 0 {
		return "admin@localhost.localdomain"
	}
	h := fnv32(strings.TrimSpace(profileID))
	if h == 0 {
		h = 1
	}
	return "admin@" + cands[int(h%uint32(len(cands)))]
}

// EffectiveACMEEmail returns explicit profile email, or PickAutoACMEEmail when auto.
func EffectiveACMEEmail(p SSLProfile, pool []string) string {
	p = p.Normalize()
	if p.Email != "" {
		return p.Email
	}
	return PickAutoACMEEmail(p.ID, pool)
}

func fnv32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// ServerName returns the TLS server_name for this profile.
func (p SSLProfile) ServerName() string {
	p = p.Normalize()
	switch p.Type {
	case SSLTypeACMEIP:
		return p.IP
	default:
		if p.Domain != "" {
			return p.Domain
		}
		return p.Name
	}
}

// IsACME reports ACME-backed leaf.
func (p SSLProfile) IsACME() bool {
	t := strings.ToLower(strings.TrimSpace(p.Type))
	return t == SSLTypeACME || t == SSLTypeACMEIP
}

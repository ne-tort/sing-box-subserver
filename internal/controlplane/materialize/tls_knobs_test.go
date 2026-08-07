//go:build with_controlplane

package materialize

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func TestApplyTLSHandshakeKnobs(t *testing.T) {
	tls := map[string]any{"enabled": true}
	applyTLSHandshakeKnobs(tls, map[string]string{
		"tls_alpn":              "h2, http/1.1",
		"tls_min_version":       "1.2",
		"tls_max_version":       "1.3",
		"tls_cipher_suites":     "TLS_AES_128_GCM_SHA256",
		"tls_curve_preferences": "X25519,P256",
	})
	alpn, _ := tls["alpn"].([]any)
	if len(alpn) != 2 || alpn[0] != "h2" || alpn[1] != "http/1.1" {
		t.Fatalf("alpn=%#v", alpn)
	}
	if tls["min_version"] != "1.2" || tls["max_version"] != "1.3" {
		t.Fatalf("versions %#v", tls)
	}
}

func TestBuildSSLProfileKnobs(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name:       "solo",
		Listen:     "0.0.0.0",
		ListenPort: 8443,
		Bindings: []domain.SetBinding{{
			Preset: "trojan-tcp",
			Params: map[string]string{domain.BindingParamSSLProfile: "p1"},
		}},
	}
	raw, err := Build(Input{
		ActiveSets:  []domain.InboundSet{set},
		Users:       []domain.User{trojanUser(now)},
		PublicHost:  "203.0.113.10",
		DataDir:     "/data",
		TLS:         domain.DefaultSelfSigned("203.0.113.10"),
		TLSCertPath: "/data/controlplane/ssl/default/cert.crt",
		TLSKeyPath:  "/data/controlplane/ssl/default/cert.key",
		SSLProfiles: []domain.SSLProfile{{
			ID: "p1", Name: "P1", Type: domain.SSLTypeSelfSigned, Domain: "vpn.example.com",
			ALPN: "http/1.1", MinVersion: "1.3",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	tls := ib["tls"].(map[string]any)
	if tls["server_name"] != "vpn.example.com" {
		t.Fatalf("server_name=%v", tls["server_name"])
	}
	if tls["min_version"] != "1.3" {
		t.Fatalf("min_version=%v", tls)
	}
	alpn, _ := tls["alpn"].([]any)
	if len(alpn) != 1 || alpn[0] != "http/1.1" {
		t.Fatalf("alpn=%#v", alpn)
	}
}

func TestBuildECHFromSSLProfile(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name:       "solo",
		Listen:     "0.0.0.0",
		ListenPort: 8443,
		Bindings: []domain.SetBinding{{
			Preset: "trojan-tcp",
			Params: map[string]string{domain.BindingParamSSLProfile: "p1"},
		}},
	}
	raw, err := Build(Input{
		ActiveSets:  []domain.InboundSet{set},
		Users:       []domain.User{trojanUser(now)},
		PublicHost:  "vpn.example.com",
		DataDir:     "/data",
		TLS:         domain.DefaultSelfSigned("vpn.example.com"),
		TLSCertPath: "/data/controlplane/ssl/default/cert.crt",
		TLSKeyPath:  "/data/controlplane/ssl/default/cert.key",
		SSLProfiles: []domain.SSLProfile{{
			ID: "p1", Name: "P1", Type: domain.SSLTypeSelfSigned, Domain: "vpn.example.com", ECHEnabled: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	tls := ib["tls"].(map[string]any)
	ech, _ := tls["ech"].(map[string]any)
	if ech["enabled"] != true {
		t.Fatalf("ech=%#v", ech)
	}
	keyPath, _ := ech["key_path"].(string)
	if !strings.HasSuffix(strings.ReplaceAll(keyPath, "\\", "/"), "controlplane/ssl/p1/ech.key.pem") {
		t.Fatalf("ech key_path=%q", keyPath)
	}
}

func TestCertificateProvidersFromSSL(t *testing.T) {
	t.Parallel()
	providers := certificateProviders(Input{
		DataDir: "/data",
		SSLProfiles: []domain.SSLProfile{{
			ID: "acme1", Name: "A", Type: domain.SSLTypeACME, Domain: "vpn.example.com",
			Email: "a@b.c", DefaultServerName: "vpn.example.com", ACMEProfile: "tlsserveronly",
			AccountKey: "secret", ExternalAccount: map[string]any{"key_id": "kid", "mac_key": "mac"},
		}},
	})
	if len(providers) != 1 {
		t.Fatalf("providers=%d", len(providers))
	}
	p := providers[0].(map[string]any)
	if p["default_server_name"] != "vpn.example.com" || p["profile"] != "tlsserveronly" {
		t.Fatalf("%#v", p)
	}
	if p["account_key"] != "secret" {
		t.Fatalf("account_key missing")
	}
	ea, _ := p["external_account"].(map[string]any)
	if ea["key_id"] != "kid" {
		t.Fatalf("external_account=%#v", ea)
	}
}

func TestCertificateProvidersAutoEmail(t *testing.T) {
	t.Parallel()
	providers := certificateProviders(Input{
		DataDir: "/data",
		SSLProfiles: []domain.SSLProfile{{
			ID: "acme-auto", Name: "A", Type: domain.SSLTypeACME, Domain: "vpn.example.com",
		}},
	})
	if len(providers) != 1 {
		t.Fatalf("providers=%d", len(providers))
	}
	p := providers[0].(map[string]any)
	email, _ := p["email"].(string)
	if !strings.HasPrefix(email, "admin@") {
		t.Fatalf("auto email=%q", email)
	}
}

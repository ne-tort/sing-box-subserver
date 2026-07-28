//go:build with_controlplane

package materialize

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func trojanSet() domain.InboundSet {
	return domain.InboundSet{
		Name:       "edge",
		Listen:     "0.0.0.0",
		ListenPort: 443,
		Presets:    []string{"trojan-tcp"},
	}
}

func trojanUser(now time.Time) domain.User {
	return domain.User{
		Name:      "u1",
		Enabled:   true,
		CreatedAt: now,
		Creds:     map[string]map[string]any{"trojan-tcp": {"password": "secret"}},
	}
}

func TestBuildSelfSignedPaths(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	tls := domain.DefaultSelfSigned("vpn.example.com")
	raw, err := Build(Input{
		PublicHost:  "vpn.example.com",
		DataDir:     "/data",
		TLS:         tls,
		TLSCertPath: "/data/controlplane/tls/server.crt",
		TLSKeyPath:  "/data/controlplane/tls/server.key",
		ActiveSets:  []domain.InboundSet{trojanSet()},
		Users:       []domain.User{trojanUser(now)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["certificate_providers"]; ok {
		t.Fatal("self_signed must not emit certificate_providers")
	}
	ib := firstInbound(t, doc)
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj["certificate_path"] != "/data/controlplane/tls/server.crt" {
		t.Fatalf("cert path: %#v", tlsObj["certificate_path"])
	}
	if tlsObj["key_path"] != "/data/controlplane/tls/server.key" {
		t.Fatalf("key path: %#v", tlsObj["key_path"])
	}
	if _, ok := tlsObj["certificate_provider"]; ok {
		t.Fatal("unexpected certificate_provider")
	}
	if tlsObj["server_name"] != "vpn.example.com" {
		t.Fatalf("server_name=%v", tlsObj["server_name"])
	}
}

func TestBuildACMEDomainProviders(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	tls := domain.TLSProfile{
		Mode: domain.TLSModeACMEDomain,
		ACME: &domain.ACMESpec{
			Email:   "admin@example.com",
			Domains: []string{"vpn.example.com"},
			KeyType: "p256",
		},
	}
	raw, err := Build(Input{
		PublicHost: "vpn.example.com",
		DataDir:    "/var/lib/subserver",
		TLS:        tls,
		ActiveSets: []domain.InboundSet{trojanSet()},
		Users:      []domain.User{trojanUser(now)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	provs, _ := doc["certificate_providers"].([]any)
	if len(provs) != 1 {
		t.Fatalf("providers=%d", len(provs))
	}
	p, _ := provs[0].(map[string]any)
	if p["type"] != "acme" || p["tag"] != domain.TLSProviderTag {
		t.Fatalf("provider: %#v", p)
	}
	wantDir := filepath.Join("/var/lib/subserver", "controlplane", "acme")
	if p["data_directory"] != wantDir {
		t.Fatalf("data_directory=%v want %s", p["data_directory"], wantDir)
	}
	ib := firstInbound(t, doc)
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj["certificate_provider"] != domain.TLSProviderTag {
		t.Fatalf("certificate_provider=%v", tlsObj["certificate_provider"])
	}
	if _, ok := tlsObj["certificate_path"]; ok {
		t.Fatal("unexpected certificate_path for acme")
	}
}

func TestBuildACMEIPProviders(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	tls := domain.TLSProfile{
		Mode: domain.TLSModeACMEIP,
		ACME: &domain.ACMESpec{
			Email:    "admin@example.com",
			Domains:  []string{"203.0.113.10"},
			Provider: "letsencrypt",
		},
	}
	raw, err := Build(Input{
		PublicHost: "203.0.113.10",
		DataDir:    "/data",
		TLS:        tls,
		ActiveSets: []domain.InboundSet{trojanSet()},
		Users:      []domain.User{trojanUser(now)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	provs, _ := doc["certificate_providers"].([]any)
	if len(provs) != 1 {
		t.Fatalf("providers=%d", len(provs))
	}
	p, _ := provs[0].(map[string]any)
	doms, _ := p["domain"].([]any)
	if len(doms) != 1 || doms[0] != "203.0.113.10" {
		t.Fatalf("domain=%v", p["domain"])
	}
	ib := firstInbound(t, doc)
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj["server_name"] != "203.0.113.10" {
		t.Fatalf("server_name=%v", tlsObj["server_name"])
	}
	if tlsObj["certificate_provider"] != domain.TLSProviderTag {
		t.Fatalf("certificate_provider=%v", tlsObj["certificate_provider"])
	}
}

func TestBuildACMEProvidersOnlyWhenTLSPreset(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	tls := domain.TLSProfile{
		Mode: domain.TLSModeACMEDomain,
		ACME: &domain.ACMESpec{Email: "a@b.c", Domains: []string{"vpn.example.com"}},
	}
	raw, err := Build(Input{
		PublicHost: "vpn.example.com",
		DataDir:    "/data",
		TLS:        tls,
		ActiveSets: []domain.InboundSet{{
			Name: "a", Listen: "0.0.0.0", ListenPort: 8443, Presets: []string{"shadowsocks-tcp"},
		}},
		Users: []domain.User{{
			Name: "u1", Enabled: true, CreatedAt: now,
			Creds: map[string]map[string]any{"shadowsocks-tcp": {"password": "x"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["certificate_providers"]; ok {
		t.Fatal("ACME providers must not emit without TLS presets")
	}
}

func TestBuildShadowsocksNoTLS(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	raw, err := Build(Input{
		PublicHost: "1.2.3.4",
		DataDir:    "/data",
		TLS:        domain.DefaultSelfSigned("1.2.3.4"),
		ActiveSets: []domain.InboundSet{{
			Name:       "a",
			Listen:     "0.0.0.0",
			ListenPort: 8443,
			Presets:    []string{"shadowsocks-tcp"},
		}},
		Users: []domain.User{{
			Name:      "u1",
			Enabled:   true,
			CreatedAt: now,
			Creds:     map[string]map[string]any{"shadowsocks-tcp": {"password": "secret"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["certificate_providers"]; ok {
		t.Fatal("no ACME providers for self_signed without TLS presets still ok, but must be absent")
	}
	inbounds, _ := doc["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("inbounds=%d", len(inbounds))
	}
}

func TestRenderSubscriptionInsecureOnlySelfSigned(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	sets := []domain.InboundSet{trojanSet()}
	user := trojanUser(now)
	user.SubToken = "tok"

	body, err := RenderSubscription(user, sets, "vpn.example.com", domain.DefaultSelfSigned("vpn.example.com"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	assertSubInsecure(t, body, true)

	acme := domain.TLSProfile{
		Mode: domain.TLSModeACMEDomain,
		ACME: &domain.ACMESpec{Email: "a@b.c", Domains: []string{"vpn.example.com"}},
	}
	body, err = RenderSubscription(user, sets, "vpn.example.com", acme, "", "")
	if err != nil {
		t.Fatal(err)
	}
	assertSubInsecure(t, body, false)
}

func firstInbound(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	inbounds, _ := doc["inbounds"].([]any)
	if len(inbounds) == 0 {
		t.Fatal("no inbounds")
	}
	ib, _ := inbounds[0].(map[string]any)
	return ib
}

func assertSubInsecure(t *testing.T, body []byte, want bool) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	outs, _ := doc["outbounds"].([]any)
	if len(outs) == 0 {
		t.Fatal("no outbounds")
	}
	ob, _ := outs[0].(map[string]any)
	tlsObj, _ := ob["tls"].(map[string]any)
	_, has := tlsObj["insecure"]
	if has != want {
		t.Fatalf("insecure present=%v want=%v tls=%#v", has, want, tlsObj)
	}
	if want && tlsObj["insecure"] != true {
		t.Fatalf("insecure value=%v", tlsObj["insecure"])
	}
}

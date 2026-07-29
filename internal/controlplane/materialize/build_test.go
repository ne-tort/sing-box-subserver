//go:build with_controlplane

package materialize

import (
	"encoding/json"
	"fmt"
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

func TestBuildShadowsocksEmptyUsersInertPassword(t *testing.T) {
	t.Parallel()
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
		Users: nil, // all ineligible → empty
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	inbounds, _ := doc["inbounds"].([]any)
	if len(inbounds) != 1 {
		t.Fatalf("inbounds=%v", inbounds)
	}
	ib := inbounds[0].(map[string]any)
	pw, _ := ib["password"].(string)
	if pw == "" || pw == "cp-no-eligible-users" || len(pw) < 16 {
		t.Fatalf("want random inert password, got %q", pw)
	}
	if _, ok := ib["users"]; ok {
		t.Fatal("users must be omitted for inert SS")
	}
}

func TestBuildSocksEmptyUsersFailClosed(t *testing.T) {
	t.Parallel()
	raw, err := Build(Input{
		PublicHost: "1.2.3.4",
		DataDir:    "/data",
		TLS:        domain.DefaultSelfSigned("1.2.3.4"),
		ActiveSets: []domain.InboundSet{{
			Name: "a", Listen: "0.0.0.0", ListenPort: 1080, Presets: []string{"socks"},
		}},
		Users: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	ib := doc["inbounds"].([]any)[0].(map[string]any)
	users, _ := ib["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("want inert socks user, got %v", users)
	}
	u := users[0].(map[string]any)
	if u["username"] != "cp-inert" {
		t.Fatalf("%v", u)
	}
	pw, _ := u["password"].(string)
	if len(pw) < 16 {
		t.Fatalf("weak inert password %q", pw)
	}
}

func TestBuildTrojanEmptyUsersInert(t *testing.T) {
	t.Parallel()
	raw, err := Build(Input{
		PublicHost: "1.2.3.4",
		DataDir:    "/data",
		TLS:        domain.DefaultSelfSigned("1.2.3.4"),
		TLSCertPath: "/data/cert.pem",
		TLSKeyPath:  "/data/key.pem",
		ActiveSets: []domain.InboundSet{{
			Name: "a", Listen: "0.0.0.0", ListenPort: 443, Presets: []string{"trojan-tcp"},
		}},
		Users: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	ib := doc["inbounds"].([]any)[0].(map[string]any)
	users, _ := ib["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("want inert trojan user, got %v", users)
	}
	u := users[0].(map[string]any)
	if u["name"] != "cp-inert" {
		t.Fatalf("%v", u)
	}
	pw, _ := u["password"].(string)
	if len(pw) < 16 {
		t.Fatalf("weak inert password %q", pw)
	}
}

func TestBuildVlessEmptyUsersInert(t *testing.T) {
	t.Parallel()
	raw, err := Build(Input{
		PublicHost:  "1.2.3.4",
		DataDir:     "/data",
		TLS:         domain.DefaultSelfSigned("1.2.3.4"),
		TLSCertPath: "/data/cert.pem",
		TLSKeyPath:  "/data/key.pem",
		ActiveSets: []domain.InboundSet{{
			Name: "a", Listen: "0.0.0.0", ListenPort: 443, Presets: []string{"vless-tcp"},
		}},
		Users: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	ib := doc["inbounds"].([]any)[0].(map[string]any)
	users, _ := ib["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("want inert vless user, got %v", users)
	}
	u := users[0].(map[string]any)
	if u["name"] != "cp-inert" {
		t.Fatalf("%v", u)
	}
	id, _ := u["uuid"].(string)
	if len(id) < 32 {
		t.Fatalf("bad inert uuid %q", id)
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

	body, err := RenderSubscription(user, sets, "vpn.example.com", domain.DefaultSelfSigned("vpn.example.com"), SubscriptionFilters{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSubInsecure(t, body, true)

	acme := domain.TLSProfile{
		Mode: domain.TLSModeACMEDomain,
		ACME: &domain.ACMESpec{Email: "a@b.c", Domains: []string{"vpn.example.com"}},
	}
	body, err = RenderSubscription(user, sets, "vpn.example.com", acme, SubscriptionFilters{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSubInsecure(t, body, false)
}

func TestRenderSubscriptionPresetFilterMulti(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "mixed", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"trojan-tcp", "vless-tcp", "shadowsocks-tcp"},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"trojan-tcp":      {"password": "t"},
			"vless-tcp":       {"uuid": "11111111-2222-3333-4444-555555555555", "uuid_xtls": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
			"shadowsocks-tcp": {"password": "s"},
		},
	}
	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"), SubscriptionFilters{Presets: []string{"vless-tcp", "trojan-tcp"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	outs, _ := doc["outbounds"].([]any)
	if len(outs) != 3 {
		t.Fatalf("outbounds=%d want 2 body=%s", len(outs), body)
	}
	tags := map[string]bool{}
	for _, o := range outs {
		m, _ := o.(map[string]any)
		tags[fmt.Sprint(m["tag"])] = true
	}
	if !tags["cp-out-mixed-vless-tcp-none"] || !tags["cp-out-mixed-vless-tcp-xtls-rprx-vision"] || !tags["cp-out-mixed-trojan-tcp"] {
		t.Fatalf("tags=%v", tags)
	}
	if tags["cp-out-mixed-shadowsocks-tcp"] {
		t.Fatal("shadowsocks should be filtered out")
	}
}

func TestBuildAndSubscriptionReality(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "r1", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"vless-reality-tcp"},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"vless-reality-tcp": {
				"uuid":      "11111111-2222-3333-4444-555555555555",
				"uuid_xtls": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			},
		},
	}
	assignments := map[string]domain.RealityAssignment{
		"r1/vless-reality-tcp": {
			InboundKey:       "r1/vless-reality-tcp",
			SNI:              "www.microsoft.com",
			HandshakeServer:  "www.microsoft.com",
			HandshakePort:    443,
			PrivateKeyBase64: "Mzi3RBq4Eb3L-ic-8z9yqV3Xcg7G7xUqKdEH7DKn-1Q",
			PublicKeyBase64:  "jQfCMZZk0RwJQK1qlf0LUFUphdE4jE6JIutIlAzxPVo",
			ShortID:          "aabbccddeeff0011",
			UpdatedAt:        now,
		},
	}
	raw, err := Build(Input{
		PublicHost:         "198.51.100.10",
		DataDir:            "/data",
		TLS:                domain.DefaultSelfSigned("198.51.100.10"),
		TLSCertPath:        "/data/controlplane/tls/server.crt",
		TLSKeyPath:         "/data/controlplane/tls/server.key",
		ActiveSets:         []domain.InboundSet{set},
		Users:              []domain.User{user},
		RealityAssignments: assignments,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj["server_name"] != "www.microsoft.com" {
		t.Fatalf("inbound reality server_name=%v", tlsObj["server_name"])
	}
	realityObj, _ := tlsObj["reality"].(map[string]any)
	if realityObj["private_key"] != assignments["r1/vless-reality-tcp"].PrivateKeyBase64 {
		t.Fatalf("private_key=%v", realityObj["private_key"])
	}
	body, err := RenderSubscription(user, []domain.InboundSet{set}, "198.51.100.10", domain.DefaultSelfSigned("198.51.100.10"), SubscriptionFilters{}, assignments)
	if err != nil {
		t.Fatal(err)
	}
	var sub map[string]any
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatal(err)
	}
	outs, _ := sub["outbounds"].([]any)
	if len(outs) != 2 {
		t.Fatalf("outbounds=%d", len(outs))
	}
	foundFlowNone, foundFlowX := false, false
	for _, o := range outs {
		ob, _ := o.(map[string]any)
		otls, _ := ob["tls"].(map[string]any)
		if otls["server_name"] != "www.microsoft.com" {
			t.Fatalf("sub server_name=%v", otls["server_name"])
		}
		if ob["flow"] != nil {
			foundFlowX = true
			if ob["flow"] != "xtls-rprx-vision" {
				t.Fatalf("flow=%v", ob["flow"])
			}
		} else {
			foundFlowNone = true
		}
		realityOut, _ := otls["reality"].(map[string]any)
		if realityOut["public_key"] != assignments["r1/vless-reality-tcp"].PublicKeyBase64 {
			t.Fatalf("public_key=%v", realityOut["public_key"])
		}
	}
	if !foundFlowNone || !foundFlowX {
		t.Fatalf("expected both flow variants; none=%v xtls=%v outs=%#v", foundFlowNone, foundFlowX, outs)
	}
}

func TestRenderSubscriptionVlessFlowAndNetworkFilters(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "v1", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"vless-tcp"},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"vless-tcp": {
				"uuid":      "11111111-2222-3333-4444-555555555555",
				"uuid_xtls": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			},
		},
	}

	t.Run("flow xtls only", func(t *testing.T) {
		body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"),
			SubscriptionFilters{Presets: []string{"vless-tcp"}, Flow: []string{"xtls-rprx-vision"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		outs, _ := doc["outbounds"].([]any)
		if len(outs) != 1 {
			t.Fatalf("outbounds=%d want 1", len(outs))
		}
		ob, _ := outs[0].(map[string]any)
		if ob["tag"] != "cp-out-v1-vless-tcp-xtls-rprx-vision" {
			t.Fatalf("tag=%v", ob["tag"])
		}
		if ob["flow"] != "xtls-rprx-vision" {
			t.Fatalf("flow=%v", ob["flow"])
		}
		if ob["uuid"] != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
			t.Fatalf("uuid=%v", ob["uuid"])
		}
	})

	t.Run("flow none only", func(t *testing.T) {
		body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"),
			SubscriptionFilters{Presets: []string{"vless-tcp"}, Flow: []string{"none"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		outs, _ := doc["outbounds"].([]any)
		if len(outs) != 1 {
			t.Fatalf("outbounds=%d want 1", len(outs))
		}
		ob, _ := outs[0].(map[string]any)
		if ob["tag"] != "cp-out-v1-vless-tcp-none" {
			t.Fatalf("tag=%v", ob["tag"])
		}
		if ob["flow"] != nil {
			t.Fatalf("flow=%v (expected nil)", ob["flow"])
		}
		if ob["uuid"] != "11111111-2222-3333-4444-555555555555" {
			t.Fatalf("uuid=%v", ob["uuid"])
		}
	})

	t.Run("network udp", func(t *testing.T) {
		body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"),
			SubscriptionFilters{Presets: []string{"vless-tcp"}, Flow: []string{"xtls-rprx-vision"}, Network: "udp"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		outs, _ := doc["outbounds"].([]any)
		if len(outs) != 1 {
			t.Fatalf("outbounds=%d want 1", len(outs))
		}
		ob, _ := outs[0].(map[string]any)
		if ob["network"] != "udp" {
			t.Fatalf("network=%v", ob["network"])
		}
	})
}

func TestRenderSubscriptionVariantTagProfileFilters(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name:       "vb",
		Listen:     "0.0.0.0",
		ListenPort: 443,
		Bindings: []domain.SetBinding{
			{
				Preset:                "vless-tcp",
				SubscriptionTags:      []string{"mobile", "stable"},
				EnabledUserVariants:   []string{"flow-none", "flow-udp-vision"},
				EnabledClientProfiles: []string{"profile-mobile"},
			},
		},
		Presets: []string{"vless-tcp"},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"vless-tcp": {
				"uuid":     "11111111-2222-3333-4444-555555555555",
				"uuid_udp": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			},
		},
	}

	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"),
		SubscriptionFilters{Variants: []string{"flow-udp-vision"}, Tags: []string{"mobile"}, Profiles: []string{"profile-mobile"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	outs, _ := doc["outbounds"].([]any)
	if len(outs) != 1 {
		t.Fatalf("outbounds=%d want 1 body=%s", len(outs), body)
	}
	ob, _ := outs[0].(map[string]any)
	if ob["flow"] != "xtls-rprx-vision-udp443" {
		t.Fatalf("flow=%v", ob["flow"])
	}
	if ob["uuid"] != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("uuid=%v", ob["uuid"])
	}
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

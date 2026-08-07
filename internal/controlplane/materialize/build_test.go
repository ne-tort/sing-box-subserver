//go:build with_controlplane

package materialize

import (
	"encoding/json"
	"fmt"
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
		TLSCertPath: "/data/controlplane/ssl/default/cert.crt",
		TLSKeyPath:  "/data/controlplane/ssl/default/cert.key",
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
	if tlsObj["certificate_path"] != "/data/controlplane/ssl/default/cert.crt" {
		t.Fatalf("cert path: %#v", tlsObj["certificate_path"])
	}
	if tlsObj["key_path"] != "/data/controlplane/ssl/default/cert.key" {
		t.Fatalf("key path: %#v", tlsObj["key_path"])
	}
	if _, ok := tlsObj["certificate_provider"]; ok {
		t.Fatal("unexpected certificate_provider")
	}
	if tlsObj["server_name"] != "vpn.example.com" {
		t.Fatalf("server_name=%v", tlsObj["server_name"])
	}
}

func TestBuildCustomOutbounds(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	tls := domain.DefaultSelfSigned("vpn.example.com")
	out := []byte(`[{"type":"direct","tag":"direct"},{"type":"block","tag":"block"},{"type":"socks","tag":"socks-out","server":"127.0.0.1","server_port":1080}]`)
	raw, err := Build(Input{
		PublicHost:  "vpn.example.com",
		DataDir:     "/data",
		TLS:         tls,
		TLSCertPath: "/data/controlplane/ssl/default/cert.crt",
		TLSKeyPath:  "/data/controlplane/ssl/default/cert.key",
		ActiveSets:  []domain.InboundSet{trojanSet()},
		Users:       []domain.User{trojanUser(now)},
		Outbounds:   out,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	outs, _ := doc["outbounds"].([]any)
	if len(outs) != 3 {
		t.Fatalf("outbounds len=%d", len(outs))
	}
	last, _ := outs[2].(map[string]any)
	if last["tag"] != "socks-out" {
		t.Fatalf("last outbound=%v", last)
	}
}





func TestBuildDefaultPEMWithoutSNI(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	raw, err := Build(Input{
		PublicHost:  "vpn.example.com",
		DataDir:     "/data",
		TLS:         domain.DefaultSelfSigned("vpn.example.com"),
		TLSCertPath: "/data/c.pem",
		TLSKeyPath:  "/data/k.pem",
			ActiveSets:  []domain.InboundSet{trojanSet()},
		Users:       []domain.User{trojanUser(now)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	ib := firstInbound(t, doc)
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj["certificate_path"] != "/data/c.pem" {
		t.Fatalf("expected PEM path, got %#v", tlsObj)
	}
	if _, ok := tlsObj["certificate_provider"]; ok {
		t.Fatal("unexpected provider without params.sni")
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

	body, err := RenderSubscription(user, sets, "vpn.example.com", domain.DefaultSelfSigned("vpn.example.com"), SubscriptionFilters{
		SSLProfiles: map[string]domain.SSLProfile{
			"default": {ID: "default", Name: "Default", Type: domain.SSLTypeSelfSigned, Domain: "vpn.example.com"},
		},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSubInsecure(t, body, true)

	setsACME := []domain.InboundSet{{
		Name: "a", Listen: "0.0.0.0", ListenPort: 443,
		Bindings: []domain.SetBinding{{Preset: "trojan-tcp", Params: map[string]string{domain.BindingParamSSLProfile: "acme1"}}},
	}}
	userACME := trojanUser(now)
	userACME.SubToken = "tok"
	body, err = RenderSubscription(userACME, setsACME, "vpn.example.com", domain.DefaultSelfSigned("vpn.example.com"), SubscriptionFilters{
		SSLProfiles: map[string]domain.SSLProfile{
			"acme1": {ID: "acme1", Name: "vpn", Type: domain.SSLTypeACME, Domain: "vpn.example.com", Email: "a@b.c"},
		},
	}, nil, nil)
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
	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"), SubscriptionFilters{Presets: []string{"vless-tcp", "trojan-tcp"}}, nil, nil)
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

func TestBuildRealityIgnoresParamsSNI(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "r1", Listen: "0.0.0.0", ListenPort: 443,
		Bindings: []domain.SetBinding{{
			Preset: "vless-reality-tcp",
			Params: map[string]string{domain.BindingParamSSLProfile: "vpn.example.com"},
		}},
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
		"r1/vless_reality": {
			InboundKey:       "r1/vless_reality",
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
		TLSCertPath:        "/data/c.pem",
		TLSKeyPath:         "/data/k.pem",
			ActiveSets:         []domain.InboundSet{set},
		Users:              []domain.User{user},
		RealityAssignments: assignments,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	ib := firstInbound(t, doc)
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj["server_name"] != "www.microsoft.com" {
		t.Fatalf("Reality must use assignment SNI, got %v", tlsObj["server_name"])
	}
	if _, ok := tlsObj["certificate_provider"]; ok {
		t.Fatal("Reality must not use certificate_provider")
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
		"r1/vless_reality": {
			InboundKey:       "r1/vless_reality",
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
		TLSCertPath:        "/data/controlplane/ssl/default/cert.crt",
		TLSKeyPath:         "/data/controlplane/ssl/default/cert.key",
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
	if realityObj["private_key"] != assignments["r1/vless_reality"].PrivateKeyBase64 {
		t.Fatalf("private_key=%v", realityObj["private_key"])
	}
	body, err := RenderSubscription(user, []domain.InboundSet{set}, "198.51.100.10", domain.DefaultSelfSigned("198.51.100.10"), SubscriptionFilters{}, assignments, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sub map[string]any
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatal(err)
	}
	outs, _ := sub["outbounds"].([]any)
	if len(outs) != 1 {
		t.Fatalf("outbounds=%d want 1 (default_user_variants flow-none)", len(outs))
	}
	ob, _ := outs[0].(map[string]any)
	otls, _ := ob["tls"].(map[string]any)
	if otls["server_name"] != "www.microsoft.com" {
		t.Fatalf("sub server_name=%v", otls["server_name"])
	}
	if ob["flow"] != nil && ob["flow"] != "" {
		t.Fatalf("expected flow-none (empty flow), got %v", ob["flow"])
	}
	realityOut, _ := otls["reality"].(map[string]any)
	if realityOut["public_key"] != assignments["r1/vless_reality"].PublicKeyBase64 {
		t.Fatalf("public_key=%v", realityOut["public_key"])
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
		body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"), SubscriptionFilters{Presets: []string{"vless-tcp"}, Flow: []string{"xtls-rprx-vision"}}, nil, nil)
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
		body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"), SubscriptionFilters{Presets: []string{"vless-tcp"}, Flow: []string{"none"}}, nil, nil)
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
		body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"), SubscriptionFilters{Presets: []string{"vless-tcp"}, Flow: []string{"xtls-rprx-vision"}, Network: "udp"}, nil, nil)
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
				EnabledClientProfiles: []string{"pkt-none", "pkt-xudp"},
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

	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"), SubscriptionFilters{Variants: []string{"flow-udp-vision"}, Tags: []string{"mobile"}, Profiles: []string{"pkt-xudp"}}, nil, nil)
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
	if ob["packet_encoding"] != "xudp" {
		t.Fatalf("packet_encoding=%v want xudp from pkt-xudp profile", ob["packet_encoding"])
	}

	bodyNone, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", domain.DefaultSelfSigned("h.example"), SubscriptionFilters{Variants: []string{"flow-none"}, Profiles: []string{"pkt-none"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var docNone map[string]any
	if err := json.Unmarshal(bodyNone, &docNone); err != nil {
		t.Fatal(err)
	}
	outsNone, _ := docNone["outbounds"].([]any)
	if len(outsNone) != 1 {
		t.Fatalf("pkt-none outbounds=%d body=%s", len(outsNone), bodyNone)
	}
	obNone, _ := outsNone[0].(map[string]any)
	if _, ok := obNone["packet_encoding"]; ok {
		t.Fatalf("pkt-none must clear packet_encoding, got %v", obNone["packet_encoding"])
	}
}

func TestMaterializeVlessHysteriaTransport(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "hy1", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"vless_hysteria_tls"},
		PeerSecrets: map[string]string{
			"vless_hysteria_tls/hy_auth": "shared-hy-auth",
		},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"vless_hysteria_tls": {"uuid": "11111111-2222-3333-4444-555555555555"},
		},
	}
	tls := domain.DefaultSelfSigned("h.example")
	raw, err := Build(Input{
		PublicHost:  "h.example",
		DataDir:     "/data",
		TLS:         tls,
		TLSCertPath: "/data/controlplane/ssl/default/cert.crt",
		TLSKeyPath:  "/data/controlplane/ssl/default/cert.key",
		ActiveSets:  []domain.InboundSet{set},
		Users:       []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	tr, _ := ib["transport"].(map[string]any)
	if tr["type"] != "hysteria" {
		t.Fatalf("transport=%v", tr)
	}
	if tr["password"] != "shared-hy-auth" {
		t.Fatalf("password=%v", tr["password"])
	}
	if tr["version"] != float64(2) {
		t.Fatalf("version=%v", tr["version"])
	}
	users, _ := ib["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("users=%d want 1 (flow-none only)", len(users))
	}

	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", tls, SubscriptionFilters{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sub map[string]any
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatal(err)
	}
	outs, _ := sub["outbounds"].([]any)
	if len(outs) != 1 {
		t.Fatalf("outbounds=%d", len(outs))
	}
	ob, _ := outs[0].(map[string]any)
	otr, _ := ob["transport"].(map[string]any)
	if otr["type"] != "hysteria" || otr["password"] != "shared-hy-auth" {
		t.Fatalf("out transport=%v", otr)
	}
	if ob["packet_encoding"] != "xudp" {
		t.Fatalf("packet_encoding=%v", ob["packet_encoding"])
	}
}

func TestMaterializeVlessWSTransportDefaults(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "w1", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"vless_ws_tls"},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"vless_ws_tls": {"uuid": "11111111-2222-3333-4444-555555555555"},
		},
	}
	tls := domain.DefaultSelfSigned("h.example")
	raw, err := Build(Input{
		PublicHost:  "h.example",
		DataDir:     "/data",
		TLS:         tls,
		TLSCertPath: "/data/controlplane/ssl/default/cert.crt",
		TLSKeyPath:  "/data/controlplane/ssl/default/cert.key",
		ActiveSets:  []domain.InboundSet{set},
		Users:       []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	tr, _ := ib["transport"].(map[string]any)
	if tr["type"] != "ws" {
		t.Fatalf("transport=%v", tr)
	}
	if _, ok := tr["host"]; ok {
		t.Fatalf("inbound ws must not set transport.host: %#v", tr)
	}
	if path, _ := tr["path"].(string); path != "/vless-ws" {
		t.Fatalf("ws path=%v", tr["path"])
	}
	users, _ := ib["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("users=%d want 1 (flow-none only)", len(users))
	}
	u, _ := users[0].(map[string]any)
	if u["name"] != "u1-flow-none" {
		t.Fatalf("user name=%v", u["name"])
	}
	if _, ok := u["flow"]; ok {
		t.Fatalf("flow should be absent: %v", u)
	}

	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", tls, SubscriptionFilters{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sub map[string]any
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatal(err)
	}
	outs, _ := sub["outbounds"].([]any)
	if len(outs) != 1 {
		t.Fatalf("outbounds=%d", len(outs))
	}
	ob, _ := outs[0].(map[string]any)
	if ob["packet_encoding"] != "xudp" {
		t.Fatalf("packet_encoding=%v", ob["packet_encoding"])
	}
	otr, _ := ob["transport"].(map[string]any)
	if otr["type"] != "ws" {
		t.Fatalf("out transport=%v", otr)
	}
}

func TestMaterializeSS2022SubscriptionCombinedPassword(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "s1", Listen: "0.0.0.0", ListenPort: 8388,
		Presets: []string{"ss_2022_aes128"},
		PeerSecrets: map[string]string{
			"ss_2022_aes128/password": "AAAAAAAAAAAAAAAAAAAAAA==",
		},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"ss_2022_aes128": {"password": "BBBBBBBBBBBBBBBBBBBBBB=="},
		},
	}
	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example",
		domain.DefaultSelfSigned("h.example"), SubscriptionFilters{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	outs, _ := doc["outbounds"].([]any)
	if len(outs) != 1 {
		t.Fatalf("outbounds=%d", len(outs))
	}
	ob, _ := outs[0].(map[string]any)
	want := "AAAAAAAAAAAAAAAAAAAAAA==:BBBBBBBBBBBBBBBBBBBBBB=="
	if ob["password"] != want {
		t.Fatalf("password=%v want %s", ob["password"], want)
	}
}

func TestPeerSecretsLookupUsesCanonicalPreset(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "h1", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"hysteria2-salamander"}, // alias
		PeerSecrets: map[string]string{
			"hy2_salamander/obfs_password": "obfs-secret",
		},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"hy2_salamander": {"password": "user-secret"},
		},
	}
	tls := domain.DefaultSelfSigned("h.example")
	raw, err := Build(Input{
		PublicHost: "h.example", DataDir: "/data", TLS: tls,
		TLSCertPath: "/data/c.crt", TLSKeyPath: "/data/c.key",
		ActiveSets: []domain.InboundSet{set}, Users: []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	obfs, _ := ib["obfs"].(map[string]any)
	if obfs["password"] != "obfs-secret" {
		t.Fatalf("obfs password=%v (alias peer lookup failed)", obfs["password"])
	}
}

func TestMaterializeSS2022KeepsServerPassword(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "s1", Listen: "0.0.0.0", ListenPort: 8388,
		Presets: []string{"ss_2022_aes128"},
		PeerSecrets: map[string]string{
			"ss_2022_aes128/password": "AAAAAAAAAAAAAAAAAAAAAA==",
		},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"ss_2022_aes128": {"password": "BBBBBBBBBBBBBBBBBBBBBB=="},
		},
	}
	raw, err := Build(Input{
		PublicHost: "h.example", DataDir: "/data",
		TLS: domain.DefaultSelfSigned("h.example"),
		TLSCertPath: "/data/c.crt", TLSKeyPath: "/data/c.key",
		ActiveSets: []domain.InboundSet{set},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	if ib["password"] != "AAAAAAAAAAAAAAAAAAAAAA==" {
		t.Fatalf("server password=%v", ib["password"])
	}
	users, _ := ib["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("users=%d", len(users))
	}
	u, _ := users[0].(map[string]any)
	if u["password"] != "BBBBBBBBBBBBBBBBBBBBBB==" {
		t.Fatalf("user password=%v", u["password"])
	}
}

func TestMaterializeSSHUserField(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "ssh1", Listen: "0.0.0.0", ListenPort: 2222,
		Presets: []string{"ssh_password"},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"ssh_password": {"username": "proxy", "password": "secret"},
		},
	}
	raw, err := Build(Input{
		PublicHost: "h.example", DataDir: "/data",
		TLS: domain.DefaultSelfSigned("h.example"),
		TLSCertPath: "/data/c.crt", TLSKeyPath: "/data/c.key",
		ActiveSets: []domain.InboundSet{set},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	users, _ := ib["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("users=%d", len(users))
	}
	u, _ := users[0].(map[string]any)
	if u["user"] != "proxy" {
		t.Fatalf("user field=%v want proxy", u)
	}
	if _, ok := u["username"]; ok {
		t.Fatalf("username should be remapped: %v", u)
	}
}

func TestMaterializeDERPKeys(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "d1", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"derp_tls"},
		PeerSecrets: map[string]string{
			"derp_tls/private_key": "kKk88zAh-UZ7N8zImwQHwyaDORp8x2kLZal7jn9cZWE",
			"derp_tls/public_key":  "gxQHX0NUGnGc4-DlOSw47u4Q1DlGpCfQRAcGACNerVk",
		},
	}
	user := domain.User{
		Name: "alice", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"derp_tls": {
				"private_key": "GFDczwT26dgQecCGugg8VhBy4NE57Z5zg4iqzCuicF8",
				"public_key":  "H2ozVoKXvYy7WKG1lec0LfEepCt2FphorktglinGyws",
			},
		},
	}
	tls := domain.DefaultSelfSigned("h.example")
	raw, err := Build(Input{
		PublicHost: "h.example", DataDir: "/data", TLS: tls,
		TLSCertPath: "/data/c.crt", TLSKeyPath: "/data/c.key",
		ActiveSets: []domain.InboundSet{set},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	if ib["private_key"] != "kKk88zAh-UZ7N8zImwQHwyaDORp8x2kLZal7jn9cZWE" {
		t.Fatalf("inbound private_key=%v", ib["private_key"])
	}
	users, _ := ib["users"].([]any)
	u, _ := users[0].(map[string]any)
	if u["public_key"] != "H2ozVoKXvYy7WKG1lec0LfEepCt2FphorktglinGyws" {
		t.Fatalf("user public_key=%v", u)
	}
	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", tls, SubscriptionFilters{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sub map[string]any
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatal(err)
	}
	ob, _ := sub["outbounds"].([]any)[0].(map[string]any)
	if ob["private_key"] != "GFDczwT26dgQecCGugg8VhBy4NE57Z5zg4iqzCuicF8" {
		t.Fatalf("out private_key=%v", ob["private_key"])
	}
	if ob["peer_public_key"] != "gxQHX0NUGnGc4-DlOSw47u4Q1DlGpCfQRAcGACNerVk" {
		t.Fatalf("peer_public_key=%v", ob["peer_public_key"])
	}
}

func TestMaterializeSudokuSharedKeyNoUsers(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "su1", Listen: "0.0.0.0", ListenPort: 8443,
		Presets: []string{"sudoku_pad"},
		PeerSecrets: map[string]string{
			"sudoku_pad/key": "shared-sudoku-key",
		},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"sudoku_pad": {"key": "ignored-user-key"},
		},
	}
	raw, err := Build(Input{
		PublicHost: "h.example", DataDir: "/data",
		TLS: domain.DefaultSelfSigned("h.example"),
		ActiveSets: []domain.InboundSet{set},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	if ib["key"] != "shared-sudoku-key" {
		t.Fatalf("key=%v", ib["key"])
	}
	if _, ok := ib["users"]; ok {
		t.Fatalf("shared_key should omit users: %v", ib["users"])
	}
}

func TestMaterializeCarrierJitsiParamsAndSubscription(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	room := "https://meet.jit.si/lx-carrier-cp-room"
	set := domain.InboundSet{
		Name: "cj1", Listen: "0.0.0.0", ListenPort: 9443,
		Bindings: []domain.SetBinding{{
			Preset: "carrier_jitsi_shared",
			Params: map[string]string{"room": room},
		}},
		PeerSecrets: map[string]string{
			"carrier_jitsi_shared/password": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"carrier_jitsi_shared": {"device_id": "dev-1"},
		},
	}
	tls := domain.DefaultSelfSigned("h.example")
	raw, err := Build(Input{
		PublicHost: "h.example", DataDir: "/data", TLS: tls,
		ActiveSets: []domain.InboundSet{set},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	if ib["provider"] != "jitsi" {
		t.Fatalf("provider=%v", ib["provider"])
	}
	if _, ok := ib["listen_port"]; ok {
		t.Fatalf("SFU inbound should not bind listen_port: %v", ib["listen_port"])
	}
	link, _ := ib["link"].(map[string]any)
	if link["room"] != room {
		t.Fatalf("inbound room=%v", link["room"])
	}
	if _, ok := link["token"]; ok {
		t.Fatalf("empty token should be pruned: %v", link)
	}
	if _, ok := ib["users"]; ok {
		t.Fatalf("shared_auth should omit users: %v", ib["users"])
	}

	body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", tls, SubscriptionFilters{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sub map[string]any
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatal(err)
	}
	ob, _ := sub["outbounds"].([]any)[0].(map[string]any)
	if _, ok := ob["server"]; ok {
		t.Fatalf("carrier outbound must not set top-level server: %v", ob["server"])
	}
	olink, _ := ob["link"].(map[string]any)
	if olink["room"] != room {
		t.Fatalf("sub room=%v", olink["room"])
	}
	if olink["device_id"] != "dev-1" {
		t.Fatalf("device_id=%v", olink["device_id"])
	}
	if olink["password"] != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("password=%v", olink["password"])
	}
}

func TestMaterializeCarrierPeerSyncListen(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "cp1", Listen: "0.0.0.0", ListenPort: 9443,
		Presets: []string{"carrier_peer_shared"},
		PeerSecrets: map[string]string{
			"carrier_peer_shared/password": "shared-pass",
		},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"carrier_peer_shared": {"device_id": "dev-peer"},
		},
	}
	raw, err := Build(Input{
		PublicHost: "edge.example", DataDir: "/data",
		TLS: domain.DefaultSelfSigned("edge.example"),
		ActiveSets: []domain.InboundSet{set},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	link, _ := ib["link"].(map[string]any)
	if link["peer"] != "0.0.0.0:9443" {
		t.Fatalf("peer=%v", link["peer"])
	}
	body, err := RenderSubscription(user, []domain.InboundSet{set}, "edge.example",
		domain.DefaultSelfSigned("edge.example"), SubscriptionFilters{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sub map[string]any
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatal(err)
	}
	ob, _ := sub["outbounds"].([]any)[0].(map[string]any)
	olink, _ := ob["link"].(map[string]any)
	if olink["peer"] != "edge.example:9443" {
		t.Fatalf("out peer=%v", olink["peer"])
	}
}

func TestMaterializeCloudflaredNoSubAndSSHPubkey(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	cfSet := domain.InboundSet{
		Name: "cf1", Listen: "0.0.0.0", ListenPort: 8080,
		Bindings: []domain.SetBinding{{
			Preset: "cloudflared_token",
			Params: map[string]string{"token": "eyJ.test.token"},
		}},
	}
	sshSet := domain.InboundSet{
		Name: "ssh1", Listen: "0.0.0.0", ListenPort: 2222,
		Presets: []string{"ssh_pubkey"},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"ssh_pubkey": {
				"username":    "alice",
				"private_key": "-----BEGIN OPENSSH PRIVATE KEY-----\nTEST\n-----END OPENSSH PRIVATE KEY-----",
				"public_key":  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest alice",
			},
		},
	}
	raw, err := Build(Input{
		PublicHost: "h.example", DataDir: "/data",
		TLS: domain.DefaultSelfSigned("h.example"),
		ActiveSets: []domain.InboundSet{cfSet, sshSet},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	inbounds, _ := doc["inbounds"].([]any)
	if len(inbounds) != 2 {
		t.Fatalf("inbounds=%d", len(inbounds))
	}
	foundCF, foundSSH := false, false
	for _, rawIB := range inbounds {
		ib, _ := rawIB.(map[string]any)
		switch ib["type"] {
		case "cloudflared":
			foundCF = true
			if ib["token"] != "eyJ.test.token" {
				t.Fatalf("token=%v", ib["token"])
			}
			if _, ok := ib["listen_port"]; ok {
				t.Fatal("cloudflared should not listen")
			}
		case "ssh":
			foundSSH = true
			users, _ := ib["users"].([]any)
			u, _ := users[0].(map[string]any)
			if u["public_key"] == nil || u["user"] != "alice" {
				t.Fatalf("ssh user=%v", u)
			}
		}
	}
	if !foundCF || !foundSSH {
		t.Fatalf("cf=%v ssh=%v", foundCF, foundSSH)
	}
	body, err := RenderSubscription(user, []domain.InboundSet{cfSet, sshSet}, "h.example",
		domain.DefaultSelfSigned("h.example"), SubscriptionFilters{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var sub map[string]any
	if err := json.Unmarshal(body, &sub); err != nil {
		t.Fatal(err)
	}
	outs, _ := sub["outbounds"].([]any)
	if len(outs) != 1 {
		t.Fatalf("expected only ssh outbound, got %d", len(outs))
	}
	ob, _ := outs[0].(map[string]any)
	if ob["type"] != "ssh" {
		t.Fatalf("type=%v", ob["type"])
	}
}

func TestMaterializeBindingParamDefaults(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "sq1", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"shadowquic_jls"},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"shadowquic_jls": {"username": "alice", "password": "secret"},
		},
	}
	raw, err := Build(Input{
		PublicHost: "edge.example", DataDir: "/data",
		TLS: domain.DefaultSelfSigned("edge.example"),
		ActiveSets: []domain.InboundSet{set},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ib := firstInbound(t, doc)
	jls, _ := ib["jls_upstream"].(map[string]any)
	if jls["addr"] != "www.cloudflare.com:443" {
		t.Fatalf("jls addr default=%v", jls["addr"])
	}
	if jls["server_name"] != "www.cloudflare.com" {
		t.Fatalf("jls sni default=%v", jls["server_name"])
	}
}

func TestMaterializeBindingParamOverrideAndWSPath(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	sq := domain.InboundSet{
		Name: "sq2", Listen: "0.0.0.0", ListenPort: 8443,
		Bindings: []domain.SetBinding{{
			Preset: "shadowquic_jls",
			Params: map[string]string{
				"jls_addr":        "cdn.example:443",
				"jls_server_name": "cdn.example",
			},
		}},
	}
	ws := domain.InboundSet{
		Name: "ws1", Listen: "0.0.0.0", ListenPort: 443,
		Presets: []string{"vless_ws_tls"},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"shadowquic_jls": {"username": "alice", "password": "secret"},
			"vless_ws_tls":   {"uuid": "11111111-1111-1111-1111-111111111111"},
		},
	}
	raw, err := Build(Input{
		PublicHost: "edge.example", DataDir: "/data",
		TLS:         domain.DefaultSelfSigned("edge.example"),
		TLSCertPath: "/data/c.crt", TLSKeyPath: "/data/c.key",
		ActiveSets: []domain.InboundSet{sq, ws},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	inbounds, _ := doc["inbounds"].([]any)
	var foundSQ, foundWS bool
	for _, rawIB := range inbounds {
		ib, _ := rawIB.(map[string]any)
		switch ib["type"] {
		case "shadowquic":
			foundSQ = true
			jls, _ := ib["jls_upstream"].(map[string]any)
			if jls["addr"] != "cdn.example:443" || jls["server_name"] != "cdn.example" {
				t.Fatalf("jls override=%v", jls)
			}
		case "vless":
			foundWS = true
			tr, _ := ib["transport"].(map[string]any)
			if tr["path"] != "/vless-ws" {
				t.Fatalf("ws path default from notes=%v", tr["path"])
			}
			headers, _ := tr["headers"].(map[string]any)
			host, _ := headers["Host"].([]any)
			if len(host) != 1 || host[0] != "edge.example" {
				t.Fatalf("ws Host=%v", headers["Host"])
			}
		}
	}
	if !foundSQ || !foundWS {
		t.Fatalf("foundSQ=%v foundWS=%v", foundSQ, foundWS)
	}
}

func TestShadowQUICDemuxSNISyncsJLS(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	set := domain.InboundSet{
		Name: "dg-sq", Listen: "::", ListenPort: 443,
		DemuxTemplate: map[string]any{
			"type": "demux", "tag": "d", "listen": "::", "listen_port": 443,
			"network": []any{"udp"}, "rules": []any{},
		},
		MemberPorts: map[string]uint16{"shadowquic_jls": 41002},
		Bindings: []domain.SetBinding{
			{Preset: "shadowquic_jls", Params: map[string]string{"demux_sni": "www.amazon.com"}},
		},
	}
	user := domain.User{
		Name: "u1", Enabled: true, CreatedAt: now,
		Creds: map[string]map[string]any{
			"shadowquic_jls": {"username": "alice", "password": "secret"},
		},
	}
	raw, err := Build(Input{
		PublicHost: "edge.example", DataDir: "/data",
		TLS:         domain.DefaultSelfSigned("edge.example"),
		TLSCertPath: "/data/c.crt", TLSKeyPath: "/data/c.key",
		ActiveSets: []domain.InboundSet{set},
		Users:      []domain.User{user},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	inbounds, _ := doc["inbounds"].([]any)
	var found bool
	for _, rawIB := range inbounds {
		ib, _ := rawIB.(map[string]any)
		if ib["type"] != "shadowquic" {
			continue
		}
		found = true
		jls, _ := ib["jls_upstream"].(map[string]any)
		if jls["server_name"] != "www.amazon.com" {
			t.Fatalf("expected demux_sni synced to jls, got %v", jls["server_name"])
		}
	}
	if !found {
		t.Fatal("shadowquic inbound missing")
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

// Synthetic smoke: catalogsqlite ready tags materialize with constructor defaults.
func TestCatalogsqliteVlessReadyMatrix(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	tls := domain.DefaultSelfSigned("h.example")
	cases := []struct {
		tag       string
		wantTr    string
		wantTLS   bool
		wantReality bool
	}{
		{"vless_tcp", "", false, false},
		{"vless_tls", "", true, false},
		{"vless_ws_tls", "ws", true, false},
		{"vless_grpc_tls", "grpc", true, false},
		{"vless_quic_tls", "quic", true, false},
		{"vless_custom", "", true, false},
		{"vless_tls_mux", "", true, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.tag, func(t *testing.T) {
			t.Parallel()
			set := domain.InboundSet{
				Name: "m1", Listen: "0.0.0.0", ListenPort: 443,
				Presets: []string{tc.tag},
			}
			user := domain.User{
				Name: "u1", Enabled: true, CreatedAt: now,
				Creds: map[string]map[string]any{
					tc.tag: {
						"uuid":      "11111111-2222-3333-4444-555555555555",
						"uuid_xtls": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
						"uuid_udp":  "99999999-8888-7777-6666-555555555555",
					},
				},
			}
			raw, err := Build(Input{
				PublicHost:  "h.example",
				DataDir:     "/data",
				TLS:         tls,
				TLSCertPath: "/data/controlplane/ssl/default/cert.crt",
				TLSKeyPath:  "/data/controlplane/ssl/default/cert.key",
				ActiveSets:  []domain.InboundSet{set},
				Users:       []domain.User{user},
			})
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			ib := firstInbound(t, doc)
			if ib["type"] != "vless" {
				t.Fatalf("type=%v", ib["type"])
			}
			_, hasTLS := ib["tls"]
			if hasTLS != tc.wantTLS {
				t.Fatalf("tls present=%v want=%v", hasTLS, tc.wantTLS)
			}
			tr, _ := ib["transport"].(map[string]any)
			if tc.wantTr == "" {
				if tr != nil {
					t.Fatalf("expected no transport, got %v", tr)
				}
			} else if tr["type"] != tc.wantTr {
				t.Fatalf("transport=%v want %s", tr, tc.wantTr)
			}
			if tc.tag == "vless_tls_mux" {
				mux, _ := ib["multiplex"].(map[string]any)
				if mux["enabled"] != true {
					t.Fatalf("mux=%v", mux)
				}
			}
			if tc.tag == "vless_ws_tls" {
				if tr["max_early_data"] != 2048 && tr["max_early_data"] != float64(2048) {
					t.Fatalf("ws early data=%v", tr["max_early_data"])
				}
			}
			body, err := RenderSubscription(user, []domain.InboundSet{set}, "h.example", tls, SubscriptionFilters{}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var sub map[string]any
			if err := json.Unmarshal(body, &sub); err != nil {
				t.Fatal(err)
			}
			outs, _ := sub["outbounds"].([]any)
			if len(outs) == 0 {
				t.Fatal("subscription empty")
			}
		})
	}
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

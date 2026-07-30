//go:build with_controlplane

package presets

import "testing"

func TestCatalogLoads(t *testing.T) {
	all := All()
	if len(all) < 90 {
		t.Fatalf("expected >=90 materialize presets, got %d", len(all))
	}
	seen := map[string]struct{}{}
	for _, p := range all {
		if p.Name == "" || p.Protocol == "" {
			t.Fatalf("invalid preset %+v", p)
		}
		if _, ok := seen[p.Name]; ok {
			t.Fatalf("duplicate %s", p.Name)
		}
		seen[p.Name] = struct{}{}
		inboundOnly := false
		endpoint := false
		for _, tr := range p.Traits {
			if tr == "inbound_only" {
				inboundOnly = true
			}
			if tr == "endpoint" {
				endpoint = true
			}
		}
		if endpoint {
			if len(p.EndpointTemplate) == 0 {
				t.Fatalf("%s: missing endpoint template", p.Name)
			}
		} else if len(p.InboundTemplate) == 0 {
			t.Fatalf("%s: missing inbound template", p.Name)
		}
		if !inboundOnly && len(p.OutboundTemplate) == 0 {
			t.Fatalf("%s: missing outbound template", p.Name)
		}
		if !inboundOnly && len(p.CredFields) == 0 {
			t.Fatalf("%s: cred_fields empty", p.Name)
		}
		if p.Description == "" {
			t.Fatalf("%s: empty description (i18n.ru?)", p.Name)
		}
		if p.ShortName == "" {
			t.Fatalf("%s: short_name empty", p.Name)
		}
	}
	// Legacy hyphen aliases must resolve.
	for _, want := range []string{
		"shadowsocks-tcp", "shadowsocks-aes-256-gcm", "shadowsocks-chacha20",
		"trojan-tcp", "vless-tcp", "vless-tls", "vless-reality-tcp", "vmess-tcp", "vmess-tls",
		"hysteria2", "tuic", "anytls", "socks", "http",
	} {
		p, err := Get(want)
		if err != nil {
			t.Fatalf("missing preset alias %s: %v", want, err)
		}
		if p.Name == "" {
			t.Fatalf("empty canonical for %s", want)
		}
	}
	// Canonical snake_case tags.
	for _, want := range []string{
		"ss_aes128", "vless_reality", "vless_ws_tls", "trojan_ws_tls", "trojan_reality",
		"ss_2022_aes128", "hy2_salamander", "tuic_0rtt", "shadowtls_v3", "naive_tls", "vmess_reality",
		"mixed_auth", "mieru_tcp", "hy1", "snell_v5",
		"shadowquic_jls", "sudoku_pad", "trusttunnel_h2", "vmess_ws_reality",
		"derp_tls", "carrier_peer_shared", "shadowtls_v3_wildcard", "sudoku_aes",
		"carrier_jitsi_shared", "carrier_telemost_shared", "carrier_wbstream_shared", "carrier_vk_shared",
		"vless_grpc_reality", "trojan_http_tls", "ssh_pubkey", "cloudflared_token", "ss_2022_aes128_mux",
		"shadowquic_uot", "trusttunnel_auto", "vless_quic_tls", "vless_hysteria_tls", "hy2_masquerade_file", "carrier_jitsi_sei_shared",
		"hy2_gecko", "hy2_gecko_compact", "hy2_gecko_masquerade",
		"vless_httpupgrade_reality", "vmess_httpupgrade_reality", "trojan_httpupgrade_reality",
		"wg", "wg_awg2", "wg_awg3",
	} {
		if _, err := Get(want); err != nil {
			t.Fatalf("missing canonical %s: %v", want, err)
		}
	}
	hyTr, err := Get("vless-hysteria-tls")
	if err != nil {
		t.Fatal(err)
	}
	if hyTr.Name != "vless_hysteria_tls" {
		t.Fatalf("alias canonical=%s", hyTr.Name)
	}
	if hyTr.PeerSecretFields["hy_auth"] != "password" {
		t.Fatalf("hy_auth gen=%v", hyTr.PeerSecretFields)
	}
	if len(hyTr.DefaultUserVariants) != 1 || hyTr.DefaultUserVariants[0] != "flow-none" {
		t.Fatalf("vless_hysteria_tls variants=%v", hyTr.DefaultUserVariants)
	}
	ws, err := Get("vless_ws_tls")
	if err != nil {
		t.Fatal(err)
	}
	if len(ws.DefaultUserVariants) != 1 || ws.DefaultUserVariants[0] != "flow-none" {
		t.Fatalf("vless_ws_tls default_user_variants=%v", ws.DefaultUserVariants)
	}
	ss, err := Get("ss_2022_aes128")
	if err != nil {
		t.Fatal(err)
	}
	if ss.CredGenerators["password"] != "ss2022_16" {
		t.Fatalf("ss2022 generator=%v", ss.CredGenerators)
	}
	if ss.PeerSecretFields["password"] != "ss2022_16" {
		t.Fatalf("ss2022 peer_secret_fields=%v", ss.PeerSecretFields)
	}
	a, _ := Get("vless-reality-tcp")
	b, _ := Get("vless_reality")
	if a.Name != b.Name || a.Name != "vless_reality" {
		t.Fatalf("alias normalize: %+v vs %+v", a.Name, b.Name)
	}
	if a.Scores == nil || a.Scores.DPI == nil || *a.Scores.DPI != 10 {
		t.Fatalf("vless_reality dpi benchmark want 10, got %+v", a.Scores)
	}
}

func TestProtocolsCatalog(t *testing.T) {
	ps := Protocols()
	if len(ps) < 10 {
		t.Fatalf("protocols=%d", len(ps))
	}
	vless, err := GetProtocol("vless")
	if err != nil {
		t.Fatal(err)
	}
	if len(vless.InvariantTags) < 6 {
		t.Fatalf("vless invariants=%v", vless.InvariantTags)
	}
	if _, err := GetProtocol("ssh"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetProtocol("mieru"); err != nil {
		t.Fatal(err)
	}
}

func TestEnrichmentTLSandTransportPresent(t *testing.T) {
	p, err := Get("vless_ws_tls")
	if err != nil {
		t.Fatal(err)
	}
	tls, _ := p.OutboundTemplate["tls"].(map[string]any)
	utls, _ := tls["utls"].(map[string]any)
	if utls["fingerprint"] != "chrome" {
		t.Fatalf("utls=%v", utls)
	}
	tr, _ := p.InboundTemplate["transport"].(map[string]any)
	if tr["max_early_data"] == nil {
		t.Fatalf("ws early data missing: %v", tr)
	}
	hy, err := Get("hy2")
	if err != nil {
		t.Fatal(err)
	}
	htls, _ := hy.InboundTemplate["tls"].(map[string]any)
	alpn, _ := htls["alpn"].([]any)
	if len(alpn) == 0 || alpn[0] != "h3" {
		t.Fatalf("hy2 alpn=%v", alpn)
	}
}

func TestDERPAndCarrierPresets(t *testing.T) {
	d, err := Get("derp_tls")
	if err != nil {
		t.Fatal(err)
	}
	if d.CredGenerators["private_key"] != "curve25519" {
		t.Fatalf("derp gen=%v", d.CredGenerators)
	}
	if d.PeerSecretFields["public_key"] == "" {
		t.Fatal("derp peer public_key missing")
	}
	c, err := Get("carrier_jitsi_shared")
	if err != nil {
		t.Fatal(err)
	}
	if c.Protocol != "carrier" {
		t.Fatalf("carrier proto=%s", c.Protocol)
	}
	if len(c.ParamFields) == 0 || c.ParamFields[0] != "room" {
		t.Fatalf("jitsi param_fields=%v", c.ParamFields)
	}
}

func TestCredsForAlias(t *testing.T) {
	creds := map[string]map[string]any{
		"vless-tcp": {"uuid": "11111111-2222-3333-4444-555555555555"},
	}
	got := CredsFor(creds, "vless_tcp")
	if got == nil || got["uuid"] != creds["vless-tcp"]["uuid"] {
		t.Fatalf("CredsFor canonical miss: %v", got)
	}
	got2 := CredsFor(creds, "vless-tcp")
	if got2 == nil {
		t.Fatal("CredsFor alias miss")
	}
}

func TestLangNormalize(t *testing.T) {
	inv, err := GetInvariant("vless_reality")
	if err != nil {
		t.Fatal(err)
	}
	p := inv.ToProtocolPreset("ru-RU")
	if p.Description == "" {
		t.Fatal("ru-RU fallback empty")
	}
	p2 := inv.ToProtocolPreset("zz")
	if p2.Description == "" {
		t.Fatal("unknown lang should fall back to ru")
	}
}

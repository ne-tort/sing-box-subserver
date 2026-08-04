//go:build with_controlplane

package catalogsqlite

import (
	"strings"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func TestVlessPilotSeed(t *testing.T) {
	if _, err := DB(); err != nil {
		t.Fatal(err)
	}
	if !Owns("vless_custom") || !Owns("vless_ws_tls") || !Owns("vless-ws-tls") {
		t.Fatalf("expected owns base/ready/alias")
	}
	if !OwnsProtocol("vless") {
		t.Fatal("owns protocol")
	}
	base, err := GetInvariant("vless_custom")
	if err != nil {
		t.Fatal(err)
	}
	if !base.CustomPreset || base.ParamMeta["transport"].Type == "" {
		t.Fatalf("base schema incomplete: %+v", base.ParamMeta["transport"])
	}
	ready, err := GetInvariant("vless_ws_tls")
	if err != nil {
		t.Fatal(err)
	}
	if !ready.CustomPreset {
		t.Fatal("ready must expose full custom schema")
	}
	if ready.ParamMeta["transport"].Default != "ws" {
		t.Fatalf("ws ready transport default=%q", ready.ParamMeta["transport"].Default)
	}
	if ready.ParamMeta["tls_mode"].Default != "tls" {
		t.Fatalf("tls_mode=%q", ready.ParamMeta["tls_mode"].Default)
	}
	// Full schema fields present on ready.
	if _, ok := ready.ParamMeta["flow"]; !ok {
		t.Fatal("ready missing flow from base schema")
	}
	p, err := GetProtocol("vless")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.InvariantTags) < 10 {
		t.Fatalf("invariant tags=%v", p.InvariantTags)
	}
	all, err := AllPresets()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 10 {
		t.Fatalf("all=%d", len(all))
	}
	eff, err := EffectiveParams("vless_ws_tls", map[string]string{"ws_path": "/x"})
	if err != nil {
		t.Fatal(err)
	}
	if eff["transport"] != "ws" || eff["transport_path"] != "/x" {
		t.Fatalf("effective=%v", eff)
	}
	_ = domain.ProtocolPreset{}
}

func TestRealityReadyDefaults(t *testing.T) {
	inv, err := GetInvariant("vless_reality")
	if err != nil {
		t.Fatal(err)
	}
	if inv.ParamMeta["tls_mode"].Default != "reality" {
		t.Fatalf("got %q", inv.ParamMeta["tls_mode"].Default)
	}
	if inv.ParamMeta["transport"].Default != "tcp" {
		t.Fatalf("transport %q", inv.ParamMeta["transport"].Default)
	}
}

func TestReadySharesFullBaseSchema(t *testing.T) {
	base, err := GetInvariant("vless_custom")
	if err != nil {
		t.Fatal(err)
	}
	readyTags := []string{
		"vless_tcp", "vless_tls", "vless_reality", "vless_ws_tls", "vless_ws_reality",
		"vless_grpc_tls", "vless_grpc_reality", "vless_http_tls", "vless_http_reality",
		"vless_httpupgrade_tls", "vless_httpupgrade_reality",
		"vless_quic_tls", "vless_hysteria_tls", "vless_tls_mux",
	}
	baseKeys := map[string]struct{}{}
	for k := range base.ParamMeta {
		baseKeys[k] = struct{}{}
	}
	for _, tag := range readyTags {
		inv, err := GetInvariant(tag)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if !inv.CustomPreset {
			t.Fatalf("%s: custom_preset=false", tag)
		}
		if len(inv.ParamFields) != len(base.ParamFields) {
			t.Fatalf("%s: param_fields=%v want %v", tag, inv.ParamFields, base.ParamFields)
		}
		for k := range baseKeys {
			if _, ok := inv.ParamMeta[k]; !ok {
				t.Fatalf("%s: missing ParamMeta %q", tag, k)
			}
		}
		// Full constructor creds (not stock-shrunk).
		if len(inv.CredFields) < 3 {
			t.Fatalf("%s: cred_fields=%v (want base uuid/uuid_xtls/uuid_udp)", tag, inv.CredFields)
		}
		if inv.ParamMeta["flow"].VisibleWhen == nil || len(inv.ParamMeta["flow"].VisibleWhen) == 0 {
			t.Fatalf("%s: flow.visible_when missing", tag)
		}
	}
}

func TestMuxOwnTemplateDoesNotLeakBaseTransport(t *testing.T) {
	inv, err := GetInvariant("vless_tls_mux")
	if err != nil {
		t.Fatal(err)
	}
	// Mux is expressed via constructor params (no own templates).
	if inv.ParamMeta["multiplex"].Default != "smux" {
		t.Fatalf("multiplex default=%q", inv.ParamMeta["multiplex"].Default)
	}
	tr, _ := inv.InboundTemplate["transport"].(map[string]any)
	if tr["type"] != "{{param.transport}}" {
		t.Fatalf("expected base constructor transport, got %#v", tr)
	}
}

func TestHysteriaReadyALPN(t *testing.T) {
	eff, err := EffectiveParams("vless_hysteria_tls", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eff["alpn"] != "h3" {
		t.Fatalf("alpn=%q", eff["alpn"])
	}
	if eff["transport"] != "hysteria" {
		t.Fatalf("transport=%q", eff["transport"])
	}
	inv, err := GetInvariant("vless_custom")
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := inv.InboundTemplate["transport"].(map[string]any)
	if pw, _ := tr["password"].(string); pw != "{{peer.hy_auth}}" {
		t.Fatalf("base must keep hy_auth placeholder, got %q", pw)
	}
}

func TestBindingTLSModeUsesReadyDefaults(t *testing.T) {
	cases := map[string]string{
		"vless_tcp":         "none",
		"vless_tls":         "tls",
		"vless_reality":     "reality",
		"vless_ws_reality":  "reality",
		"vless_grpc_tls":    "tls",
	}
	for tag, want := range cases {
		inv, err := GetInvariant(tag)
		if err != nil {
			t.Fatal(err)
		}
		pp := inv.ToProtocolPreset("en")
		mode, ok := domain.BindingTLSMode(pp, nil)
		if !ok {
			t.Fatalf("%s: BindingTLSMode uncontrolled", tag)
		}
		if mode != want {
			t.Fatalf("%s: mode=%q want %q", tag, mode, want)
		}
		if want == "reality" && !domain.BindingUsesReality(pp, nil) {
			t.Fatalf("%s: BindingUsesReality=false", tag)
		}
	}
}

func TestVlessVariantsAndProfilesFromSQLite(t *testing.T) {
	if _, err := DB(); err != nil {
		t.Fatal(err)
	}
	variants := domain.UserVariantCatalog("vless")
	if len(variants) != 3 {
		t.Fatalf("user variants=%d want 3", len(variants))
	}
	var sawVision bool
	for _, vv := range variants {
		if vv.Name == "flow-xtls-rprx-vision" {
			sawVision = true
			if !vv.SubscriptionDefault || vv.CredentialField != "uuid_xtls" || vv.FlowValue != "xtls-rprx-vision" {
				t.Fatalf("vision spec %#v", vv)
			}
		}
	}
	if !sawVision {
		t.Fatal("missing flow-xtls-rprx-vision")
	}
	profiles := domain.ClientProfileCatalog("vless")
	if len(profiles) != 3 {
		t.Fatalf("client profiles=%d want 3", len(profiles))
	}
	var sawXUDP bool
	for _, cp := range profiles {
		if cp.Name == "pkt-xudp" {
			sawXUDP = true
			if cp.OutboundOverrides["packet_encoding"] != "xudp" {
				t.Fatalf("pkt-xudp overrides %#v", cp.OutboundOverrides)
			}
		}
	}
	if !sawXUDP {
		t.Fatal("missing pkt-xudp")
	}
	var sawNone bool
	for _, cp := range profiles {
		if cp.Name == "pkt-none" {
			sawNone = true
			if v, ok := cp.OutboundOverrides["packet_encoding"]; !ok || v != nil {
				t.Fatalf("pkt-none must explicitly clear packet_encoding via null override, got %#v", cp.OutboundOverrides)
			}
		}
	}
	if !sawNone {
		t.Fatal("missing pkt-none")
	}
	profilesVMess := domain.ClientProfileCatalog("vmess")
	if len(profilesVMess) != 4 {
		t.Fatalf("vmess client profiles=%d want 4", len(profilesVMess))
	}
	if len(domain.UserVariantCatalog("vmess")) != 0 {
		t.Fatal("vmess must have empty user_variants")
	}
}

func TestReadyHasNoOwnTemplates(t *testing.T) {
	conn, err := DB()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	err = conn.QueryRow(`
SELECT COUNT(1) FROM ready_presets
WHERE inbound_template_json IS NOT NULL
   OR outbound_template_json IS NOT NULL
   OR endpoint_template_json IS NOT NULL`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("ready presets with own templates: %d (want 0)", n)
	}
	err = conn.QueryRow(`SELECT COUNT(1) FROM ready_param_values`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n < 50 {
		t.Fatalf("ready_param_values=%d too small", n)
	}
}

func TestReadyParamPathsAlignWithNotes(t *testing.T) {
	cases := map[string]string{
		"vless_ws_tls":              "/vless-ws",
		"vless_ws_reality":          "/vless-ws",
		"vless_http_tls":            "/vless-h2",
		"vless_http_reality":        "/vless-h2",
		"vless_httpupgrade_tls":     "/vless-hu",
		"vless_httpupgrade_reality": "/vless-hu",
	}
	for tag, wantPath := range cases {
		eff, err := EffectiveParams(tag, nil)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if eff["transport_path"] != wantPath {
			t.Fatalf("%s transport_path=%q want %q", tag, eff["transport_path"], wantPath)
		}
	}
	eff, err := EffectiveParams("vless_ws_reality", nil)
	if err != nil {
		t.Fatal(err)
	}
	if eff["ws_max_early_data"] != "0" {
		t.Fatalf("ws_reality early data=%q want 0", eff["ws_max_early_data"])
	}
}

func TestReadyDefaultsAvoidCatalogFallbacks(t *testing.T) {
	// Empty default_user_variants falls back to catalog subscription_default=Vision.
	// Empty default_client_profiles falls back to pkt-none. Ready presets must set both.
	cases := map[string]struct {
		variants []string
		profiles []string
	}{
		"vless_tcp":     {[]string{"flow-none"}, []string{"pkt-xudp"}},
		"vless_tls":     {[]string{"flow-xtls-rprx-vision"}, []string{"pkt-xudp"}},
		"vless_tls_mux": {[]string{"flow-none"}, []string{"pkt-xudp"}},
		"vless_ws_tls":  {[]string{"flow-none"}, []string{"pkt-xudp"}},
	}
	for tag, want := range cases {
		inv, err := GetInvariant(tag)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if len(inv.DefaultUserVariants) == 0 {
			t.Fatalf("%s: empty default_user_variants (would fall back to Vision)", tag)
		}
		if inv.DefaultUserVariants[0] != want.variants[0] {
			t.Fatalf("%s: first variant=%v want %v", tag, inv.DefaultUserVariants, want.variants)
		}
		if len(inv.DefaultClientProfiles) == 0 || inv.DefaultClientProfiles[0] != want.profiles[0] {
			t.Fatalf("%s: profiles=%v want %v", tag, inv.DefaultClientProfiles, want.profiles)
		}
	}
}

func TestAllReadyPresetsListedOnProtocol(t *testing.T) {
	p, err := GetProtocol("vless")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"vless_custom",
		"vless_tcp", "vless_tls", "vless_reality", "vless_tls_mux",
		"vless_ws_tls", "vless_ws_reality",
		"vless_grpc_tls", "vless_grpc_reality",
		"vless_http_tls", "vless_http_reality",
		"vless_httpupgrade_tls", "vless_httpupgrade_reality",
		"vless_quic_tls", "vless_hysteria_tls",
	}
	have := map[string]bool{}
	for _, t := range p.InvariantTags {
		have[t] = true
	}
	for _, tag := range want {
		if !have[tag] {
			t.Fatalf("protocol invariants missing %s: %v", tag, p.InvariantTags)
		}
	}
}

func TestVmessCutoverSeed(t *testing.T) {
	if !OwnsProtocol("vmess") || !Owns("vmess_custom") || !Owns("vmess_ws_tls") || !Owns("vmess-ws-tls") {
		t.Fatal("expected owns vmess base/ready/alias")
	}
	base, err := GetInvariant("vmess_custom")
	if err != nil {
		t.Fatal(err)
	}
	if !base.CustomPreset || base.ParamMeta["multiplex"].Type == "" || base.ParamMeta["ws_max_early_data"].Type == "" {
		t.Fatalf("vmess base schema incomplete: multiplex/ws_max_early_data")
	}
	ready, err := GetInvariant("vmess_tls_mux")
	if err != nil {
		t.Fatal(err)
	}
	if ready.ParamMeta["multiplex"].Default != "smux" {
		t.Fatalf("mux default=%q", ready.ParamMeta["multiplex"].Default)
	}
	if len(ready.DefaultClientProfiles) == 0 || ready.DefaultClientProfiles[0] != "sec-auto" {
		t.Fatalf("profiles=%v", ready.DefaultClientProfiles)
	}
	eff, err := EffectiveParams("vmess_ws_tls", map[string]string{"ws_path": "/x"})
	if err != nil {
		t.Fatal(err)
	}
	if eff["transport"] != "ws" || eff["transport_path"] != "/x" || eff["ws_max_early_data"] != "2048" {
		t.Fatalf("effective=%v", eff)
	}
	effR, err := EffectiveParams("vmess_ws_reality", nil)
	if err != nil {
		t.Fatal(err)
	}
	if effR["ws_max_early_data"] != "0" || effR["transport_path"] != "/vmess" {
		t.Fatalf("ws_reality=%v", effR)
	}
	p, err := GetProtocol("vmess")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"vmess_custom",
		"vmess_tcp", "vmess_tls", "vmess_reality", "vmess_tls_mux",
		"vmess_ws_tls", "vmess_ws_reality",
		"vmess_grpc_tls", "vmess_grpc_reality",
		"vmess_http_tls", "vmess_http_reality",
		"vmess_httpupgrade_tls", "vmess_httpupgrade_reality",
		"vmess_quic_tls",
	}
	have := map[string]bool{}
	for _, tag := range p.InvariantTags {
		have[tag] = true
	}
	for _, tag := range want {
		if !have[tag] {
			t.Fatalf("vmess invariants missing %s: %v", tag, p.InvariantTags)
		}
	}
	readyTags := []string{
		"vmess_tcp", "vmess_tls", "vmess_reality", "vmess_tls_mux",
		"vmess_ws_tls", "vmess_ws_reality",
		"vmess_grpc_tls", "vmess_grpc_reality",
		"vmess_http_tls", "vmess_http_reality",
		"vmess_httpupgrade_tls", "vmess_httpupgrade_reality",
		"vmess_quic_tls",
	}
	baseKeys := map[string]struct{}{}
	for k := range base.ParamMeta {
		baseKeys[k] = struct{}{}
	}
	for _, tag := range readyTags {
		inv, err := GetInvariant(tag)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if !inv.CustomPreset {
			t.Fatalf("%s: ready must expose custom schema", tag)
		}
		for k := range baseKeys {
			if _, ok := inv.ParamMeta[k]; !ok {
				t.Fatalf("%s missing base param %s", tag, k)
			}
		}
		if len(inv.DefaultClientProfiles) == 0 || inv.DefaultClientProfiles[0] != "sec-auto" {
			t.Fatalf("%s profiles=%v want [sec-auto]", tag, inv.DefaultClientProfiles)
		}
	}
}

func TestTrojanCutoverSeed(t *testing.T) {
	if !OwnsProtocol("trojan") || !Owns("trojan_custom") || !Owns("trojan_tls") || !Owns("trojan-tcp") {
		t.Fatal("expected owns trojan base/ready/alias trojan-tcp")
	}
	base, err := GetInvariant("trojan_custom")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"multiplex", "ws_max_early_data", "fallback"} {
		if base.ParamMeta[key].Type == "" {
			t.Fatalf("trojan base missing %s", key)
		}
	}
	mux, err := GetInvariant("trojan_tls_mux")
	if err != nil {
		t.Fatal(err)
	}
	if mux.ParamMeta["multiplex"].Default != "smux" {
		t.Fatalf("mux=%q", mux.ParamMeta["multiplex"].Default)
	}
	fb, err := GetInvariant("trojan_tls_fallback")
	if err != nil {
		t.Fatal(err)
	}
	if fb.ParamMeta["fallback"].Default != "local" {
		t.Fatalf("fallback=%q", fb.ParamMeta["fallback"].Default)
	}
	eff, err := EffectiveParams("trojan_ws_tls", map[string]string{"ws_path": "/x"})
	if err != nil {
		t.Fatal(err)
	}
	if eff["transport"] != "ws" || eff["transport_path"] != "/x" || eff["ws_max_early_data"] != "2048" {
		t.Fatalf("effective=%v", eff)
	}
	effR, err := EffectiveParams("trojan_ws_reality", nil)
	if err != nil {
		t.Fatal(err)
	}
	if effR["transport_path"] != "/trojan" || effR["ws_max_early_data"] != "0" {
		t.Fatalf("ws_reality=%v", effR)
	}
	if len(domain.UserVariantCatalog("trojan")) != 0 || len(domain.ClientProfileCatalog("trojan")) != 0 {
		t.Fatal("trojan must have empty variants/profiles")
	}
	p, err := GetProtocol("trojan")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"trojan_custom",
		"trojan_tls", "trojan_reality", "trojan_tls_mux", "trojan_tls_fallback",
		"trojan_ws_tls", "trojan_ws_reality",
		"trojan_grpc_tls", "trojan_grpc_reality",
		"trojan_http_tls", "trojan_http_reality",
		"trojan_httpupgrade_tls", "trojan_httpupgrade_reality",
		"trojan_quic_tls",
	}
	have := map[string]bool{}
	for _, tag := range p.InvariantTags {
		have[tag] = true
	}
	for _, tag := range want {
		if !have[tag] {
			t.Fatalf("trojan invariants missing %s: %v", tag, p.InvariantTags)
		}
	}
	for _, tag := range want {
		if tag == "trojan_custom" {
			continue
		}
		inv, err := GetInvariant(tag)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if !inv.CustomPreset {
			t.Fatalf("%s: ready must expose custom schema", tag)
		}
		if _, ok := inv.ParamMeta["fallback"]; !ok {
			t.Fatalf("%s missing fallback from base schema", tag)
		}
	}
}

func TestTuicCutoverSeed(t *testing.T) {
	if !OwnsProtocol("tuic") || !Owns("tuic_custom") || !Owns("tuic") || !Owns("tuic_0rtt") || !Owns("tuic-0rtt") {
		t.Fatal("expected owns tuic base/ready/alias")
	}
	base, err := GetInvariant("tuic_custom")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"congestion_control", "udp_relay_mode", "zero_rtt"} {
		if base.ParamMeta[key].Type == "" {
			t.Fatalf("tuic base missing %s", key)
		}
	}
	ready, err := GetInvariant("tuic")
	if err != nil {
		t.Fatal(err)
	}
	if ready.ParamMeta["congestion_control"].Default != "bbr" || ready.ParamMeta["zero_rtt"].Default != "false" {
		t.Fatalf("tuic defaults congestion=%q zero_rtt=%q", ready.ParamMeta["congestion_control"].Default, ready.ParamMeta["zero_rtt"].Default)
	}
	if len(ready.DefaultClientProfiles) == 0 || ready.DefaultClientProfiles[0] != "udp-native" {
		t.Fatalf("profiles=%v", ready.DefaultClientProfiles)
	}
	z, err := GetInvariant("tuic_0rtt")
	if err != nil {
		t.Fatal(err)
	}
	if z.ParamMeta["zero_rtt"].Default != "true" {
		t.Fatalf("0rtt=%q", z.ParamMeta["zero_rtt"].Default)
	}
	profiles := domain.ClientProfileCatalog("tuic")
	if len(profiles) != 2 {
		t.Fatalf("tuic profiles=%d want 2", len(profiles))
	}
	if len(domain.UserVariantCatalog("tuic")) != 0 {
		t.Fatal("tuic must have empty user_variants")
	}
	p, err := GetProtocol("tuic")
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"tuic_custom", "tuic", "tuic_0rtt"} {
		found := false
		for _, t := range p.InvariantTags {
			if t == tag {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %s in %v", tag, p.InvariantTags)
		}
	}
}

func TestHy2CutoverSeed(t *testing.T) {
	if !OwnsProtocol("hysteria2") || !Owns("hy2_custom") || !Owns("hy2") || !Owns("hysteria2") || !Owns("hy2_salamander") {
		t.Fatal("expected owns hy2 base/ready/alias")
	}
	base, err := GetInvariant("hy2_custom")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"obfs_type", "masquerade_mode", "realm_mode", "up_mbps"} {
		if base.ParamMeta[key].Type == "" && key != "realm_server_url" {
			if _, ok := base.ParamMeta[key]; !ok {
				t.Fatalf("hy2 base missing %s", key)
			}
		}
	}
	if base.ParamMeta["obfs_type"].Default != "none" {
		t.Fatalf("obfs default=%q", base.ParamMeta["obfs_type"].Default)
	}
	sal, err := GetInvariant("hy2_salamander")
	if err != nil {
		t.Fatal(err)
	}
	if sal.ParamMeta["obfs_type"].Default != "salamander" {
		t.Fatalf("salamander=%q", sal.ParamMeta["obfs_type"].Default)
	}
	g, err := GetInvariant("hy2_gecko_compact")
	if err != nil {
		t.Fatal(err)
	}
	if g.ParamMeta["obfs_type"].Default != "gecko_compact" {
		t.Fatalf("gecko_compact=%q", g.ParamMeta["obfs_type"].Default)
	}
	r, err := GetInvariant("hy2_realm")
	if err != nil {
		t.Fatal(err)
	}
	if r.ParamMeta["realm_mode"].Default != "on" {
		t.Fatalf("realm_mode=%q", r.ParamMeta["realm_mode"].Default)
	}
	if len(domain.UserVariantCatalog("hysteria2")) != 0 || len(domain.ClientProfileCatalog("hysteria2")) != 0 {
		t.Fatal("hy2 must have empty variants/profiles")
	}
	p, err := GetProtocol("hysteria2")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"hy2_custom", "hy2", "hy2_salamander", "hy2_gecko", "hy2_gecko_compact",
		"hy2_masquerade", "hy2_gecko_masquerade", "hy2_masquerade_file", "hy2_masquerade_proxy", "hy2_realm",
	}
	have := map[string]bool{}
	for _, tag := range p.InvariantTags {
		have[tag] = true
	}
	for _, tag := range want {
		if !have[tag] {
			t.Fatalf("missing %s in %v", tag, p.InvariantTags)
		}
	}
}

func TestShadowsocksCutoverSeed(t *testing.T) {
	if !OwnsProtocol("shadowsocks") || !Owns("shadowsocks_custom") || !Owns("ss_aes128") || !Owns("shadowsocks-tcp") {
		t.Fatal("expected owns shadowsocks base/ready/alias")
	}
	base, err := GetInvariant("shadowsocks_custom")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"method", "network", "udp_over_tcp", "multiplex"} {
		if _, ok := base.ParamMeta[key]; !ok {
			t.Fatalf("ss base missing %s", key)
		}
	}
	if base.PeerSecretFields["password"] != "ss2022_16" {
		t.Fatalf("base peer gen=%v", base.PeerSecretFields)
	}
	aes, err := GetInvariant("ss_aes128")
	if err != nil {
		t.Fatal(err)
	}
	if aes.ParamMeta["method"].Default != "aes-128-gcm" {
		t.Fatalf("ss_aes128 method=%q", aes.ParamMeta["method"].Default)
	}
	dual, err := GetInvariant("ss_aes128_dual")
	if err != nil {
		t.Fatal(err)
	}
	if dual.ParamMeta["network"].Default != "tcp,udp" {
		t.Fatalf("ss_aes128_dual network=%q", dual.ParamMeta["network"].Default)
	}
	mux, err := GetInvariant("ss_aes128_mux")
	if err != nil {
		t.Fatal(err)
	}
	if mux.ParamMeta["multiplex"].Default != "smux" {
		t.Fatalf("mux=%q", mux.ParamMeta["multiplex"].Default)
	}
	ss2022, err := GetInvariant("ss_2022_aes128")
	if err != nil {
		t.Fatal(err)
	}
	if ss2022.CredGenerators["password"] != "ss2022_16" || ss2022.PeerSecretFields["password"] != "ss2022_16" {
		t.Fatalf("ss2022 gens=%v peer=%v", ss2022.CredGenerators, ss2022.PeerSecretFields)
	}
	aes256, err := GetInvariant("ss_2022_aes256")
	if err != nil {
		t.Fatal(err)
	}
	if aes256.CredGenerators["password"] != "ss2022_32" || aes256.PeerSecretFields["password"] != "ss2022_32" {
		t.Fatalf("ss2022-256 gens=%v peer=%v", aes256.CredGenerators, aes256.PeerSecretFields)
	}
	chacha, err := GetInvariant("ss_2022_chacha")
	if err != nil {
		t.Fatal(err)
	}
	hasShared, hasNoUsers := false, false
	for _, tr := range chacha.Traits {
		if tr == "shared_key" {
			hasShared = true
		}
		if tr == "no_users" {
			hasNoUsers = true
		}
	}
	if !hasShared || !hasNoUsers {
		t.Fatalf("chacha traits=%v", chacha.Traits)
	}
	if len(domain.UserVariantCatalog("shadowsocks")) != 0 || len(domain.ClientProfileCatalog("shadowsocks")) != 0 {
		t.Fatal("shadowsocks must have empty variants/profiles")
	}
	p, err := GetProtocol("shadowsocks")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"shadowsocks_custom", "ss_aes128", "ss_aes128_mux", "ss_aes128_uot",
		"ss_aes256", "ss_chacha20", "ss_2022_aes128", "ss_2022_aes128_mux",
		"ss_2022_aes256", "ss_2022_chacha",
	}
	have := map[string]bool{}
	for _, tag := range p.InvariantTags {
		have[tag] = true
	}
	for _, tag := range want {
		if !have[tag] {
			t.Fatalf("missing %s in %v", tag, p.InvariantTags)
		}
	}
}


func TestSocksHTTPMixedCutoverSeed(t *testing.T) {
	for _, proto := range []string{"socks", "http", "mixed"} {
		if !OwnsProtocol(proto) {
			t.Fatalf("expected owns %s", proto)
		}
	}
	if !Owns("socks") || !Owns("socks_custom") || !Owns("socks_uot") || !Owns("socks-uot") {
		t.Fatal("socks owns")
	}
	if !Owns("http") || !Owns("http_tls") || !Owns("http-tls") || !Owns("http_custom") {
		t.Fatal("http owns")
	}
	if !Owns("mixed_auth") || !Owns("mixed") || !Owns("mixed_tls") || !Owns("mixed_custom") {
		t.Fatal("mixed owns")
	}
	socks, err := GetInvariant("socks")
	if err != nil {
		t.Fatal(err)
	}
	if socks.ParamMeta["udp_over_tcp"].Default != "false" {
		t.Fatalf("socks uot=%q", socks.ParamMeta["udp_over_tcp"].Default)
	}
	uot, err := GetInvariant("socks_uot")
	if err != nil {
		t.Fatal(err)
	}
	if uot.ParamMeta["udp_over_tcp"].Default != "true" {
		t.Fatalf("socks_uot=%q", uot.ParamMeta["udp_over_tcp"].Default)
	}
	plain, err := GetInvariant("http")
	if err != nil {
		t.Fatal(err)
	}
	if plain.ParamMeta["tls_mode"].Default != "none" {
		t.Fatalf("http tls_mode=%q", plain.ParamMeta["tls_mode"].Default)
	}
	if plain.Requirements != nil && plain.Requirements.TLSProfile {
		t.Fatal("plain http must not require tls_profile")
	}
	ht, err := GetInvariant("http_tls")
	if err != nil {
		t.Fatal(err)
	}
	if ht.ParamMeta["tls_mode"].Default != "tls" {
		t.Fatalf("http_tls=%q", ht.ParamMeta["tls_mode"].Default)
	}
	ma, err := GetInvariant("mixed_auth")
	if err != nil {
		t.Fatal(err)
	}
	if ma.ParamMeta["outbound_type"].Default != "socks" {
		t.Fatalf("mixed_auth outbound=%q", ma.ParamMeta["outbound_type"].Default)
	}
	mt, err := GetInvariant("mixed_tls")
	if err != nil {
		t.Fatal(err)
	}
	if mt.ParamMeta["outbound_type"].Default != "http" || mt.ParamMeta["tls_mode"].Default != "tls" {
		t.Fatalf("mixed_tls params tls=%q out=%q", mt.ParamMeta["tls_mode"].Default, mt.ParamMeta["outbound_type"].Default)
	}
	for _, proto := range []string{"socks", "http", "mixed"} {
		if len(domain.UserVariantCatalog(proto)) != 0 || len(domain.ClientProfileCatalog(proto)) != 0 {
			t.Fatalf("%s must have empty variants/profiles", proto)
		}
	}
}

func TestLXMieruDerpSSHAnyTLSCutoverSeed(t *testing.T) {
	for _, proto := range []string{"mieru", "derp", "ssh", "anytls"} {
		if !OwnsProtocol(proto) {
			t.Fatalf("expected owns %s", proto)
		}
	}
	if !Owns("mieru_tcp") || !Owns("mieru") || !Owns("mieru_udp") {
		t.Fatal("mieru owns")
	}
	mt, err := GetInvariant("mieru_tcp")
	if err != nil {
		t.Fatal(err)
	}
	if mt.ParamMeta["transport"].Default != "TCP" {
		t.Fatalf("mieru transport=%q", mt.ParamMeta["transport"].Default)
	}
	if mt.Requirements == nil || mt.Requirements.BuildTag != "with_mieru" {
		t.Fatalf("mieru build_tag=%v", mt.Requirements)
	}
	if !Owns("derp_tls") || !Owns("derp") || !Owns("derp_uot") || !Owns("derp_ws") {
		t.Fatal("derp owns")
	}
	du, err := GetInvariant("derp_uot")
	if err != nil {
		t.Fatal(err)
	}
	if du.ParamMeta["udp"].Default != "uot" || du.ParamMeta["websocket"].Default != "true" {
		t.Fatalf("derp_uot udp=%q ws=%q", du.ParamMeta["udp"].Default, du.ParamMeta["websocket"].Default)
	}
	if !Owns("ssh_password") || !Owns("ssh_pubkey") || !Owns("ssh-key") || !Owns("ssh_uot") {
		t.Fatal("ssh owns")
	}
	pk, err := GetInvariant("ssh_pubkey")
	if err != nil {
		t.Fatal(err)
	}
	if pk.ParamMeta["auth_mode"].Default != "pubkey" {
		t.Fatalf("auth_mode=%q", pk.ParamMeta["auth_mode"].Default)
	}
	wantCred := map[string]bool{"username": true, "private_key": true, "public_key": true}
	if len(pk.CredFields) != 3 {
		t.Fatalf("ssh_pubkey creds=%v", pk.CredFields)
	}
	for _, f := range pk.CredFields {
		if !wantCred[f] {
			t.Fatalf("unexpected cred %q in %v", f, pk.CredFields)
		}
	}
	if pk.CredGenerators["private_key"] != "ssh_ed25519" {
		t.Fatalf("ssh gen=%v", pk.CredGenerators)
	}
	if !Owns("anytls") || !Owns("anytls_idle") || !Owns("anytls_custom") {
		t.Fatal("anytls owns")
	}
	idle, err := GetInvariant("anytls_idle")
	if err != nil {
		t.Fatal(err)
	}
	if idle.ParamMeta["idle_session"].Default != "true" {
		t.Fatalf("idle_session=%q", idle.ParamMeta["idle_session"].Default)
	}
	for _, proto := range []string{"mieru", "derp", "ssh", "anytls"} {
		if len(domain.UserVariantCatalog(proto)) != 0 || len(domain.ClientProfileCatalog(proto)) != 0 {
			t.Fatalf("%s variants/profiles must be empty", proto)
		}
	}
}

func TestLXCarrierShadowQUICSudokuTrustTunnelCutoverSeed(t *testing.T) {
	for _, proto := range []string{"carrier", "shadowquic", "sudoku", "trusttunnel"} {
		if !OwnsProtocol(proto) {
			t.Fatalf("expected owns %s", proto)
		}
	}
	if !Owns("carrier_peer_shared") || !Owns("carrier") || !Owns("carrier_jitsi_shared") || !Owns("carrier-jitsi") {
		t.Fatal("carrier owns")
	}
	peer, err := GetInvariant("carrier_peer_shared")
	if err != nil {
		t.Fatal(err)
	}
	if peer.ParamMeta["provider"].Default != "peer" {
		t.Fatalf("carrier peer provider=%q", peer.ParamMeta["provider"].Default)
	}
	if peer.Requirements == nil || peer.Requirements.BuildTag != "with_carrier" {
		t.Fatalf("carrier build_tag=%v", peer.Requirements)
	}
	jitsi, err := GetInvariant("carrier_jitsi_shared")
	if err != nil {
		t.Fatal(err)
	}
	if len(jitsi.ParamFields) != 1 || jitsi.ParamFields[0] != "room" {
		t.Fatalf("jitsi param_fields=%v", jitsi.ParamFields)
	}
	if len(peer.ParamFields) != 0 {
		t.Fatalf("peer param_fields must be empty, got %v", peer.ParamFields)
	}
	if !Owns("shadowquic_jls") || !Owns("shadowquic") || !Owns("shadowquic_uot") || !Owns("shadowquic_0rtt") || !Owns("shadowquic_cubic") {
		t.Fatal("shadowquic owns")
	}
	sq, err := GetInvariant("shadowquic_uot")
	if err != nil {
		t.Fatal(err)
	}
	if sq.ParamMeta["udp_over_stream"].Default != "true" || sq.ParamMeta["zero_rtt"].Default != "false" {
		t.Fatalf("shadowquic_uot uot=%q zrtt=%q", sq.ParamMeta["udp_over_stream"].Default, sq.ParamMeta["zero_rtt"].Default)
	}
	if sq.Requirements == nil || sq.Requirements.BuildTag != "with_shadowquic" {
		t.Fatalf("shadowquic build_tag=%v", sq.Requirements)
	}
	sqCubic, err := GetInvariant("shadowquic_cubic")
	if err != nil {
		t.Fatal(err)
	}
	if sqCubic.ParamMeta["congestion_control"].Default != "cubic" {
		t.Fatalf("shadowquic_cubic cc=%q", sqCubic.ParamMeta["congestion_control"].Default)
	}
	if !Owns("sudoku_pad") || !Owns("sudoku") || !Owns("sudoku_aes") || !Owns("sudoku_aes256") || !Owns("sudoku_httpmask") || !Owns("sudoku_mux") {
		t.Fatal("sudoku owns")
	}
	pad, err := GetInvariant("sudoku_pad")
	if err != nil {
		t.Fatal(err)
	}
	if pad.ParamMeta["multiplex"].Default != "auto" || pad.ParamMeta["httpmask_mode"].Default != "off" {
		t.Fatalf("sudoku_pad mux=%q httpmask=%q", pad.ParamMeta["multiplex"].Default, pad.ParamMeta["httpmask_mode"].Default)
	}
	muxOn, err := GetInvariant("sudoku_mux")
	if err != nil {
		t.Fatal(err)
	}
	if muxOn.ParamMeta["multiplex"].Default != "on" {
		t.Fatalf("sudoku_mux multiplex=%q", muxOn.ParamMeta["multiplex"].Default)
	}
	if !Owns("trusttunnel_h2") || !Owns("trusttunnel") || !Owns("trusttunnel_h3") || !Owns("trusttunnel_auto") {
		t.Fatal("trusttunnel owns")
	}
	h3, err := GetInvariant("trusttunnel_h3")
	if err != nil {
		t.Fatal(err)
	}
	if h3.ParamMeta["mode"].Default != "h3" {
		t.Fatalf("trusttunnel_h3 mode=%q", h3.ParamMeta["mode"].Default)
	}
	for _, proto := range []string{"carrier", "shadowquic", "sudoku", "trusttunnel"} {
		if len(domain.UserVariantCatalog(proto)) != 0 || len(domain.ClientProfileCatalog(proto)) != 0 {
			t.Fatalf("%s variants/profiles must be empty", proto)
		}
	}
}

func TestRemainingProtocolsCutoverSeed(t *testing.T) {
	for _, proto := range []string{"wireguard", "cloudflared", "shadowtls", "snell", "naive", "hysteria"} {
		if !OwnsProtocol(proto) {
			t.Fatalf("expected owns %s", proto)
		}
	}
	if !Owns("wg") || !Owns("wireguard") || !Owns("wg_awg2") || !Owns("wg_awg3") || !Owns("wg_custom") {
		t.Fatal("wireguard owns")
	}
	awg2, err := GetInvariant("wg_awg2")
	if err != nil {
		t.Fatal(err)
	}
	if awg2.ParamMeta["mtu"].Default != "1280" {
		t.Fatalf("awg2 mtu=%q", awg2.ParamMeta["mtu"].Default)
	}
	if len(awg2.EndpointTemplate) == 0 {
		t.Fatal("wg_awg2 missing endpoint template from constructor")
	}
	if !Owns("cloudflared_token") || !Owns("cloudflared") || !Owns("cloudflared_custom") {
		t.Fatal("cloudflared owns")
	}
	if !Owns("shadowtls_v3") || !Owns("shadowtls") || !Owns("shadowtls_v3_wildcard") {
		t.Fatal("shadowtls owns")
	}
	st, err := GetInvariant("shadowtls_v3_wildcard")
	if err != nil {
		t.Fatal(err)
	}
	if st.ParamMeta["wildcard_sni"].Default != "authed" {
		t.Fatalf("wildcard=%q", st.ParamMeta["wildcard_sni"].Default)
	}
	if !Owns("snell_v5") || !Owns("snell") || !Owns("snell_v6") || !Owns("snell_v5_tls") {
		t.Fatal("snell owns")
	}
	v6, err := GetInvariant("snell_v6")
	if err != nil {
		t.Fatal(err)
	}
	if v6.ParamMeta["version"].Default != "6" || v6.ParamMeta["obfs_mode"].Default != "off" {
		t.Fatalf("snell_v6 version=%q obfs=%q", v6.ParamMeta["version"].Default, v6.ParamMeta["obfs_mode"].Default)
	}
	snellTLS, err := GetInvariant("snell_v5_tls")
	if err != nil {
		t.Fatal(err)
	}
	if snellTLS.ParamMeta["obfs_mode"].Default != "tls" {
		t.Fatalf("snell_v5_tls obfs=%q", snellTLS.ParamMeta["obfs_mode"].Default)
	}
	if !Owns("naive_tls") || !Owns("naive") || !Owns("naive_quic") {
		t.Fatal("naive owns")
	}
	nq, err := GetInvariant("naive_quic")
	if err != nil {
		t.Fatal(err)
	}
	if nq.ParamMeta["network"].Default != "tcp,udp" {
		t.Fatalf("naive_quic network=%q", nq.ParamMeta["network"].Default)
	}
	if !Owns("hy1") || !Owns("hysteria") || !Owns("hy1_obfs") || !Owns("hysteria_custom") {
		t.Fatal("hysteria owns")
	}
	obfs, err := GetInvariant("hy1_obfs")
	if err != nil {
		t.Fatal(err)
	}
	if obfs.ParamMeta["obfs"].Default != "peer" {
		t.Fatalf("hy1_obfs=%q", obfs.ParamMeta["obfs"].Default)
	}
	if _, ok := obfs.PeerSecretFields["obfs"]; !ok {
		t.Fatalf("hy1_obfs peer secrets=%v", obfs.PeerSecretFields)
	}
}

func TestAllCustomParamMetaLocalized(t *testing.T) {
	protos, err := ListOwnedProtocols()
	if err != nil {
		t.Fatal(err)
	}
	for _, proto := range protos {
		p, err := GetProtocol(proto)
		if err != nil {
			t.Fatalf("%s: %v", proto, err)
		}
		var custom string
		for _, tag := range p.InvariantTags {
			if strings.HasSuffix(tag, "_custom") {
				custom = tag
				break
			}
		}
		if custom == "" {
			t.Fatalf("%s: no *_custom invariant", proto)
		}
		inv, err := GetInvariant(custom)
		if err != nil {
			t.Fatalf("%s: %v", custom, err)
		}
		keys := map[string]struct{}{}
		for _, k := range inv.ParamFields {
			keys[k] = struct{}{}
		}
		for _, k := range inv.OptionalParamFields {
			keys[k] = struct{}{}
		}
		for k := range inv.ParamMeta {
			keys[k] = struct{}{}
		}
		for k := range keys {
			meta, ok := inv.ParamMeta[k]
			if !ok {
				t.Fatalf("%s: field %q missing ParamMeta", custom, k)
			}
			if strings.TrimSpace(meta.Title["en"]) == "" || strings.TrimSpace(meta.Title["ru"]) == "" {
				t.Fatalf("%s.%s: title en/ru required, got %#v", custom, k, meta.Title)
			}
			if strings.TrimSpace(meta.Description["en"]) == "" || strings.TrimSpace(meta.Description["ru"]) == "" {
				t.Fatalf("%s.%s: description en/ru required, got %#v", custom, k, meta.Description)
			}
			if meta.Description["en"] == k || meta.Description["ru"] == k {
				t.Fatalf("%s.%s: description must not be bare key name", custom, k)
			}
		}
	}
}

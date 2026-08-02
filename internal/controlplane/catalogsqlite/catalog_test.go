//go:build with_controlplane

package catalogsqlite

import (
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
		"vless_grpc_tls", "vless_quic_tls", "vless_hysteria_tls", "vless_tls_mux",
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
	if _, ok := inv.InboundTemplate["transport"]; ok {
		t.Fatalf("mux inbound must not keep base transport: %#v", inv.InboundTemplate["transport"])
	}
	if inv.InboundTemplate["multiplex"] == nil {
		t.Fatal("mux inbound missing multiplex")
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

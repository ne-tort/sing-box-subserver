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

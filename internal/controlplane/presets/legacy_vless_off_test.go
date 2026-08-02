//go:build with_controlplane

package presets

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/catalogsqlite"
)

func TestLegacyVlessJSONRemovedFromEmbed(t *testing.T) {
	if _, err := dataFS.Open("data/vless/protocol.json"); err == nil {
		t.Fatal("legacy presets/data/vless still embedded")
	}
	ensureLoaded()
	mustOK()
	if _, ok := protocolBy["vless"]; ok {
		t.Fatal("JSON loader still registered protocol vless")
	}
	for _, inv := range invariants {
		if inv.Protocol == "vless" || inv.Tag == "vless_custom" || inv.Tag == "vless_ws_tls" {
			t.Fatalf("JSON invariants still contain vless entry %q", inv.Tag)
		}
	}
	if !catalogsqlite.Owns("vless_ws_tls") {
		t.Fatal("vless_ws_tls must be owned by catalogsqlite")
	}
	if _, err := Get("vless_ws_tls"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range Protocols() {
		if p.Tag == "vless" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Protocols() must still expose vless from catalogsqlite")
	}
}

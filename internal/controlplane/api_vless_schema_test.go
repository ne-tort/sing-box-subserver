//go:build with_controlplane

package controlplane

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	cpi18n "github.com/ne-tort/sing-box-subserver/internal/controlplane/presets/i18n"
)

// API-shape smoke: ready VLESS presets advertise the full constructor schema.
func TestAPIVlessReadyFullConstructorSchema(t *testing.T) {
	pp, err := presets.Get("vless_ws_tls")
	if err != nil {
		t.Fatal(err)
	}
	if !pp.CustomPreset {
		t.Fatal("custom_preset required for ready presets")
	}
	schema := buildParamsSchemaLang(pp, true, "en")
	for _, key := range []string{"transport", "tls_mode", "flow", "packet_encoding", "fingerprint", "transport_path", "multiplex", "ws_max_early_data"} {
		m, ok := schema[key].(map[string]any)
		if !ok {
			t.Fatalf("schema missing %s", key)
		}
		if key == "flow" {
			vw, _ := m["visible_when"].([]any)
			if len(vw) == 0 {
				t.Fatal("flow.visible_when empty")
			}
		}
	}
	if schema["transport"].(map[string]any)["default"] != "ws" {
		t.Fatalf("transport default=%v", schema["transport"].(map[string]any)["default"])
	}
	// Param copy must resolve via base constructor locale keys.
	help, _ := schema["transport"].(map[string]any)["help"].(map[string]any)
	if help == nil || help["summary"] == nil || help["summary"] == "" {
		t.Fatalf("transport help.summary missing (expected param.vless_custom.* locale): %#v", schema["transport"])
	}
	base, err := presets.Get("vless_custom")
	if err != nil {
		t.Fatal(err)
	}
	baseSchema := buildParamsSchemaLang(base, true, "en")
	for k := range baseSchema {
		if k[0] == '_' {
			continue
		}
		if _, ok := schema[k]; !ok {
			t.Fatalf("ready schema missing base key %s", k)
		}
	}
	// Localized ready descriptions must stay tag-specific.
	if cpi18n.Preset("vless_ws_tls", "description", "en") == "" {
		t.Fatal("missing preset.vless_ws_tls.description")
	}
	if cpi18n.Preset("vless_ws_tls", "title", "en") == "" {
		t.Fatal("missing preset.vless_ws_tls.title")
	}
}

//go:build with_controlplane

package controlplane

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

func TestRequestLangSources(t *testing.T) {
	t.Parallel()
	mk := func(q, xLang, accept string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x?"+q, nil)
		if xLang != "" {
			r.Header.Set("X-Lang", xLang)
		}
		if accept != "" {
			r.Header.Set("Accept-Language", accept)
		}
		return r
	}
	if got := requestLang(mk("lang=en-US", "", "")); got != "en" {
		t.Fatalf("query: got %q", got)
	}
	if got := requestLang(mk("", "ru-RU", "en")); got != "ru" {
		t.Fatalf("X-Lang wins over Accept: got %q", got)
	}
	if got := requestLang(mk("", "", "en-GB,en;q=0.8")); got != "en" {
		t.Fatalf("Accept-Language: got %q", got)
	}
	if got := requestLang(mk("", "", "")); got != "ru" {
		t.Fatalf("default: got %q", got)
	}
}

func TestParamsSchemaVlessCustomLocalizedHelp(t *testing.T) {
	t.Parallel()
	inv, err := presets.GetInvariant("vless_custom")
	if err != nil {
		t.Fatal(err)
	}
	pp := inv.ToProtocolPreset("ru")
	schema := buildParamsSchemaLang(pp, true, "ru")
	tr, ok := schema["transport"].(map[string]any)
	if !ok {
		t.Fatalf("transport missing: %#v", schema)
	}
	help, _ := tr["help"].(map[string]any)
	sum, _ := help["summary"].(string)
	if sum == "" || len(sum) < 20 {
		t.Fatalf("weak transport.help.summary: %q", sum)
	}
	desc, _ := tr["description"].(string)
	if desc == "" {
		t.Fatal("transport.description empty")
	}

	schemaEn := buildParamsSchemaLang(pp, true, "en")
	trEn := schemaEn["transport"].(map[string]any)
	helpEn := trEn["help"].(map[string]any)
	if helpEn["summary"] == help["summary"] {
		// both languages should differ for this key
		t.Fatalf("expected different ru/en summaries")
	}
}

func TestParamsSchemaCommonFallbackWS(t *testing.T) {
	t.Parallel()
	inv, err := presets.GetInvariant("vless_ws_tls")
	if err != nil {
		t.Fatal(err)
	}
	pp := inv.ToProtocolPreset("en")
	schema := buildParamsSchemaLang(pp, true, "en")
	path, ok := schema["ws_path"].(map[string]any)
	if !ok {
		t.Fatal("ws_path missing")
	}
	help, _ := path["help"].(map[string]any)
	if help["summary"] == nil || help["summary"] == "" {
		t.Fatalf("ws_path help missing: %#v", path)
	}
}

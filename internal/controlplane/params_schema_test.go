//go:build with_controlplane

package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

func TestParamsSchemaCarrierRoomRequired(t *testing.T) {
	inv, err := presets.GetInvariant("carrier_jitsi_shared")
	if err != nil {
		t.Fatal(err)
	}
	pp := inv.ToProtocolPreset("en")
	schema := buildParamsSchema(pp, true)
	room, ok := schema["room"].(map[string]any)
	if !ok {
		t.Fatalf("room missing: %#v", schema)
	}
	if req, _ := room["required"].(bool); !req {
		t.Fatalf("room.required want true, got %#v", room["required"])
	}
	if title, _ := room["title"].(string); title == "" {
		t.Fatalf("room.title missing: %#v", room)
	}
	if _, ok := room["help"]; !ok {
		t.Fatalf("room.help missing: %#v", room)
	}
	if _, ok := room["required_guide"]; !ok {
		t.Fatalf("room.required_guide missing: %#v", room)
	}
	opt := presetOptionalParamsDetail(pp)
	if _, exists := opt["room"]; exists {
		t.Fatalf("room must not appear in optional_params: %#v", opt)
	}
	if _, exists := opt["listen_port"]; !exists {
		t.Fatal("listen_port missing from optional_params")
	}
}

func TestParamsSchemaVlessNoRequiredExtras(t *testing.T) {
	// Stock Reality is now a ready preset on the VLESS constructor (full schema).
	inv, err := presets.GetInvariant("vless_reality")
	if err != nil {
		t.Fatal(err)
	}
	pp := inv.ToProtocolPreset("en")
	if !pp.CustomPreset {
		t.Fatal("vless_reality must be custom_preset (constructor schema)")
	}
	if len(pp.ParamFields) == 0 {
		t.Fatal("expected constructor required param_fields")
	}
	schema := buildParamsSchema(pp, false)
	for k, v := range schema {
		if strings.HasPrefix(k, "_") {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%s: expected map, got %T", k, v)
		}
		// Only constructor required keys may be required.
		if req, _ := m["required"].(bool); req && k != "transport" && k != "tls_mode" && k != "listen_port" {
			t.Fatalf("%s unexpectedly required", k)
		}
	}
	if ver, ok := schema["_schema_version"].(int); !ok || ver != 2 {
		t.Fatalf("_schema_version want 2, got %#v", schema["_schema_version"])
	}
	// Full constructor schema may expose ACME sni (user can switch tls_mode to tls).
}

func TestParamsSchemaTlsListExposesSSLProfile(t *testing.T) {
	inv, err := presets.GetInvariant("vless_tls")
	if err != nil {
		t.Fatal(err)
	}
	pp := inv.ToProtocolPreset("en")
	schema := buildParamsSchema(pp, false)
	sp, ok := schema["ssl_profile"].(map[string]any)
	if !ok {
		t.Fatalf("ssl_profile missing from list schema: %#v", schema)
	}
	if req, _ := sp["required"].(bool); req {
		t.Fatal("ssl_profile must be optional")
	}
	if _, has := schema["sni"]; has {
		t.Fatal("legacy sni must not appear in schema")
	}
}

func TestParamFieldDescriptionRoom(t *testing.T) {
	pp := domain.ProtocolPreset{Name: "carrier_jitsi_shared", Protocol: "carrier"}
	d := paramFieldDescription("room", pp, "en")
	if !strings.Contains(strings.ToLower(d), "required") && !strings.Contains(strings.ToLower(d), "room") {
		t.Fatalf("weak description: %q", d)
	}
}

func TestAllRequiredParamFieldsHaveGuides(t *testing.T) {
	tags := []string{
		"carrier_jitsi_shared",
		"carrier_jitsi_users",
		"carrier_jitsi_sei_shared",
		"carrier_jitsi_sei_users",
		"carrier_telemost_shared",
		"carrier_telemost_users",
		"carrier_wbstream_shared",
		"carrier_wbstream_users",
		"cloudflared_token",
	}
	for _, tag := range tags {
		inv, err := presets.GetInvariant(tag)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		pp := inv.ToProtocolPreset("ru")
		if len(pp.ParamFields) == 0 {
			t.Fatalf("%s: expected param_fields", tag)
		}
		schema := buildParamsSchemaLang(pp, true, "ru")
		for _, field := range pp.ParamFields {
			m, ok := schema[field].(map[string]any)
			if !ok {
				t.Fatalf("%s: schema missing %s", tag, field)
			}
			if req, _ := m["required"].(bool); !req {
				t.Fatalf("%s.%s: want required", tag, field)
			}
			if _, ok := m["help"]; !ok {
				t.Fatalf("%s.%s: help missing", tag, field)
			}
			guide, ok := m["required_guide"].(map[string]any)
			if !ok {
				t.Fatalf("%s.%s: required_guide missing", tag, field)
			}
			steps, _ := guide["steps"].([]any)
			if len(steps) == 0 {
				t.Fatalf("%s.%s: required_guide.steps empty", tag, field)
			}
			meta := pp.ParamMeta[field]
			hasSingular := meta.RequiredGuide != nil && len(meta.RequiredGuide.Steps) > 0
			hasPlural := len(meta.RequiredGuides) > 0 && len(meta.RequiredGuides[0].Steps) > 0
			if !hasSingular && !hasPlural {
				t.Fatalf("%s.%s: preset JSON param_meta.required_guide(s) missing", tag, field)
			}
		}
	}
}

func TestOkJSONETag304(t *testing.T) {
	data := map[string]any{"hello": "world"}
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	okJSONETag(rec1, req1, data)
	if rec1.Code != 200 {
		t.Fatalf("code=%d body=%s", rec1.Code, rec1.Body.String())
	}
	etag := rec1.Header().Get("ETag")
	if !strings.HasPrefix(etag, `"sha256:`) {
		t.Fatalf("etag=%q", etag)
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("If-None-Match", etag)
	okJSONETag(rec2, req2, data)
	if rec2.Code != http.StatusNotModified {
		t.Fatalf("want 304, got %d body=%s", rec2.Code, rec2.Body.String())
	}
	if rec2.Body.Len() != 0 {
		t.Fatalf("304 must have empty body, got %q", rec2.Body.String())
	}

	// Clients / shells often strip quotes from If-None-Match.
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.Header.Set("If-None-Match", strings.Trim(etag, `"`))
	okJSONETag(rec3, req3, data)
	if rec3.Code != http.StatusNotModified {
		t.Fatalf("unquoted If-None-Match: want 304, got %d", rec3.Code)
	}
}

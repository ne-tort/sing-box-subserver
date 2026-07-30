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
	opt := presetOptionalParamsDetail(pp)
	if _, exists := opt["room"]; exists {
		t.Fatalf("room must not appear in optional_params: %#v", opt)
	}
	if _, exists := opt["listen_port"]; !exists {
		t.Fatal("listen_port missing from optional_params")
	}
}

func TestParamsSchemaVlessNoRequiredExtras(t *testing.T) {
	inv, err := presets.GetInvariant("vless_reality")
	if err != nil {
		t.Fatal(err)
	}
	pp := inv.ToProtocolPreset("en")
	if len(pp.ParamFields) != 0 {
		t.Fatalf("unexpected param_fields=%v", pp.ParamFields)
	}
	schema := buildParamsSchema(pp, false)
	for k, v := range schema {
		m := v.(map[string]any)
		if req, _ := m["required"].(bool); req {
			t.Fatalf("%s unexpectedly required", k)
		}
	}
}

func TestParamFieldDescriptionRoom(t *testing.T) {
	pp := domain.ProtocolPreset{Name: "carrier_jitsi_shared", Protocol: "carrier"}
	d := paramFieldDescription("room", pp)
	if !strings.Contains(strings.ToLower(d), "required") && !strings.Contains(strings.ToLower(d), "room") {
		t.Fatalf("weak description: %q", d)
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

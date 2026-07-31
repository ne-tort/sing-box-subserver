//go:build with_controlplane

package paramvalidate_test

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/paramvalidate"
)

func TestValidateRequiredAndEnum(t *testing.T) {
	req := true
	pp := domain.ProtocolPreset{
		Name:        "vless_custom",
		ParamFields: []string{"transport", "tls_mode"},
		ParamMeta: map[string]domain.ParamFieldMeta{
			"transport": {Type: "enum", Enum: []string{"tcp", "ws"}, Required: &req},
			"tls_mode":  {Type: "enum", Enum: []string{"none", "tls", "reality"}, Required: &req},
			"flow": {
				Type:          "enum",
				Enum:          []string{"", "xtls-rprx-vision"},
				ConflictsWith: []string{"packet_encoding"},
				VisibleWhen:   []domain.ParamCondition{{Key: "transport", Equals: "tcp"}},
			},
		},
	}
	if err := paramvalidate.Validate(pp, map[string]string{}); err == nil {
		t.Fatal("expected required error")
	}
	if err := paramvalidate.Validate(pp, map[string]string{"transport": "tcp", "tls_mode": "weird"}); err == nil {
		t.Fatal("expected enum error")
	}
	if err := paramvalidate.Validate(pp, map[string]string{"transport": "tcp", "tls_mode": "tls"}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateConflicts(t *testing.T) {
	pp := domain.ProtocolPreset{
		Name: "x",
		ParamMeta: map[string]domain.ParamFieldMeta{
			"a": {ConflictsWith: []string{"b"}},
			"b": {},
		},
	}
	err := paramvalidate.Validate(pp, map[string]string{"a": "1", "b": "2"})
	if err == nil {
		t.Fatal("expected conflict")
	}
	pe, ok := err.(*paramvalidate.Error)
	if !ok || pe.Code != "cp_param_conflict" {
		t.Fatalf("got %#v", err)
	}
}

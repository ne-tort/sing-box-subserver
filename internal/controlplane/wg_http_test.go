//go:build with_controlplane

package controlplane

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func TestMergeWgAWGOverrides(t *testing.T) {
	h := domain.WgHub{
		Profile: domain.WgProfileAWG2,
		AWG:     map[string]any{"jc": 3, "jmin": 40, "jmax": 70, "s1": 12},
	}
	if err := mergeWgAWGOverrides(&h, map[string]any{
		"jc":  5.0,
		"awg": map[string]any{"jmin": 50.0, "jmax": 80.0, "s2": 20.0, "h1": "1-2"},
	}); err != nil {
		t.Fatal(err)
	}
	if h.AWG["jc"] != 5 || h.AWG["jmin"] != 50 || h.AWG["jmax"] != 80 {
		t.Fatalf("awg=%#v", h.AWG)
	}
	if h.AWG["s1"] != 12 || h.AWG["s2"] != 20 || h.AWG["h1"] != "1-2" {
		t.Fatalf("s/h override awg=%#v", h.AWG)
	}
	if err := mergeWgAWGOverrides(&h, map[string]any{
		"masquerade_mode": "quic",
		"masquerade_url":  "https://www.cloudflare.com/path",
	}); err != nil {
		t.Fatal(err)
	}
	if h.AWG["ip"] != "quic" || h.AWG["id"] != "www.cloudflare.com" {
		t.Fatalf("masquerade awg=%#v", h.AWG)
	}
	if err := mergeWgAWGOverrides(&h, map[string]any{"awg": map[string]any{"i1": "<b 0x01>"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.AWG["ip"]; ok {
		t.Fatal("manual i1 must clear masquerade sugar")
	}
	if h.AWG["i1"] != "<b 0x01>" {
		t.Fatalf("i1=%#v", h.AWG["i1"])
	}
	if err := mergeWgAWGOverrides(&h, map[string]any{"jmin": 90.0, "jmax": 80.0}); err == nil {
		t.Fatal("jmax < jmin must fail")
	}
}

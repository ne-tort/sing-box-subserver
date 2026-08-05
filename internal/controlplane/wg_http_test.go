//go:build with_controlplane

package controlplane

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func TestMergeWgObfuscationNested(t *testing.T) {
	h := domain.WgHub{
		Profile: domain.WgProfileAWG2,
		AWG2:    map[string]any{"jc": 3, "jmin": 40, "jmax": 70, "s1": 12},
	}
	if err := mergeWgObfuscation(&h, map[string]any{
		"awg2": map[string]any{"jc": 5.0, "jmin": 50.0, "jmax": 80.0, "s2": 20.0, "h1": "1-2", "ip": "quic"},
	}); err != nil {
		t.Fatal(err)
	}
	if h.AWG2["jc"] != 5 || h.AWG2["jmin"] != 50 || h.AWG2["jmax"] != 80 {
		t.Fatalf("awg2=%#v", h.AWG2)
	}
	if h.AWG2["s1"] != 12 || h.AWG2["s2"] != 20 || h.AWG2["h1"] != "1-2" {
		t.Fatalf("s/h override awg2=%#v", h.AWG2)
	}
	if h.AWG2["ip"] != "quic" {
		t.Fatalf("ip=%#v", h.AWG2["ip"])
	}
	if err := mergeWgObfuscation(&h, map[string]any{"awg2": map[string]any{"i1": "<b 0x01>"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.AWG2["ip"]; ok {
		t.Fatal("manual i1 must clear masquerade sugar")
	}
	if h.AWG2["i1"] != "<b 0x01>" {
		t.Fatalf("i1=%#v", h.AWG2["i1"])
	}
	if err := mergeWgObfuscation(&h, map[string]any{"awg2": map[string]any{"jmin": 90.0, "jmax": 80.0}}); err == nil {
		t.Fatal("jmax < jmin must fail")
	}
}

func TestRejectFlatAWGBody(t *testing.T) {
	if err := rejectFlatAWGBody(map[string]any{"jc": 4}); err == nil {
		t.Fatal("want reject flat jc")
	}
	if err := rejectFlatAWGBody(map[string]any{"awg": map[string]any{"jc": 1}}); err == nil {
		t.Fatal("want reject legacy awg")
	}
	if err := rejectFlatAWGBody(map[string]any{"awg2": map[string]any{"jc": 1}}); err != nil {
		t.Fatal(err)
	}
}

func TestMergePathology(t *testing.T) {
	h := domain.WgHub{Profile: domain.WgProfilePathology}
	if err := mergeWgObfuscation(&h, map[string]any{
		"pathology": map[string]any{"key": "abc", "auto": true, "persona": "balanced"},
	}); err != nil {
		t.Fatal(err)
	}
	if h.Pathology["key"] != "abc" || h.Pathology["auto"] != true {
		t.Fatalf("%#v", h.Pathology)
	}
	if _, ok := h.Pathology["persona"]; ok {
		t.Fatalf("auto=true must strip advanced knobs: %#v", h.Pathology)
	}

	h2 := domain.WgHub{Profile: domain.WgProfilePathology}
	if err := mergeWgObfuscation(&h2, map[string]any{
		"pathology": map[string]any{
			"key": "abc", "auto": false, "persona": "balanced", "preset": "balanced",
			"intensity": 3.0, "frame": "tls13", "cipher": "stream", "dialog": "auto",
			"pad_budget": 64.0,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if h2.Pathology["persona"] != "balanced" || h2.Pathology["intensity"] != 3 {
		t.Fatalf("%#v", h2.Pathology)
	}

	h3 := domain.WgHub{Profile: domain.WgProfilePathology}
	if err := mergeWgObfuscation(&h3, map[string]any{
		"pathology": map[string]any{"key": "abc", "auto": false, "persona": "web"},
	}); err == nil {
		t.Fatal("invalid persona must fail")
	}

	h4 := domain.WgHub{Profile: domain.WgProfilePathology}
	if err := mergeWgObfuscation(&h4, map[string]any{
		"pathology": map[string]any{"key": "abc", "auto": false, "pad_budget": 999.0},
	}); err == nil {
		t.Fatal("pad_budget out of range must fail")
	}
}

func TestMergeAWGRange(t *testing.T) {
	h := domain.WgHub{Profile: domain.WgProfileAWG2, AWG2: map[string]any{}}
	if err := mergeWgObfuscation(&h, map[string]any{
		"awg2": map[string]any{"jc": 200.0},
	}); err == nil {
		t.Fatal("jc out of range must fail")
	}
}

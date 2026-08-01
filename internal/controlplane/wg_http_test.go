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
		"jc": 5.0,
		"awg": map[string]any{"jmin": 50.0, "jmax": 80.0},
	}); err != nil {
		t.Fatal(err)
	}
	if h.AWG["jc"] != 5 || h.AWG["jmin"] != 50 || h.AWG["jmax"] != 80 {
		t.Fatalf("awg=%#v", h.AWG)
	}
	if h.AWG["s1"] != 12 {
		t.Fatal("non-override keys must stay")
	}
	if err := mergeWgAWGOverrides(&h, map[string]any{"awg": map[string]any{"i1": "x"}}); err == nil {
		t.Fatal("i1 must be rejected")
	}
	if err := mergeWgAWGOverrides(&h, map[string]any{"jmin": 90.0, "jmax": 80.0}); err == nil {
		t.Fatal("jmax < jmin must fail")
	}
}

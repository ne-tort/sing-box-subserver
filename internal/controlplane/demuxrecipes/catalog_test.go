//go:build with_controlplane

package demuxrecipes

import "testing"

func TestValidateTemplateOK(t *testing.T) {
	for _, r := range All() {
		if err := ValidateTemplate(r.DemuxTemplate); err != nil {
			t.Fatalf("recipe %s: %v", r.Name, err)
		}
	}
}

func TestValidateTemplateRejectsEmptyTLS(t *testing.T) {
	err := ValidateTemplate(map[string]any{
		"network": []any{"tcp"},
		"rules": []any{
			map[string]any{
				"name":   "bad",
				"match":  map[string]any{"tls": map[string]any{}},
				"action": map[string]any{"reject": true},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for empty tls match")
	}
}

func TestCatalogUnique(t *testing.T) {
	seen := map[string]struct{}{}
	for _, r := range All() {
		if _, ok := seen[r.Name]; ok {
			t.Fatalf("duplicate recipe %s", r.Name)
		}
		seen[r.Name] = struct{}{}
		if len(r.RequiredPresets) == 0 {
			t.Fatalf("%s: required_presets empty", r.Name)
		}
	}
}

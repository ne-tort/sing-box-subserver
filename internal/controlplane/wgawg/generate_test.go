//go:build with_controlplane

package wgawg

import "testing"

func TestBundleAWG2HasMasqueradeNoISlots(t *testing.T) {
	t.Parallel()
	m, err := Bundle(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4", "id", "ip", "ib"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5", "header_protection_key"} {
		if _, ok := m[k]; ok {
			t.Fatalf("unexpected %s", k)
		}
	}
}

func TestBundleAWG3HasHP(t *testing.T) {
	t.Parallel()
	m, err := Bundle(true)
	if err != nil {
		t.Fatal(err)
	}
	if m["header_protection_key"] == "" {
		t.Fatal("missing HP key")
	}
	if m["id"] == "" || m["ip"] == "" {
		t.Fatalf("masquerade missing: %v", m)
	}
}

//go:build with_controlplane

package controlplane

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSlotSelfSigned(t *testing.T) {
	dir := t.TempDir()
	c1, k1, ch1, err := ensureSlotSelfSigned(dir, "www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !ch1 {
		t.Fatal("expected first write changed=true")
	}
	if _, err := os.Stat(c1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(k1); err != nil {
		t.Fatal(err)
	}
	c2, k2, ch2, err := ensureSlotSelfSigned(dir, "www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ch2 {
		t.Fatal("expected reuse changed=false")
	}
	if c1 != c2 || k1 != k2 {
		t.Fatalf("paths differ: %s/%s vs %s/%s", c1, k1, c2, k2)
	}
	if filepath.Base(c1) != "www.example.com.crt" {
		t.Fatalf("unexpected name %s", c1)
	}
}

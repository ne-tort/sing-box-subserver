//go:build with_controlplane

package controlplane

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func TestWriteSelfSignedPair(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.crt")
	key := filepath.Join(dir, "cert.key")
	meta := cert + ".meta"
	spec := domain.SelfSignedSpec{
		CommonName: "vpn.test",
		DNSSANs:    []string{"vpn.test"},
		KeyType:    "p256",
		ValidDays:  30,
	}
	if _, _, err := writeSelfSignedPair(cert, key, meta, spec, fingerprintSpec(spec)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cert); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(key); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureSlotSelfSigned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c1, k1, ch1, err := ensureSlotSelfSigned(dir, "www.apple.com")
	if err != nil {
		t.Fatal(err)
	}
	if !ch1 {
		t.Fatal("first write must change")
	}
	c2, k2, ch2, err := ensureSlotSelfSigned(dir, "www.apple.com")
	if err != nil {
		t.Fatal(err)
	}
	if ch2 {
		t.Fatal("reuse must not change")
	}
	if c1 != c2 || k1 != k2 {
		t.Fatalf("paths differ")
	}
	if _, _, _, err := ensureSlotSelfSigned(dir, "foo.local"); err == nil {
		t.Fatal("expected .local rejection")
	}
}

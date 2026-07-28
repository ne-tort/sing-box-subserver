//go:build with_controlplane

package controlplane

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func TestEnsureSelfSignedReuseAndForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	spec := domain.SelfSignedSpec{
		CommonName: "vpn.test",
		DNSSANs:    []string{"vpn.test", "localhost"},
		KeyType:    "p256",
		ValidDays:  30,
	}
	cert1, key1, changed, err := ensureSelfSigned(dir, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first write must change")
	}
	b1, err := os.ReadFile(cert1)
	if err != nil {
		t.Fatal(err)
	}
	_, _, changed, err = ensureSelfSigned(dir, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("same fingerprint must reuse")
	}
	b2, err := os.ReadFile(cert1)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) != string(b2) {
		t.Fatal("cert bytes changed on reuse")
	}
	_, _, changed, err = ensureSelfSigned(dir, spec, true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("force must rewrite")
	}
	b3, err := os.ReadFile(cert1)
	if err != nil {
		t.Fatal(err)
	}
	if string(b1) == string(b3) {
		t.Fatal("force rewrite must change cert bytes")
	}
	if _, err := os.Stat(key1); err != nil {
		t.Fatal(err)
	}
	meta := filepath.Join(dir, "controlplane", "tls", "self_signed.meta.json")
	if _, err := os.Stat(meta); err != nil {
		t.Fatal(err)
	}

	// Spec change → rewrite without force.
	spec.ValidDays = 60
	_, _, changed, err = ensureSelfSigned(dir, spec, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("spec change must rewrite")
	}
}

//go:build with_controlplane

package controlplane

import (
	"os"
	"path/filepath"
	"testing"
)

func TestACMECertificateReady(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "controlplane", "acme", "certificates", "acme-v02.api.letsencrypt.org-directory", "vpn.example.com")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	ready, missing, found := acmeCertificateReady(dir, []string{"vpn.example.com"})
	if ready || len(found) != 0 || len(missing) != 1 {
		t.Fatalf("before write: ready=%v missing=%v found=%v", ready, missing, found)
	}
	if err := os.WriteFile(filepath.Join(root, "vpn.example.com.crt"), []byte("CERT"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, missing, found = acmeCertificateReady(dir, []string{"vpn.example.com", "other.example.com"})
	if ready || len(found) != 1 || len(missing) != 1 {
		t.Fatalf("partial: ready=%v missing=%v found=%v", ready, missing, found)
	}
	other := filepath.Join(dir, "controlplane", "acme", "certificates", "acme-v02.api.letsencrypt.org-directory", "other.example.com")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "other.example.com.crt"), []byte("CERT"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready, missing, found = acmeCertificateReady(dir, []string{"vpn.example.com", "other.example.com"})
	if !ready || len(missing) != 0 || len(found) != 2 {
		t.Fatalf("full: ready=%v missing=%v found=%v", ready, missing, found)
	}
}

func TestListIssuedAcmeDomains(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "controlplane", "acme", "certificates", "acme-v02.api.letsencrypt.org-directory")
	for _, name := range []string{"a.example.com", "b.example.com"} {
		d := filepath.Join(root, name)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, name+".crt"), []byte("CERT"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := listIssuedAcmeDomains(dir)
	if len(got) != 2 || got[0] != "a.example.com" || got[1] != "b.example.com" {
		t.Fatalf("issued=%v", got)
	}
}

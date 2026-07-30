//go:build with_controlplane

package controlplane

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
)

func TestCertManagerMigratesLegacyTLSACME(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	legacy := map[string]any{
		"mode": "acme_domain",
		"acme": map[string]any{
			"email":   "ops@example.com",
			"domains": []string{"vpn.example.com"},
			"provider": "letsencrypt",
		},
	}
	raw, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(dir, "controlplane", "tls_profile.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	svc := &Service{store: st, cfg: Deps{DataDir: dir}}
	cm, err := svc.ensureCertManager()
	if err != nil {
		t.Fatal(err)
	}
	if !cm.Enabled() || !cm.HasDomain("vpn.example.com") {
		t.Fatalf("migrated cm=%#v", cm)
	}
	loaded, ok, err := st.LoadCertManager()
	if err != nil || !ok {
		t.Fatalf("persist: ok=%v err=%v", ok, err)
	}
	if loaded.Email != "ops@example.com" {
		t.Fatalf("email=%q", loaded.Email)
	}
	// TLS profile rewritten to self_signed.
	p, ok, err := st.LoadTLSProfile()
	if err != nil || !ok || p.SelfSigned == nil {
		t.Fatalf("tls after migrate: ok=%v p=%#v err=%v", ok, p, err)
	}
}

func TestValidateBindingParamsRejectsRealitySNI(t *testing.T) {
	t.Parallel()
	cm := domain.CertManager{Email: "a@b.c", Domains: []string{"vpn.example.com"}}
	p := domain.ProtocolPreset{Name: "vless-reality-tcp", Traits: []string{"tls", "reality"}}
	err := validateBindingParams(p, domain.SetBinding{
		Preset: "vless-reality-tcp",
		Params: map[string]string{domain.BindingParamSNI: "vpn.example.com"},
	}, cm)
	if err == nil {
		t.Fatal("expected reject params.sni on Reality")
	}
}

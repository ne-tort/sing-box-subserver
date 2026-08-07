//go:build with_controlplane

package controlplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/freedns"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
)

func TestSSLListIncludesFieldOptions(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{
		DataDir: dir,
		Cfg:     &agentcfg.Config{Controlplane: agentcfg.ControlplaneConfig{PublicHost: "203.0.113.10"}},
	}}
	_ = s.ensureSSLProfiles()
	writeFreeDNSState(t, dir, "203.0.113.10", map[string]freedns.ProviderStatus{
		freedns.ProviderSSLip: {Host: "203-0-113-10.sslip.io", Status: freedns.StatusOK},
	})

	rr := httptest.NewRecorder()
	s.handleSSLList(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/ssl", nil))
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	opts, ok := envelope.Data["options"].(map[string]any)
	if !ok {
		t.Fatalf("missing options: %#v", envelope.Data)
	}
	types, _ := opts["types"].([]any)
	if len(types) < 3 {
		t.Fatalf("types=%v", types)
	}
	provs, _ := opts["providers"].([]any)
	for _, p := range provs {
		if p == "zerossl" && !sslZeroSSLEnabled {
			t.Fatal("zerossl must stay gated off until live test")
		}
	}
	domains, _ := opts["domains"].([]any)
	found := false
	for _, d := range domains {
		if d == "203-0-113-10.sslip.io" {
			found = true
		}
	}
	if !found {
		t.Fatalf("domains=%v", domains)
	}
	ips, _ := opts["ips"].([]any)
	if len(ips) == 0 || ips[0] != "203.0.113.10" {
		t.Fatalf("ips=%v", ips)
	}
	snis, ok := opts["reality_snis"].([]any)
	if !ok || len(snis) == 0 {
		t.Fatalf("reality_snis must not be empty, got %v", opts["reality_snis"])
	}
}

func TestSSLRegenerateClearsACMEMaterial(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	_ = s.ensureSSLProfiles()
	now := time.Now().UTC()
	p := domain.SSLProfile{
		ID: "acme-regen", Name: "ACME", Type: domain.SSLTypeACME,
		Domain: "vpn.example.com", Email: "ops@example.com", Provider: "letsencrypt",
		CreatedAt: now, UpdatedAt: now,
	}.Normalize()
	if err := s.upsertSSLProfile(p); err != nil {
		t.Fatal(err)
	}
	cert, key := sslCertPaths(dir, p.ID)
	if err := os.MkdirAll(filepath.Dir(cert), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cert, []byte("CERT"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	acmeMarker := filepath.Join(sslACMEDir(dir, p.ID), "marker")
	if err := os.MkdirAll(filepath.Dir(acmeMarker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(acmeMarker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := s.clearSSLACMEMaterial(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cert); !os.IsNotExist(err) {
		t.Fatal("leaf cert must be removed")
	}
	if _, err := os.Stat(key); !os.IsNotExist(err) {
		t.Fatal("leaf key must be removed")
	}
	if _, err := os.Stat(acmeMarker); !os.IsNotExist(err) {
		t.Fatal("acme cache must be wiped")
	}
	if st, err := os.Stat(sslACMEDir(dir, p.ID)); err != nil || !st.IsDir() {
		t.Fatal("acme dir must be recreated empty")
	}
}

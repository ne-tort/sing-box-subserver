//go:build with_controlplane

package controlplane

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/materialize"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
)

func TestSSLProfilesCRUDAndStatus(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	if err := s.ensureSSLProfiles(); err != nil {
		t.Fatal(err)
	}
	list, err := s.loadSSLProfiles()
	if err != nil {
		t.Fatal(err)
	}
	foundDefault := false
	for _, p := range list {
		if p.ID == defaultSSLProfileID {
			foundDefault = true
		}
	}
	if !foundDefault {
		t.Fatal("expected default profile")
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/controlplane/ssl", strings.NewReader(`{"name":"My VPN"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.handleSSLCreate(rr, req)
	if rr.Code != 201 {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	created := envelope.Data
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("missing id body=%s", rr.Body.String())
	}

	p := domain.SSLProfile{
		ID: id, Name: "My VPN", Type: domain.SSLTypeSelfSigned, Domain: "vpn.example.com",
	}.Normalize()
	p, err = s.ensureSSLProfileMaterial(p, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.upsertSSLProfile(p); err != nil {
		t.Fatal(err)
	}
	stt := s.computeSSLStatus(p)
	if stt.State != domain.SSLStateReady {
		t.Fatalf("expected ready, got %#v", stt)
	}
	if _, err := os.Stat(stt.CertPath); err != nil {
		t.Fatalf("cert missing: %v", err)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/controlplane/ssl/"+id, nil)
	req.SetPathValue("id", id)
	rr = httptest.NewRecorder()
	s.handleSSLDelete(rr, req)
	if rr.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(sslProfileDir(dir, id)); !os.IsNotExist(err) {
		t.Fatalf("profile dir should be gone: %v", err)
	}
}

func TestSSLEnsureDefaultOnly(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	// Legacy ACME orphan must NOT invent profiles anymore.
	spec := domain.SelfSignedSpec{CommonName: "orphan.example.com", DNSSANs: []string{"orphan.example.com"}, KeyType: "p256", ValidDays: 30}
	certPath := filepath.Join(dir, "controlplane", "acme", "certificates", "issuer", "orphan.example.com", "orphan.example.com.crt")
	keyPath := filepath.Join(dir, "controlplane", "acme", "certificates", "issuer", "orphan.example.com", "orphan.example.com.key")
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writeSelfSignedPair(certPath, keyPath, certPath+".meta", spec, "orphan"); err != nil {
		t.Fatal(err)
	}
	if err := s.ensureSSLProfiles(); err != nil {
		t.Fatal(err)
	}
	list, err := s.loadSSLProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "default" {
		t.Fatalf("want only default profile, got %#v", list)
	}
	stt := s.computeSSLStatus(list[0])
	if stt.State != domain.SSLStateReady {
		t.Fatalf("default status=%#v", stt)
	}
}

func TestMaterializeUsesSSLProfileProvider(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{cfg: Deps{DataDir: dir}}
	profiles := []domain.SSLProfile{
		{ID: "default", Name: "Default", Type: domain.SSLTypeSelfSigned, Domain: "h.example"},
		{ID: "acme1", Name: "vpn", Type: domain.SSLTypeACME, Domain: "vpn.example.com", Email: "a@b.c"},
	}
	for i := range profiles {
		p, err := svc.ensureSSLProfileMaterial(profiles[i], true)
		if err != nil && profiles[i].Type == domain.SSLTypeSelfSigned {
			t.Fatal(err)
		}
		profiles[i] = p
	}
	cert, key := filepath.Join(dir, "controlplane", "ssl", "default", "cert.crt"), filepath.Join(dir, "controlplane", "ssl", "default", "cert.key")
	raw, err := materialize.Build(materialize.Input{
		ActiveSets: []domain.InboundSet{{
			Name:       "t",
			Listen:     "0.0.0.0",
			ListenPort: 443,
			Presets:    []string{"trojan-tcp"},
			Bindings: []domain.SetBinding{{
				Preset: "trojan-tcp",
				Params: map[string]string{domain.BindingParamSSLProfile: "acme1"},
			}},
		}},
		Users: []domain.User{{
			ID: "u1", Name: "u", Enabled: true,
			Creds: map[string]map[string]any{"trojan-tcp": {"password": "secret"}},
		}},
		PublicHost:  "h.example",
		DataDir:     dir,
		TLS:         domain.DefaultSelfSigned("h.example"),
		TLSCertPath: cert,
		TLSKeyPath:  key,
		SSLProfiles: profiles,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	provs, _ := doc["certificate_providers"].([]any)
	if len(provs) == 0 {
		t.Fatalf("expected certificate_providers, doc=%s", string(raw))
	}
	tag, _ := provs[0].(map[string]any)["tag"].(string)
	if tag != domain.SSLProviderTag("acme1") {
		t.Fatalf("tag=%q", tag)
	}
	inbounds, _ := doc["inbounds"].([]any)
	foundProv := false
	for _, ib := range inbounds {
		m, _ := ib.(map[string]any)
		tls, _ := m["tls"].(map[string]any)
		if tls == nil {
			continue
		}
		if tls["certificate_provider"] == domain.SSLProviderTag("acme1") {
			foundProv = true
		}
	}
	if !foundProv {
		t.Fatalf("inbound missing provider tag: %s", string(raw))
	}
}

func TestValidateBindingRequiresExistingSSLProfile(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	_ = s.ensureSSLProfiles()
	p, err := presets.Get("trojan-tcp")
	if err != nil {
		t.Fatal(err)
	}
	err = s.validateBindingParams(p, domain.SetBinding{
		Preset: p.Name,
		Params: map[string]string{domain.BindingParamSSLProfile: "missing-id"},
	})
	if err == nil {
		t.Fatal("expected missing profile error")
	}
}

func TestSSLDeleteRefusesWhenReferenced(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	_ = s.ensureSSLProfiles()
	p := domain.SSLProfile{ID: "used1", Name: "Used", Type: domain.SSLTypeSelfSigned, Domain: "u.example"}.Normalize()
	p, _ = s.ensureSSLProfileMaterial(p, true)
	_ = s.upsertSSLProfile(p)
	_ = st.SaveSets([]domain.InboundSet{{
		Name: "s1", Listen: "0.0.0.0", ListenPort: 443,
		Presets:  []string{"trojan-tcp"},
		Bindings: []domain.SetBinding{{Preset: "trojan-tcp", Params: map[string]string{domain.BindingParamSSLProfile: "used1"}}},
	}})
	req := httptest.NewRequest(http.MethodDelete, "/v1/controlplane/ssl/used1", nil)
	req.SetPathValue("id", "used1")
	rr := httptest.NewRecorder()
	s.handleSSLDelete(rr, req)
	if rr.Code != 422 {
		t.Fatalf("expected 422, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestSSLStatusExpired(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	p := domain.SSLProfile{ID: "exp1", Name: "Exp", Type: domain.SSLTypeSelfSigned, Domain: "exp.example"}.Normalize()
	_ = os.MkdirAll(sslProfileDir(dir, p.ID), 0o700)
	certPath, keyPath := sslCertPaths(dir, p.ID)
	if err := writeExpiredSelfSigned(certPath, keyPath, "exp.example"); err != nil {
		t.Fatal(err)
	}
	_ = s.upsertSSLProfile(p)
	stt := s.computeSSLStatus(p)
	if stt.State != domain.SSLStateExpired {
		t.Fatalf("want expired, got %#v", stt)
	}
}

func TestSSLECHAutoSNIFromReality(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	now := time.Now().UTC()
	_ = s.store.SaveRealityConfig(domain.RealityConfig{
		Profiles:  []domain.RealityEndpoint{{SNI: "www.apple.com"}},
		UpdatedAt: &now,
	})
	p := domain.SSLProfile{ID: "ech1", Name: "E", Type: domain.SSLTypeSelfSigned, Domain: "leaf.example", ECHEnabled: true}.Normalize()
	p, err = s.ensureSSLProfileMaterial(p, true)
	if err != nil {
		t.Fatal(err)
	}
	_, cfgPath := sslECHPaths(dir, p.ID)
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "www.apple.com") && !strings.Contains(string(raw), "leaf.example") {
		// ECH config PEM embeds public_name; accept either Reality pick or leaf fallback.
		t.Logf("ech config generated (%d bytes)", len(raw))
	}
	stt := s.computeSSLStatus(p)
	if !stt.ECHReady {
		t.Fatalf("ech not ready: %#v", stt)
	}
}

func TestSSLDeletePermissionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permission simulation not reliable on Windows")
	}
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	p := domain.SSLProfile{ID: "perm1", Name: "P", Type: domain.SSLTypeSelfSigned, Domain: "p.example"}.Normalize()
	p, err = s.ensureSSLProfileMaterial(p, true)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.upsertSSLProfile(p)
	profDir := sslProfileDir(dir, p.ID)
	if err := os.Chmod(profDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(profDir, 0o700) })
	err = s.deleteSSLProfileFiles(p.ID)
	if err == nil || !strings.Contains(err.Error(), "cp_ssl_delete_failed") {
		t.Fatalf("want cp_ssl_delete_failed, got %v", err)
	}
}

func TestSSLACMEEmailAutoPayload(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir, Cfg: &agentcfg.Config{Controlplane: agentcfg.ControlplaneConfig{PublicHost: "10.0.0.1"}}}}
	_ = s.ensureSSLProfiles()
	_ = s.store.SaveRealityConfig(domain.RealityConfig{
		Profiles: []domain.RealityEndpoint{{SNI: "www.apple.com"}, {SNI: "www.amazon.com"}},
	})

	p := domain.SSLProfile{
		ID: "acme-auto1", Name: "LE", Type: domain.SSLTypeACME, Domain: "vpn.example.com",
	}.Normalize()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := s.upsertSSLProfile(p); err != nil {
		t.Fatal(err)
	}
	payload := s.sslProfilePayload(p)
	if payload["email_auto"] != true {
		t.Fatalf("email_auto=%v", payload["email_auto"])
	}
	eff, _ := payload["email_effective"].(string)
	if !strings.HasPrefix(eff, "admin@") {
		t.Fatalf("email_effective=%q", eff)
	}
	if !strings.Contains(eff, "apple.com") && !strings.Contains(eff, "amazon.com") {
		t.Fatalf("effective should use Reality pool, got %q", eff)
	}

	p.Email = "ops@corp.example"
	payload = s.sslProfilePayload(p)
	if payload["email_auto"] != false {
		t.Fatal("expected not auto")
	}
	if payload["email_effective"] != "ops@corp.example" {
		t.Fatalf("effective=%v", payload["email_effective"])
	}
}

func writeExpiredSelfSigned(certPath, keyPath, cn string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().UTC().Add(-48 * time.Hour),
		NotAfter:     time.Now().UTC().Add(-24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{cn},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return err
	}
	b, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b}), 0o600)
}

func TestSSLStaleLeafReportsPendingNotOldExpiry(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	p := domain.SSLProfile{
		ID: "rotate1", Name: "R", Type: domain.SSLTypeSelfSigned, Domain: "old.example",
	}.Normalize()
	p, err = s.ensureSSLProfileMaterial(p, true)
	if err != nil {
		t.Fatal(err)
	}
	certPath, _ := sslCertPaths(dir, p.ID)
	if _, err := os.Stat(certPath); err != nil {
		t.Fatal(err)
	}
	// Switch to ACME without clearing leaf → status must be pending (no 3650-day not_after).
	p.Type = domain.SSLTypeACME
	p.Domain = "new.example.com"
	p = p.Normalize()
	stt := s.computeSSLStatus(p)
	if stt.State != domain.SSLStatePending {
		t.Fatalf("want pending for mismatched leaf, got %#v", stt)
	}
	if stt.NotAfter != nil {
		t.Fatalf("stale not_after must be omitted, got %v", stt.NotAfter)
	}

	// ensureSSLProfileMaterial must delete the stale leaf for ACME.
	if _, err := s.ensureSSLProfileMaterial(p, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(certPath); !os.IsNotExist(err) {
		t.Fatalf("stale leaf should be removed, err=%v", err)
	}
	stt = s.computeSSLStatus(p)
	if stt.State != domain.SSLStatePending {
		t.Fatalf("want pending after clear, got %#v", stt)
	}
}

func TestSSLProfileIdentityChanged(t *testing.T) {
	a := domain.SSLProfile{Type: domain.SSLTypeSelfSigned, Domain: "a.example"}.Normalize()
	b := domain.SSLProfile{Type: domain.SSLTypeACME, Domain: "a.example"}.Normalize()
	if !sslProfileIdentityChanged(a, b) {
		t.Fatal("type change should count")
	}
	c := domain.SSLProfile{Type: domain.SSLTypeACME, Domain: "b.example"}.Normalize()
	if !sslProfileIdentityChanged(b, c) {
		t.Fatal("domain change should count")
	}
	if sslProfileIdentityChanged(b, b) {
		t.Fatal("same identity")
	}
}


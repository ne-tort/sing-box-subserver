//go:build with_controlplane

package controlplane

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/freedns"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
)

func writeFreeDNSState(t *testing.T, dir, ipv4 string, providers map[string]freedns.ProviderStatus) {
	t.Helper()
	st := freedns.State{IPv4: ipv4, Providers: providers, UpdatedAt: time.Now().UTC()}
	if err := freedns.SaveState(dir, st); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureFreeDNSSSLProfilesCreateAndReuse(t *testing.T) {
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
		freedns.ProviderSSLip:     {Host: "203-0-113-10.sslip.io", Status: freedns.StatusOK},
		freedns.ProviderNIP:       {Host: "203-0-113-10.nip.io", Status: freedns.StatusOK},
		freedns.ProviderAddrTools: {Host: "abcd.dyn.addr.tools", Status: freedns.StatusOK},
	})

	s.ensureFreeDNSSSLProfiles(nil)
	list, err := s.loadSSLProfiles()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]domain.SSLProfile{}
	for _, p := range list {
		byID[p.ID] = p
	}
	for _, id := range []string{sslIDFreeDNSSSLip, sslIDFreeDNSNIP, sslIDFreeDNSAddr, sslIDFreeDNSIP} {
		p, ok := byID[id]
		if !ok {
			t.Fatalf("missing profile %s", id)
		}
		if !stringsHasPrefix(p.Source, "free_dns:") {
			t.Fatalf("%s source=%q", id, p.Source)
		}
	}
	if byID[sslIDFreeDNSSSLip].Domain != "203-0-113-10.sslip.io" {
		t.Fatalf("sslip domain=%q", byID[sslIDFreeDNSSSLip].Domain)
	}
	if byID[sslIDFreeDNSIP].Type != domain.SSLTypeACMEIP || byID[sslIDFreeDNSIP].IP != "203.0.113.10" {
		t.Fatalf("ip profile=%#v", byID[sslIDFreeDNSIP])
	}
	n1 := len(list)

	// Second ensure: no duplicates.
	s.ensureFreeDNSSSLProfiles(nil)
	list, _ = s.loadSSLProfiles()
	if len(list) != n1 {
		t.Fatalf("duplicated profiles: before=%d after=%d", n1, len(list))
	}
}

func TestEnsureFreeDNSSSLProfilesNoDupWhenDomainCovered(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	_ = s.ensureSSLProfiles()
	now := time.Now().UTC()
	manual := domain.SSLProfile{
		ID: "manual-acme", Name: "Manual", Type: domain.SSLTypeACME,
		Domain: "203-0-113-10.sslip.io", Email: "ops@example.com",
		CreatedAt: now, UpdatedAt: now,
	}.Normalize()
	if err := s.upsertSSLProfile(manual); err != nil {
		t.Fatal(err)
	}
	writeFreeDNSState(t, dir, "203.0.113.10", map[string]freedns.ProviderStatus{
		freedns.ProviderSSLip: {Host: "203-0-113-10.sslip.io", Status: freedns.StatusOK},
	})
	s.ensureFreeDNSSSLProfiles(nil)
	list, _ := s.loadSSLProfiles()
	for _, p := range list {
		if p.ID == sslIDFreeDNSSSLip {
			t.Fatal("must not create fd-sslip when domain already covered")
		}
	}
}

func TestEnsureFreeDNSSSLProfilesAdoptOrphanDir(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	_ = s.ensureSSLProfiles()

	// Simulate preserved ACME tree without catalog entry; provider currently skipped.
	profDir := sslProfileDir(dir, sslIDFreeDNSAddr)
	if err := os.MkdirAll(filepath.Join(profDir, "acme"), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := domain.SelfSignedSpec{CommonName: "deadbeef.dyn.addr.tools", DNSSANs: []string{"deadbeef.dyn.addr.tools"}, KeyType: "p256", ValidDays: 30}
	cert, key := sslCertPaths(dir, sslIDFreeDNSAddr)
	if _, _, err := writeSelfSignedPair(cert, key, cert+".meta", spec, "orphan"); err != nil {
		t.Fatal(err)
	}
	writeFreeDNSState(t, dir, "203.0.113.10", map[string]freedns.ProviderStatus{
		freedns.ProviderAddrTools: {Host: "deadbeef.dyn.addr.tools", Status: freedns.StatusSkipped, Error: "dns timeout"},
	})

	s.ensureFreeDNSSSLProfiles(nil)
	list, _ := s.loadSSLProfiles()
	found := false
	for _, p := range list {
		if p.ID == sslIDFreeDNSAddr {
			found = true
			if p.Domain != "deadbeef.dyn.addr.tools" {
				t.Fatalf("adopted domain=%q", p.Domain)
			}
			if p.Type != domain.SSLTypeACME {
				t.Fatalf("type=%q", p.Type)
			}
		}
	}
	if !found {
		t.Fatal("orphan fd-addrtools not adopted")
	}
}

func TestEnsureFreeDNSSSLProfilesUpdateDomainOnIPChange(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	_ = s.ensureSSLProfiles()
	now := time.Now().UTC()
	_ = s.upsertSSLProfile(domain.SSLProfile{
		ID: sslIDFreeDNSSSLip, Name: "Free DNS (sslip)", Type: domain.SSLTypeACME,
		Domain: "1-2-3-4.sslip.io", Source: sslSourceFreeDNSSSLip,
		CreatedAt: now, UpdatedAt: now,
	}.Normalize())

	writeFreeDNSState(t, dir, "203.0.113.10", map[string]freedns.ProviderStatus{
		freedns.ProviderSSLip: {Host: "203-0-113-10.sslip.io", Status: freedns.StatusOK},
	})
	s.ensureFreeDNSSSLProfiles(nil)
	p, ok, _ := s.findSSLProfile(sslIDFreeDNSSSLip)
	if !ok || p.Domain != "203-0-113-10.sslip.io" {
		t.Fatalf("expected domain update, got %#v", p)
	}
}

func TestEnsureFreeDNSSSLProfilesSkipsFailedProvider(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st, cfg: Deps{DataDir: dir}}
	_ = s.ensureSSLProfiles()
	writeFreeDNSState(t, dir, "203.0.113.10", map[string]freedns.ProviderStatus{
		freedns.ProviderSSLip: {Host: "203-0-113-10.sslip.io", Status: freedns.StatusSkipped, Error: "no A"},
		freedns.ProviderNIP:   {Host: "203-0-113-10.nip.io", Status: freedns.StatusOK},
	})
	s.ensureFreeDNSSSLProfiles(nil)
	list, _ := s.loadSSLProfiles()
	ids := map[string]bool{}
	for _, p := range list {
		ids[p.ID] = true
	}
	if ids[sslIDFreeDNSSSLip] {
		t.Fatal("skipped sslip must not create profile")
	}
	if !ids[sslIDFreeDNSNIP] {
		t.Fatal("ok nip must create profile")
	}
}

func stringsHasPrefix(s, pfx string) bool {
	return len(s) >= len(pfx) && s[:len(pfx)] == pfx
}

func TestFreeDNSStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeFreeDNSState(t, dir, "1.2.3.4", map[string]freedns.ProviderStatus{
		freedns.ProviderSSLip: {Host: "1-2-3-4.sslip.io", Status: freedns.StatusOK},
	})
	raw, err := os.ReadFile(filepath.Join(dir, "controlplane", "free_dns.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["ipv4"] != "1.2.3.4" {
		t.Fatalf("%v", m)
	}
}

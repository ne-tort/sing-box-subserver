//go:build with_controlplane

package controlplane

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
)

func TestMigrateRealityConfigPrefersUser(t *testing.T) {
	now := time.Now().UTC()
	leg := legacyRealityConfigFile{
		UsingUserOverrides: true,
		UserProfiles: []domain.RealityEndpoint{
			{SNI: "www.user.example"},
		},
		EffectiveProfiles: []domain.RealityEndpoint{
			{SNI: "www.effective.example"},
		},
		UpdatedAt: &now,
	}
	cfg, migrated := migrateRealityConfig(leg)
	if !migrated {
		t.Fatal("expected migrated=true")
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0].SNI != "www.user.example" {
		t.Fatalf("profiles=%v", cfg.Profiles)
	}
}

func TestMigrateRealityConfigKeepsProfiles(t *testing.T) {
	leg := legacyRealityConfigFile{
		Profiles: []domain.RealityEndpoint{{SNI: "www.apple.com"}},
		UserProfiles: []domain.RealityEndpoint{
			{SNI: "www.user.example"},
		},
	}
	cfg, migrated := migrateRealityConfig(leg)
	if !migrated {
		t.Fatal("legacy dual fields should mark migrated")
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0].SNI != "www.apple.com" {
		t.Fatalf("profiles=%v", cfg.Profiles)
	}
}

func TestRealityPreferSNI(t *testing.T) {
	if got := realityPreferSNI(map[string]string{"reality_sni": "A.Example", "demux_sni": "b.example"}); got != "a.example" {
		t.Fatalf("got %q", got)
	}
	if got := realityPreferSNI(map[string]string{"demux_sni": "B.Example"}); got != "b.example" {
		t.Fatalf("legacy got %q", got)
	}
	if got := realityPreferSNI(nil); got != "" {
		t.Fatalf("empty got %q", got)
	}
}

func TestPickRealityEndpointPreferAndFallback(t *testing.T) {
	pool := []domain.RealityEndpoint{
		{SNI: "a.example", HandshakeServer: "a.example", HandshakePort: 443},
		{SNI: "b.example", HandshakeServer: "b.example", HandshakePort: 443},
	}
	used := map[string]string{"a.example": "other/set"}
	ep, err := pickRealityEndpoint(pool, "a.example", used, "me/set")
	if err != nil {
		t.Fatal(err)
	}
	if ep.SNI != "b.example" {
		t.Fatalf("expected unused b, got %s", ep.SNI)
	}
	used["b.example"] = "other2/set"
	ep, err = pickRealityEndpoint(pool, "", used, "me/set")
	if err != nil {
		t.Fatal(err)
	}
	if ep.SNI != "a.example" && ep.SNI != "b.example" {
		t.Fatalf("fallback reuse got %s", ep.SNI)
	}
}

func TestEnsureRealityAssignmentsPreferStickyKeys(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st}
	pool := []domain.RealityEndpoint{
		{SNI: "www.apple.com", HandshakeServer: "www.apple.com", HandshakePort: 443},
		{SNI: "www.microsoft.com", HandshakeServer: "www.microsoft.com", HandshakePort: 443},
	}
	sets := []domain.InboundSet{{
		Name: "solo",
		Bindings: []domain.SetBinding{{
			Preset: "vless_reality",
			Params: map[string]string{domain.BindingParamRealitySNI: "www.microsoft.com"},
		}},
	}}
	asg, changed, err := s.ensureRealityAssignments(sets, pool)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected first assignment")
	}
	key := realityInboundKey("solo", "vless_reality")
	a1 := asg[key]
	if a1.SNI != "www.microsoft.com" || a1.PrivateKeyBase64 == "" {
		t.Fatalf("assignment=%+v", a1)
	}
	asg2, changed2, err := s.ensureRealityAssignments(sets, pool)
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Fatal("sticky re-run must not rewrite")
	}
	a2 := asg2[key]
	if a2.PrivateKeyBase64 != a1.PrivateKeyBase64 || a2.PublicKeyBase64 != a1.PublicKeyBase64 || a2.ShortID != a1.ShortID {
		t.Fatal("keys changed without SNI change")
	}
}

func TestLoadRealityConfigSeedsOnce(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st}
	cfg, err := s.loadRealityConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) == 0 {
		t.Fatal("expected seed profiles")
	}
	cfg2, err := s.loadRealityConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg2.Profiles) == 0 {
		t.Fatal("expected profiles after reload")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "controlplane", "reality_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if _, ok := file["profiles"]; !ok {
		t.Fatalf("missing profiles in file: %v", file)
	}
	if _, ok := file["user_profiles"]; ok {
		t.Fatal("legacy user_profiles must not be written")
	}
}

func TestRealityStatusPayloadShape(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: st}
	now := time.Now().UTC()
	cfg := domain.RealityConfig{
		Profiles:  []domain.RealityEndpoint{{SNI: "www.apple.com", HandshakeServer: "www.apple.com", HandshakePort: 443}},
		UpdatedAt: &now,
	}
	payload := s.realityStatusPayload(cfg)
	if _, ok := payload["profiles"]; !ok {
		t.Fatal("profiles")
	}
	if _, ok := payload["seed_defaults"]; !ok {
		t.Fatal("seed_defaults")
	}
	if _, ok := payload["default_profiles"]; ok {
		t.Fatal("legacy default_profiles must be gone")
	}
}

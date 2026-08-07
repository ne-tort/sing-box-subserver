//go:build with_controlplane

package controlplane

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

func defaultRealityProfiles() []domain.RealityEndpoint {
	return domain.DefaultRealityProfiles()
}

func normalizeRealityEndpoint(in domain.RealityEndpoint) (domain.RealityEndpoint, error) {
	sni := strings.ToLower(strings.TrimSpace(in.SNI))
	if sni == "" {
		return domain.RealityEndpoint{}, fmt.Errorf("sni required")
	}
	if net.ParseIP(sni) != nil {
		return domain.RealityEndpoint{}, fmt.Errorf("sni must be domain, got ip %q", sni)
	}
	hs := strings.ToLower(strings.TrimSpace(in.HandshakeServer))
	if hs == "" {
		hs = sni
	}
	if net.ParseIP(hs) != nil {
		return domain.RealityEndpoint{}, fmt.Errorf("handshake_server must be domain, got ip %q", hs)
	}
	port := in.HandshakePort
	if port == 0 {
		port = 443
	}
	return domain.RealityEndpoint{
		SNI:             sni,
		HandshakeServer: hs,
		HandshakePort:   port,
	}, nil
}

func randomRealityShortID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func generateRealityKeyPair() (privateRawURL string, publicRawURL string, err error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	privateRawURL = base64.RawURLEncoding.EncodeToString(priv.Bytes())
	publicRawURL = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	return privateRawURL, publicRawURL, nil
}

func realityInboundKey(setName, presetName string) string {
	return setName + "/" + presetName
}

func normalizedRealityDefaults() []domain.RealityEndpoint {
	base := defaultRealityProfiles()
	out := make([]domain.RealityEndpoint, 0, len(base))
	for _, p := range base {
		ep, err := normalizeRealityEndpoint(p)
		if err != nil {
			continue
		}
		out = append(out, ep)
	}
	return out
}

func normalizeRealityPoolInPlace(in []domain.RealityEndpoint) ([]domain.RealityEndpoint, bool) {
	if len(in) == 0 {
		return in, false
	}
	out := make([]domain.RealityEndpoint, 0, len(in))
	changed := false
	seen := map[string]struct{}{}
	for _, raw := range in {
		ep, err := normalizeRealityEndpoint(raw)
		if err != nil {
			changed = true
			continue
		}
		key := ep.SNI + "|" + ep.HandshakeServer + "|" + strconv.Itoa(int(ep.HandshakePort))
		if _, ok := seen[key]; ok {
			changed = true
			continue
		}
		seen[key] = struct{}{}
		if ep != raw {
			changed = true
		}
		out = append(out, ep)
	}
	return out, changed
}

// legacyRealityConfigFile reads old dual-pool JSON for one-shot migration.
type legacyRealityConfigFile struct {
	Profiles           []domain.RealityEndpoint `json:"profiles"`
	UpdatedAt          *time.Time               `json:"updated_at"`
	UserProfiles       []domain.RealityEndpoint `json:"user_profiles"`
	EffectiveProfiles  []domain.RealityEndpoint `json:"effective_profiles"`
	UsingUserOverrides bool                     `json:"using_user_overrides"`
}

func migrateRealityConfig(raw legacyRealityConfigFile) (domain.RealityConfig, bool) {
	now := time.Now().UTC()
	pick := func(list []domain.RealityEndpoint) domain.RealityConfig {
		fixed, _ := normalizeRealityPoolInPlace(list)
		ua := raw.UpdatedAt
		if ua == nil {
			ua = &now
		}
		return domain.RealityConfig{Profiles: fixed, UpdatedAt: ua}
	}
	if len(raw.Profiles) > 0 {
		return pick(raw.Profiles), len(raw.UserProfiles) > 0 || len(raw.EffectiveProfiles) > 0 || raw.UsingUserOverrides
	}
	if raw.UsingUserOverrides && len(raw.UserProfiles) > 0 {
		return pick(raw.UserProfiles), true
	}
	if len(raw.EffectiveProfiles) > 0 {
		return pick(raw.EffectiveProfiles), true
	}
	if len(raw.UserProfiles) > 0 {
		return pick(raw.UserProfiles), true
	}
	return domain.RealityConfig{}, false
}

func (s *Service) loadRealityConfig() (domain.RealityConfig, error) {
	// Prefer raw read for legacy migration when Profiles is empty but old fields exist.
	if cfg, ok, err := s.loadRealityConfigMigrating(); err != nil {
		return domain.RealityConfig{}, err
	} else if ok {
		if len(cfg.Profiles) == 0 {
			now := time.Now().UTC()
			cfg = domain.RealityConfig{Profiles: normalizedRealityDefaults(), UpdatedAt: &now}
			if err := s.store.SaveRealityConfig(cfg); err != nil {
				return domain.RealityConfig{}, err
			}
			return cfg, nil
		}
		fixed, changed := normalizeRealityPoolInPlace(cfg.Profiles)
		if changed {
			cfg.Profiles = fixed
			if err := s.store.SaveRealityConfig(cfg); err != nil {
				return domain.RealityConfig{}, err
			}
		} else if cfg.UpdatedAt == nil {
			now := time.Now().UTC()
			cfg.UpdatedAt = &now
			_ = s.store.SaveRealityConfig(cfg)
		}
		return cfg, nil
	}
	now := time.Now().UTC()
	cfg := domain.RealityConfig{
		Profiles:  normalizedRealityDefaults(),
		UpdatedAt: &now,
	}
	if err := s.store.SaveRealityConfig(cfg); err != nil {
		return domain.RealityConfig{}, err
	}
	return cfg, nil
}

func (s *Service) loadRealityConfigMigrating() (domain.RealityConfig, bool, error) {
	raw, err := os.ReadFile(s.store.RealityConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return domain.RealityConfig{}, false, nil
		}
		return domain.RealityConfig{}, false, err
	}
	var leg legacyRealityConfigFile
	if err := json.Unmarshal(raw, &leg); err != nil {
		return domain.RealityConfig{}, false, err
	}
	cfg, migrated := migrateRealityConfig(leg)
	if len(cfg.Profiles) > 0 {
		if migrated || len(leg.UserProfiles) > 0 || len(leg.EffectiveProfiles) > 0 {
			if err := s.store.SaveRealityConfig(cfg); err != nil {
				return domain.RealityConfig{}, false, err
			}
		}
		return cfg, true, nil
	}
	// File exists but empty — treat as present empty.
	return domain.RealityConfig{UpdatedAt: leg.UpdatedAt}, true, nil
}

func hasRealityPreset(sets []domain.InboundSet) bool {
	for _, set := range sets {
		for _, b := range set.EffectiveBindings() {
			p, err := presets.Get(b.Preset)
			if err != nil {
				continue
			}
			if domain.BindingUsesReality(p, b.Params) {
				return true
			}
		}
	}
	return false
}

func presetHasTrait(p domain.ProtocolPreset, trait string) bool {
	for _, t := range p.Traits {
		if t == trait {
			return true
		}
	}
	return false
}

func poolContainsEndpoint(pool []domain.RealityEndpoint, ep domain.RealityEndpoint) bool {
	for _, p := range pool {
		if p.SNI == ep.SNI && p.HandshakeServer == ep.HandshakeServer && p.HandshakePort == ep.HandshakePort {
			return true
		}
	}
	return false
}

func realityPreferSNI(params map[string]string) string {
	if params == nil {
		return ""
	}
	if v := strings.ToLower(strings.TrimSpace(params[domain.BindingParamRealitySNI])); v != "" {
		return v
	}
	return strings.ToLower(strings.TrimSpace(params["demux_sni"]))
}

func (s *Service) ensureRealityAssignments(sets []domain.InboundSet, profiles []domain.RealityEndpoint) (map[string]domain.RealityAssignment, bool, error) {
	assignments, err := s.store.LoadRealityAssignments()
	if err != nil {
		return nil, false, err
	}
	if assignments == nil {
		assignments = map[string]domain.RealityAssignment{}
	}
	if len(profiles) == 0 {
		return assignments, false, nil
	}
	now := time.Now().UTC()
	changed := false
	needed := map[string]struct{}{}
	type needItem struct {
		key       string
		preferSNI string
	}
	var items []needItem
	for _, set := range sets {
		for _, b := range set.EffectiveBindings() {
			p, err := presets.Get(b.Preset)
			if err != nil || !domain.BindingUsesReality(p, b.Params) {
				continue
			}
			key := realityInboundKey(set.Name, p.Name)
			needed[key] = struct{}{}
			items = append(items, needItem{key: key, preferSNI: realityPreferSNI(b.Params)})
		}
	}
	usedSNI := map[string]string{}
	for _, it := range items {
		a, ok := assignments[it.key]
		if !ok || a.SNI == "" {
			continue
		}
		if strings.TrimSpace(a.HandshakeServer) == "" || a.HandshakePort == 0 {
			if strings.TrimSpace(a.HandshakeServer) == "" {
				a.HandshakeServer = a.SNI
			}
			if a.HandshakePort == 0 {
				a.HandshakePort = 443
			}
			a.UpdatedAt = now
			assignments[it.key] = a
			changed = true
		}
		valid := poolContainsEndpoint(profiles, domain.RealityEndpoint{
			SNI:             a.SNI,
			HandshakeServer: a.HandshakeServer,
			HandshakePort:   a.HandshakePort,
		}) && a.PrivateKeyBase64 != "" && a.PublicKeyBase64 != "" && a.ShortID != ""
		if !valid {
			continue
		}
		sni := strings.ToLower(a.SNI)
		if owner, taken := usedSNI[sni]; taken && owner != it.key {
			continue
		}
		usedSNI[sni] = it.key
	}
	for _, it := range items {
		key := it.key
		preferSNI := it.preferSNI
		a, ok := assignments[key]
		valid := ok && poolContainsEndpoint(profiles, domain.RealityEndpoint{
			SNI:             a.SNI,
			HandshakeServer: a.HandshakeServer,
			HandshakePort:   a.HandshakePort,
		}) && a.PrivateKeyBase64 != "" && a.PublicKeyBase64 != "" && a.ShortID != ""
		if valid {
			cur := strings.ToLower(a.SNI)
			if preferSNI != "" && cur != preferSNI {
				if ep, found := findRealityEndpointBySNI(profiles, preferSNI); found {
					if owner, taken := usedSNI[preferSNI]; !taken || owner == key {
						a.SNI = ep.SNI
						a.HandshakeServer = ep.HandshakeServer
						a.HandshakePort = ep.HandshakePort
						a.UpdatedAt = now
						assignments[key] = a
						delete(usedSNI, cur)
						usedSNI[preferSNI] = key
						changed = true
						continue
					}
				}
			}
			if owner, taken := usedSNI[cur]; !taken || owner == key {
				usedSNI[cur] = key
				continue
			}
		}
		ep, err := pickRealityEndpoint(profiles, preferSNI, usedSNI, key)
		if err != nil {
			return nil, false, err
		}
		ep, err = normalizeRealityEndpoint(ep)
		if err != nil {
			return nil, false, err
		}
		priv, pub, err := generateRealityKeyPair()
		if err != nil {
			return nil, false, err
		}
		shortID, err := randomRealityShortID()
		if err != nil {
			return nil, false, err
		}
		if old, ok := assignments[key]; ok && old.SNI != "" {
			delete(usedSNI, strings.ToLower(old.SNI))
		}
		assignments[key] = domain.RealityAssignment{
			InboundKey:       key,
			SNI:              ep.SNI,
			HandshakeServer:  ep.HandshakeServer,
			HandshakePort:    ep.HandshakePort,
			PrivateKeyBase64: priv,
			PublicKeyBase64:  pub,
			ShortID:          shortID,
			UpdatedAt:        now,
		}
		usedSNI[strings.ToLower(ep.SNI)] = key
		changed = true
	}
	for key := range assignments {
		if _, ok := needed[key]; ok {
			continue
		}
		delete(assignments, key)
		changed = true
	}
	if changed {
		if err := s.store.SaveRealityAssignments(assignments); err != nil {
			return nil, false, err
		}
	}
	return assignments, changed, nil
}

func findRealityEndpointBySNI(pool []domain.RealityEndpoint, sni string) (domain.RealityEndpoint, bool) {
	want := strings.ToLower(strings.TrimSpace(sni))
	for _, ep := range pool {
		if strings.ToLower(ep.SNI) == want {
			return ep, true
		}
	}
	return domain.RealityEndpoint{}, false
}

func pickRealityEndpoint(pool []domain.RealityEndpoint, preferSNI string, usedSNI map[string]string, selfKey string) (domain.RealityEndpoint, error) {
	if len(pool) == 0 {
		return domain.RealityEndpoint{}, fmt.Errorf("no reality profiles available")
	}
	prefer := strings.ToLower(strings.TrimSpace(preferSNI))
	if prefer != "" {
		if ep, ok := findRealityEndpointBySNI(pool, prefer); ok {
			if owner, taken := usedSNI[prefer]; !taken || owner == selfKey {
				return ep, nil
			}
		}
	}
	unused := make([]domain.RealityEndpoint, 0, len(pool))
	for _, ep := range pool {
		sni := strings.ToLower(ep.SNI)
		if owner, taken := usedSNI[sni]; taken && owner != selfKey {
			continue
		}
		unused = append(unused, ep)
	}
	if len(unused) == 0 {
		return randomRealityEndpoint(pool)
	}
	return randomRealityEndpoint(unused)
}

func randomRealityEndpoint(pool []domain.RealityEndpoint) (domain.RealityEndpoint, error) {
	if len(pool) == 0 {
		return domain.RealityEndpoint{}, fmt.Errorf("no reality profiles available")
	}
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return domain.RealityEndpoint{}, err
	}
	idx := int(b[0])<<8 | int(b[1])
	idx %= len(pool)
	return pool[idx], nil
}

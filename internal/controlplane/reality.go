//go:build with_controlplane

package controlplane

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

const realityValidationInterval = 5 * time.Minute

var likelyCDNSuffixes = []string{
	".cloudflare.com",
	".cloudfront.net",
	".fastly.net",
	".akamaiedge.net",
}

func defaultRealityProfiles() []domain.RealityEndpoint {
	return []domain.RealityEndpoint{
		{SNI: "www.microsoft.com"},
		{SNI: "www.apple.com"},
		{SNI: "www.cloudflare.com"},
	}
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

func isLikelyCDNHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	for _, s := range likelyCDNSuffixes {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	return false
}

func (s *Service) validateRealityEndpoint(ctx context.Context, ep domain.RealityEndpoint) bool {
	// Conservative filter: skip hosts that look like direct CDN edge entries.
	if isLikelyCDNHost(ep.SNI) || isLikelyCDNHost(ep.HandshakeServer) {
		return false
	}
	resolver := net.DefaultResolver
	if _, err := resolver.LookupIPAddr(ctx, ep.SNI); err != nil {
		return false
	}
	if _, err := resolver.LookupIPAddr(ctx, ep.HandshakeServer); err != nil {
		return false
	}
	address := net.JoinHostPort(ep.HandshakeServer, strconv.Itoa(int(ep.HandshakePort)))
	dialer := net.Dialer{Timeout: 2 * time.Second}
	c, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
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

func (s *Service) loadRealityConfig() (domain.RealityConfig, error) {
	cfg, ok, err := s.store.LoadRealityConfig()
	if err != nil {
		return domain.RealityConfig{}, err
	}
	if ok {
		return cfg, nil
	}
	now := time.Now().UTC()
	cfg = domain.RealityConfig{
		EffectiveProfiles: defaultRealityProfiles(),
		UpdatedAt:         &now,
	}
	if err := s.store.SaveRealityConfig(cfg); err != nil {
		return domain.RealityConfig{}, err
	}
	return cfg, nil
}

func (s *Service) validateRealityPool(ctx context.Context, profiles []domain.RealityEndpoint) []domain.RealityEndpoint {
	out := make([]domain.RealityEndpoint, 0, len(profiles))
	seen := map[string]struct{}{}
	for _, raw := range profiles {
		ep, err := normalizeRealityEndpoint(raw)
		if err != nil {
			continue
		}
		key := ep.SNI + "|" + ep.HandshakeServer + "|" + strconv.Itoa(int(ep.HandshakePort))
		if _, ok := seen[key]; ok {
			continue
		}
		if s.validateRealityEndpoint(ctx, ep) {
			seen[key] = struct{}{}
			out = append(out, ep)
		}
	}
	return out
}

func hasRealityPreset(sets []domain.InboundSet) bool {
	for _, set := range sets {
		for _, pn := range set.Presets {
			p, err := presets.Get(pn)
			if err != nil {
				continue
			}
			for _, t := range p.Traits {
				if t == "reality" {
					return true
				}
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

func (s *Service) refreshRealityConfig(ctx context.Context, force bool) (domain.RealityConfig, bool, error) {
	cfg, err := s.loadRealityConfig()
	if err != nil {
		return domain.RealityConfig{}, false, err
	}
	if !force && cfg.UpdatedAt != nil && time.Since(*cfg.UpdatedAt) < realityValidationInterval {
		return cfg, false, nil
	}
	defaults := s.validateRealityPool(ctx, defaultRealityProfiles())
	if len(defaults) == 0 {
		defaults = normalizedRealityDefaults()
	}
	effective := defaults
	usingUser := false
	if len(cfg.UserProfiles) > 0 {
		validUser := s.validateRealityPool(ctx, cfg.UserProfiles)
		if len(validUser) > 0 {
			effective = validUser
			usingUser = true
		} else {
			// Silent failover: invalid user profile list is dropped.
			cfg.UserProfiles = nil
		}
	}
	now := time.Now().UTC()
	changed := usingUser != cfg.UsingUserOverrides || !sameRealityPool(cfg.EffectiveProfiles, effective) || len(cfg.UserProfiles) == 0 && cfg.UsingUserOverrides
	cfg.EffectiveProfiles = effective
	cfg.UsingUserOverrides = usingUser
	cfg.UpdatedAt = &now
	if changed {
		if err := s.store.SaveRealityConfig(cfg); err != nil {
			return domain.RealityConfig{}, false, err
		}
	}
	return cfg, changed, nil
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

func sameRealityPool(a, b []domain.RealityEndpoint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func poolContainsEndpoint(pool []domain.RealityEndpoint, ep domain.RealityEndpoint) bool {
	for _, p := range pool {
		if p.SNI == ep.SNI && p.HandshakeServer == ep.HandshakeServer && p.HandshakePort == ep.HandshakePort {
			return true
		}
	}
	return false
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
	for _, set := range sets {
		for _, pn := range set.Presets {
			p, err := presets.Get(pn)
			if err != nil || !presetHasTrait(p, "reality") {
				continue
			}
			key := realityInboundKey(set.Name, pn)
			needed[key] = struct{}{}
			a, ok := assignments[key]
			if ok && poolContainsEndpoint(profiles, domain.RealityEndpoint{
				SNI:             a.SNI,
				HandshakeServer: a.HandshakeServer,
				HandshakePort:   a.HandshakePort,
			}) && a.PrivateKeyBase64 != "" && a.PublicKeyBase64 != "" && a.ShortID != "" {
				continue
			}
			ep, err := randomRealityEndpoint(profiles)
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
			changed = true
		}
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

func randomRealityEndpoint(pool []domain.RealityEndpoint) (domain.RealityEndpoint, error) {
	if len(pool) == 0 {
		return domain.RealityEndpoint{}, fmt.Errorf("no validated reality profiles available")
	}
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return domain.RealityEndpoint{}, err
	}
	idx := int(b[0])<<8 | int(b[1])
	idx %= len(pool)
	return pool[idx], nil
}

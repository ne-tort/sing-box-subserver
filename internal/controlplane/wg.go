//go:build with_controlplane

package controlplane

import (
	"fmt"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/wgawg"
)

func wgCredKeys() []string {
	return []string{
		domain.WgProfilePlain, "wireguard", "plain",
		domain.WgProfileAWG2, "awg2", "amnezia-wg2",
		domain.WgProfileAWG3, "awg3", "amnezia-wg3",
		domain.WgProfilePathology, "pathology",
	}
}

func hostIndexFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), n == float64(int(n))
	case int:
		return n, true
	case int64:
		return int(n), true
	case string:
		var i int
		_, err := fmt.Sscanf(strings.TrimSpace(n), "%d", &i)
		return i, err == nil
	default:
		return 0, false
	}
}

func firstWgCreds(creds map[string]map[string]any) map[string]any {
	if creds == nil {
		return nil
	}
	for _, k := range wgCredKeys() {
		if c := creds[k]; c != nil {
			return c
		}
	}
	return presets.CredsFor(creds, "wg")
}

func allocWgHostIndex(used map[int]string) (int, error) {
	for i := domain.WgMinHostIndex; i <= domain.WgMaxHostIndex; i++ {
		if _, ok := used[i]; !ok {
			return i, nil
		}
	}
	return 0, fmt.Errorf("cp_wg_pool_exhausted: no free host index in %d-%d", domain.WgMinHostIndex, domain.WgMaxHostIndex)
}

// ensureWgHubSecrets fills hub keys + obfuscation bundle when needed.
func (s *Service) ensureWgHubSecrets(h *domain.WgHub, forceObf bool) (bool, error) {
	if h == nil {
		return false, nil
	}
	h.Normalize()
	changed := false
	if strings.TrimSpace(h.HubPrivateKey) == "" {
		priv, err := domain.RandomWireGuardPrivate()
		if err != nil {
			return false, err
		}
		h.HubPrivateKey = priv
		h.HubPublicKey = ""
		changed = true
	} else {
		norm, err := domain.NormalizeWireGuardKey(h.HubPrivateKey)
		if err != nil {
			return false, fmt.Errorf("hub_private_key: %w", err)
		}
		if norm != h.HubPrivateKey {
			h.HubPrivateKey = norm
			h.HubPublicKey = "" // re-derive in StdEncoding
			changed = true
		}
	}
	if strings.TrimSpace(h.HubPublicKey) == "" {
		pub, err := domain.WireGuardPublicFromPrivate(h.HubPrivateKey)
		if err != nil {
			return false, err
		}
		h.HubPublicKey = pub
		changed = true
	} else {
		norm, err := domain.NormalizeWireGuardKey(h.HubPublicKey)
		if err != nil {
			return false, fmt.Errorf("hub_public_key: %w", err)
		}
		if norm != h.HubPublicKey {
			h.HubPublicKey = norm
			changed = true
		}
	}
	needObf := domain.NeedsObfuscation(h.Profile)
	if !needObf {
		if h.AWG2 != nil || h.AWG3 != nil || h.Pathology != nil {
			h.ClearObfuscation()
			changed = true
		}
		return changed, nil
	}
	prev := h.ActiveObfuscation()
	missing := !h.HasObfuscation()
	if !(forceObf || missing) {
		return changed, nil
	}
	var bundle map[string]any
	var err error
	switch h.Profile {
	case domain.WgProfilePathology:
		// PUT never rotates Pathology key; regenerate-obfuscation preserves it too.
		// Key is mirrored into user WG creds as pathology_key on ensureWgUserCreds.
		if missing {
			bundle, err = wgawg.BundlePathology()
		}
	case domain.WgProfileAWG2, domain.WgProfileAWG3:
		awg3 := h.Profile == domain.WgProfileAWG3
		if forceObf && len(prev) > 0 {
			bundle, err = wgawg.BundleFromExisting(awg3, prev, "")
		} else {
			bundle, err = wgawg.Bundle(awg3)
		}
	}
	if err != nil {
		return false, err
	}
	if bundle != nil {
		h.SetActiveObfuscation(bundle)
		changed = true
	}
	return changed, nil
}

// ensureWgUserCreds assigns curve25519 + sticky wg_host_index under shared "wg" creds.
// Also refreshes derived `address` (host IP, no CIDR) from the current hub subnet.
// When hub Pathology is active, mirrors the hub PSK into sticky `pathology_key`
// (same semantics as private_key — assigned once, not rotated by regenerate).
func (s *Service) ensureWgUserCreds(users []domain.User) ([]domain.User, bool, error) {
	hub := domain.DefaultWgHub()
	if s.store != nil {
		if loaded, err := s.store.LoadWgHub(); err == nil {
			hub = loaded
		}
	}
	hub.Normalize()
	hubPathologyKey := ""
	if hub.Profile == domain.WgProfilePathology || wgawg.PathologyHasKey(hub.Pathology) {
		hubPathologyKey = wgawg.PathologyKey(hub.Pathology)
	}

	// Pass 1: reserve sticky indices so allocation cannot steal them.
	used := map[int]string{}
	for _, u := range users {
		creds := firstWgCreds(u.Creds)
		if creds == nil {
			continue
		}
		idx, ok := hostIndexFromAny(creds["wg_host_index"])
		if !ok {
			continue
		}
		if idx < domain.WgMinHostIndex || idx > domain.WgMaxHostIndex {
			return nil, false, fmt.Errorf("user %q: wg_host_index %d out of range", u.Name, idx)
		}
		if other, taken := used[idx]; taken {
			return nil, false, fmt.Errorf("cp_wg_peer_conflict: users %q and %q share wg_host_index %d", other, u.Name, idx)
		}
		used[idx] = u.Name
	}

	changed := false
	out := make([]domain.User, len(users))
	copy(out, users)
	for i := range out {
		u := &out[i]
		if u.Creds == nil {
			u.Creds = map[string]map[string]any{}
		}
		creds := firstWgCreds(u.Creds)
		if creds == nil {
			creds = map[string]any{}
		} else {
			// Clone so we don't mutate shared maps unexpectedly before mirror.
			cloned := make(map[string]any, len(creds)+2)
			for k, v := range creds {
				cloned[k] = v
			}
			creds = cloned
		}
		uc := false
		if credFieldEmpty(creds["private_key"]) {
			priv, err := domain.RandomWireGuardPrivate()
			if err != nil {
				return nil, false, err
			}
			creds["private_key"] = priv
			creds["public_key"] = ""
			uc = true
		} else if priv, ok := creds["private_key"].(string); ok {
			norm, err := domain.NormalizeWireGuardKey(priv)
			if err != nil {
				return nil, false, fmt.Errorf("user %q private_key: %w", u.Name, err)
			}
			if norm != priv {
				creds["private_key"] = norm
				creds["public_key"] = ""
				uc = true
			}
		}
		if credFieldEmpty(creds["public_key"]) {
			priv, _ := creds["private_key"].(string)
			pub, err := domain.WireGuardPublicFromPrivate(priv)
			if err != nil {
				return nil, false, err
			}
			creds["public_key"] = pub
			uc = true
		} else if pub, ok := creds["public_key"].(string); ok {
			norm, err := domain.NormalizeWireGuardKey(pub)
			if err != nil {
				// Stale/placeholder public_key — re-derive from private.
				priv, _ := creds["private_key"].(string)
				derived, derr := domain.WireGuardPublicFromPrivate(priv)
				if derr != nil {
					return nil, false, fmt.Errorf("user %q public_key: %w", u.Name, err)
				}
				creds["public_key"] = derived
				uc = true
			} else if norm != pub {
				creds["public_key"] = norm
				uc = true
			}
		}
		idx, hasIdx := hostIndexFromAny(creds["wg_host_index"])
		if !hasIdx {
			var allocErr error
			idx, allocErr = allocWgHostIndex(used)
			if allocErr != nil {
				return nil, false, allocErr
			}
			creds["wg_host_index"] = idx
			used[idx] = u.Name
			uc = true
		}
		if addr, err := hub.PeerInterfaceAddress(idx); err == nil {
			want := domain.HostIPOnly(addr)
			if domain.HostIPOnly(fmt.Sprint(creds["address"])) != want {
				creds["address"] = want
				uc = true
			}
		}
		if hubPathologyKey != "" && credFieldEmpty(creds["pathology_key"]) {
			creds["pathology_key"] = hubPathologyKey
			uc = true
		}
		mirrored := false
		for _, k := range wgCredKeys() {
			if u.Creds[k] == nil {
				mirrored = true
			}
			u.Creds[k] = creds
		}
		if uc || mirrored {
			changed = true
		}
	}
	return out, changed, nil
}

func presetIsEndpoint(p domain.ProtocolPreset) bool {
	for _, t := range p.Traits {
		if t == "endpoint" {
			return true
		}
	}
	return false
}

// validateWgListenPort rejects collision with active inbound set listen ports.
func (s *Service) validateWgListenPort(h domain.WgHub) error {
	if !h.Enabled || h.ListenPort == 0 {
		return nil
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		return err
	}
	st, err := s.store.LoadState()
	if err != nil {
		return err
	}
	active := map[string]struct{}{}
	for _, n := range st.ActiveSets {
		active[n] = struct{}{}
	}
	for _, set := range sets {
		if _, ok := active[set.Name]; !ok {
			continue
		}
		if set.ListenPort == h.ListenPort {
			return fmt.Errorf("cp_port_conflict: wg listen_port %d in use by set %q", h.ListenPort, set.Name)
		}
	}
	return nil
}

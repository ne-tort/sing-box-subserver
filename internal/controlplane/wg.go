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

// ensureWgHubSecrets fills hub keys + AWG bundle when needed.
func (s *Service) ensureWgHubSecrets(h *domain.WgHub, forceAWG bool) (bool, error) {
	if h == nil {
		return false, nil
	}
	h.Normalize()
	changed := false
	if strings.TrimSpace(h.HubPrivateKey) == "" {
		priv, err := randomCurve25519Private()
		if err != nil {
			return false, err
		}
		h.HubPrivateKey = priv
		changed = true
	}
	if strings.TrimSpace(h.HubPublicKey) == "" {
		pub, err := curve25519PublicFromPrivate(h.HubPrivateKey)
		if err != nil {
			return false, err
		}
		h.HubPublicKey = pub
		changed = true
	}
	needAWG := h.Profile == domain.WgProfileAWG2 || h.Profile == domain.WgProfileAWG3
	if needAWG && (forceAWG || len(h.AWG) == 0) {
		bundle, err := wgawg.Bundle(h.Profile == domain.WgProfileAWG3)
		if err != nil {
			return false, err
		}
		h.AWG = bundle
		changed = true
	}
	if !needAWG && len(h.AWG) > 0 {
		h.AWG = nil
		changed = true
	}
	return changed, nil
}

// ensureWgUserCreds assigns curve25519 + sticky wg_host_index under shared "wg" creds.
func (s *Service) ensureWgUserCreds(users []domain.User) ([]domain.User, bool, error) {
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
			priv, err := randomCurve25519Private()
			if err != nil {
				return nil, false, err
			}
			creds["private_key"] = priv
			uc = true
		}
		pubChanged, err := ensureCurve25519Public(creds, true)
		if err != nil {
			return nil, false, err
		}
		if pubChanged {
			uc = true
		}
		if _, ok := hostIndexFromAny(creds["wg_host_index"]); !ok {
			idx, err := allocWgHostIndex(used)
			if err != nil {
				return nil, false, err
			}
			creds["wg_host_index"] = idx
			used[idx] = u.Name
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

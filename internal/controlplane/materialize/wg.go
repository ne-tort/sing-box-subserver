//go:build with_controlplane

package materialize

import (
	"fmt"
	"math"
	"strconv"
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

// wgCredsFromUser resolves WG peer secrets from any mirrored profile key.
func wgCredsFromUser(u domain.User, hubProfile string) map[string]any {
	if u.Creds == nil {
		return nil
	}
	for _, k := range wgCredKeys() {
		if c := u.Creds[k]; c != nil {
			return c
		}
	}
	if c := presets.CredsFor(u.Creds, "wg"); c != nil {
		return c
	}
	return presets.CredsFor(u.Creds, hubProfile)
}

// BytesPerSecToMbps converts CP speed_* to integer Mbps for WG peer ceilings.
// 0 → 0 (omit); otherwise max(1, ceil(bytes*8/1e6)).
func BytesPerSecToMbps(bytesPerSec int64) int {
	if bytesPerSec <= 0 {
		return 0
	}
	mbps := math.Ceil(float64(bytesPerSec) * 8 / 1_000_000)
	if mbps < 1 {
		return 1
	}
	return int(mbps)
}

func hostIndexFromCred(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		i := int(n)
		if i < domain.WgMinHostIndex || i > domain.WgMaxHostIndex {
			return 0, false
		}
		return i, true
	case int:
		if n < domain.WgMinHostIndex || n > domain.WgMaxHostIndex {
			return 0, false
		}
		return n, true
	case int64:
		i := int(n)
		if i < domain.WgMinHostIndex || i > domain.WgMaxHostIndex {
			return 0, false
		}
		return i, true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil || i < domain.WgMinHostIndex || i > domain.WgMaxHostIndex {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

// BuildWireGuardEndpoint builds the singleton hub endpoint, or nil if disabled.
func BuildWireGuardEndpoint(hub domain.WgHub, users []domain.User, publicHost string) (map[string]any, error) {
	hub.Normalize()
	if !hub.Enabled {
		return nil, nil
	}
	if err := hub.Validate(); err != nil {
		return nil, err
	}
	if hub.Profile != domain.WgProfilePlain && len(hub.AWG) == 0 {
		return nil, fmt.Errorf("cp_invalid_wg: profile %s requires generated AWG/masquerade params", hub.Profile)
	}
	if strings.TrimSpace(hub.HubPrivateKey) == "" {
		return nil, fmt.Errorf("cp_invalid_wg: hub_private_key required")
	}
	p, err := presets.Get(hub.Profile)
	if err != nil {
		return nil, fmt.Errorf("wg profile: %w", err)
	}
	ep, err := cloneMap(p.EndpointTemplate)
	if err != nil {
		return nil, err
	}
	if ep == nil {
		ep = map[string]any{}
	}
	ep["type"] = "wireguard"
	ep["tag"] = "cp-wg"
	hubAddr, err := hub.HubAddress()
	if err != nil {
		return nil, err
	}
	ep["address"] = []any{hubAddr}
	ep["private_key"] = hub.HubPrivateKey
	ep["listen_port"] = hub.ListenPort
	if hub.MTU > 0 {
		ep["mtu"] = hub.MTU
	} else if hub.Profile != domain.WgProfilePlain {
		ep["mtu"] = 1280
	}
	if hub.System {
		ep["system"] = true
		name := strings.TrimSpace(hub.Name)
		if name == "" {
			name = "wg-cp0"
		}
		ep["name"] = name
	} else {
		delete(ep, "system")
		delete(ep, "name")
	}
	if hub.UpMbps > 0 {
		ep["up_mbps"] = hub.UpMbps
	}
	if hub.DownMbps > 0 {
		ep["down_mbps"] = hub.DownMbps
	}

	if hub.Profile != domain.WgProfilePlain {
		wgawg.ApplyToEndpoint(ep, hub.AWG, hub.Profile)
	} else {
		for _, k := range []string{
			"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4",
			"id", "ip", "ib", "i1", "i2", "i3", "i4", "i5",
			"header_protection_key", "content_padding_addition",
			"rekey_after_time", "rekey_timeout", "reject_after_time",
			"keepalive_timeout", "max_handshake_attempts",
		} {
			delete(ep, k)
		}
	}

	peers := make([]any, 0, len(users))
	seenIndex := map[int]string{}
	seenPub := map[string]string{}
	for _, u := range users {
		creds := wgCredsFromUser(u, hub.Profile)
		if creds == nil {
			continue
		}
		pub, _ := creds["public_key"].(string)
		pub = strings.TrimSpace(pub)
		if pub == "" {
			continue
		}
		if other, ok := seenPub[pub]; ok {
			return nil, fmt.Errorf("cp_wg_peer_conflict: users %q and %q share public_key", other, u.Name)
		}
		idx, ok := hostIndexFromCred(creds["wg_host_index"])
		if !ok {
			return nil, fmt.Errorf("user %q missing or invalid wg_host_index", u.Name)
		}
		if other, ok := seenIndex[idx]; ok {
			return nil, fmt.Errorf("cp_wg_peer_conflict: users %q and %q share wg_host_index %d", other, u.Name, idx)
		}
		seenIndex[idx] = u.Name
		seenPub[pub] = u.Name
		allowed, err := hub.PeerAllowedIP(idx)
		if err != nil {
			return nil, fmt.Errorf("user %q: %w", u.Name, err)
		}
		peer := map[string]any{
			"public_key":                    pub,
			"allowed_ips":                   []any{allowed},
			"persistent_keepalive_interval": 0,
		}
		if up := BytesPerSecToMbps(u.SpeedUpBytesPerSec); up > 0 {
			peer["up_mbps"] = up
		}
		if down := BytesPerSecToMbps(u.SpeedDownBytesPerSec); down > 0 {
			peer["down_mbps"] = down
		}
		peers = append(peers, peer)
	}
	ep["peers"] = peers
	_ = publicHost
	return ep, nil
}

// RenderWireGuardClientEndpoint builds a client-side wireguard endpoint for subscription.
func RenderWireGuardClientEndpoint(user domain.User, hub domain.WgHub, publicHost string) (map[string]any, error) {
	hub.Normalize()
	if !hub.Enabled {
		return nil, nil
	}
	if hub.Profile != domain.WgProfilePlain && len(hub.AWG) == 0 {
		return nil, fmt.Errorf("cp_invalid_wg: profile %s requires AWG params for subscription", hub.Profile)
	}
	creds := wgCredsFromUser(user, hub.Profile)
	if creds == nil {
		return nil, nil
	}
	priv, _ := creds["private_key"].(string)
	if strings.TrimSpace(priv) == "" {
		return nil, nil
	}
	idx, ok := hostIndexFromCred(creds["wg_host_index"])
	if !ok {
		return nil, fmt.Errorf("missing or invalid wg_host_index")
	}
	localIP, err := hub.PeerAllowedIP(idx)
	if err != nil {
		return nil, err
	}
	localAddr := strings.TrimSuffix(localIP, "/32") + "/32"

	server := publicHost
	if server == "" {
		server = "127.0.0.1"
	}

	var peerAllowed []any
	if hub.InternetAllowed() {
		peerAllowed = []any{"0.0.0.0/0", "::/0"}
	} else {
		// Subnet route: reach hub + other peers; not a hub AllowedIPs expansion.
		peerAllowed = []any{hub.Subnet}
	}

	mtu := 1408
	if hub.MTU > 0 {
		mtu = hub.MTU
	} else if hub.Profile != domain.WgProfilePlain {
		mtu = 1280
	}

	ep := map[string]any{
		"type":        "wireguard",
		"tag":         "cp-wg",
		"mtu":         mtu,
		"address":     []any{localAddr},
		"private_key": priv,
		"peers": []any{
			map[string]any{
				"address":                       server,
				"port":                          hub.ListenPort,
				"public_key":                    hub.HubPublicKey,
				"allowed_ips":                   peerAllowed,
				"persistent_keepalive_interval": 25,
			},
		},
	}
	if hub.Profile != domain.WgProfilePlain {
		wgawg.ApplyToEndpoint(ep, hub.AWG, hub.Profile)
	}
	return ep, nil
}

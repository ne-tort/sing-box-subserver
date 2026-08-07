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
		domain.WgProfilePathology, "pathology",
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

// BuildWireGuardEndpoint builds the singleton hub endpoint (sugar JSON), or nil if disabled.
func BuildWireGuardEndpoint(hub domain.WgHub, users []domain.User, publicHost string) (map[string]any, error) {
	hub.Normalize()
	if !hub.Enabled {
		return nil, nil
	}
	if err := hub.Validate(); err != nil {
		return nil, err
	}
	if domain.NeedsObfuscation(hub.Profile) && !hub.HasObfuscation() {
		return nil, fmt.Errorf("cp_invalid_wg: profile %s requires generated AWG/pathology params", hub.Profile)
	}
	if strings.TrimSpace(hub.HubPrivateKey) == "" {
		return nil, fmt.Errorf("cp_invalid_wg: hub_private_key required")
	}
	hubPriv, err := domain.NormalizeWireGuardKey(hub.HubPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("cp_invalid_wg: hub_private_key: %w", err)
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
	if !strings.Contains(hubAddr, "/") {
		hubAddr = hubAddr + "/32"
	}
	ep["subnet"] = hub.Subnet
	ep["address"] = []any{hubAddr}
	ep["private_key"] = hubPriv
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
	if hub.PeerRelay {
		ep["peer_relay"] = true
	} else {
		delete(ep, "peer_relay")
	}

	if domain.NeedsObfuscation(hub.Profile) {
		wgawg.ApplyToEndpoint(ep, hub.ActiveObfuscation(), hub.Profile)
	} else {
		wgawg.ClearEndpointObfuscation(ep)
	}

	exitID := strings.TrimSpace(hub.ExitUserID)
	peers := make([]any, 0, len(users))
	seenIndex := map[int]string{}
	seenPub := map[string]string{}
	exitFound := exitID == ""
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
		normPub, err := domain.NormalizeWireGuardKey(pub)
		if err != nil {
			return nil, fmt.Errorf("user %q public_key: %w", u.Name, err)
		}
		pub = normPub
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
		hostIP, err := hub.PeerHostIP(idx)
		if err != nil {
			return nil, fmt.Errorf("user %q: %w", u.Name, err)
		}
		peer := map[string]any{
			"public_key":                    pub,
			"ip":                            hostIP,
			"persistent_keepalive_interval": hub.PeerKeepalive,
		}
		if exitID != "" && u.ID == exitID {
			peer["exit_node"] = true
			exitFound = true
		}
		if up := BytesPerSecToMbps(u.SpeedUpBytesPerSec); up > 0 {
			peer["up_mbps"] = up
		}
		if down := BytesPerSecToMbps(u.SpeedDownBytesPerSec); down > 0 {
			peer["down_mbps"] = down
		}
		peers = append(peers, peer)
	}
	if exitID != "" && !exitFound {
		return nil, fmt.Errorf("cp_invalid_wg: exit_user_id %q has no WG peer", exitID)
	}
	ep["peers"] = peers
	_ = publicHost
	return ep, nil
}

// RenderWireGuardClientEndpoint builds a client-side sugar wireguard endpoint for subscription.
func RenderWireGuardClientEndpoint(user domain.User, hub domain.WgHub, publicHost string) (map[string]any, error) {
	hub.Normalize()
	if !hub.Enabled {
		return nil, nil
	}
	if domain.NeedsObfuscation(hub.Profile) && !hub.HasObfuscation() {
		return nil, fmt.Errorf("cp_invalid_wg: profile %s requires AWG/pathology params for subscription", hub.Profile)
	}
	creds := wgCredsFromUser(user, hub.Profile)
	if creds == nil {
		return nil, nil
	}
	priv, _ := creds["private_key"].(string)
	if strings.TrimSpace(priv) == "" {
		return nil, nil
	}
	priv, err := domain.NormalizeWireGuardKey(priv)
	if err != nil {
		return nil, fmt.Errorf("private_key: %w", err)
	}
	hubPub, err := domain.NormalizeWireGuardKey(hub.HubPublicKey)
	if err != nil {
		return nil, fmt.Errorf("hub_public_key: %w", err)
	}
	idx, ok := hostIndexFromCred(creds["wg_host_index"])
	if !ok {
		return nil, fmt.Errorf("missing or invalid wg_host_index")
	}
	localAddr, err := hub.PeerInterfaceAddress(idx)
	if err != nil {
		return nil, err
	}
	// Explicit /32 so Prefixable never depends on bare-host sugar in older clients.
	if !strings.Contains(localAddr, "/") {
		localAddr = localAddr + "/32"
	}

	server := publicHost
	if server == "" {
		server = "127.0.0.1"
	}

	mtu := 1408
	if hub.MTU > 0 {
		mtu = hub.MTU
	} else if hub.Profile != domain.WgProfilePlain {
		mtu = 1280
	}

	clientKA := hub.ClientKeepalive
	if clientKA <= 0 {
		clientKA = 25
	}
	ep := map[string]any{
		"type":        "wireguard",
		"tag":         "cp-wg",
		"mtu":         mtu,
		"subnet":      hub.Subnet,
		"address":     []any{localAddr},
		"private_key": priv,
		"peers": []any{
			map[string]any{
				"address":                       server,
				"port":                          hub.ListenPort,
				"public_key":                    hubPub,
				"persistent_keepalive_interval": clientKA,
			},
		},
	}
	exitID := strings.TrimSpace(hub.ExitUserID)
	isExitPeer := exitID != "" && user.ID == exitID
	if isExitPeer {
		ep["advertise_exit_node"] = true
	} else if hub.InternetAllowed() {
		ep["use_exit_node"] = true
	}
	if domain.NeedsObfuscation(hub.Profile) {
		wgawg.ApplyToEndpoint(ep, hub.ActiveObfuscation(), hub.Profile)
		// Pathology PSK lives in sticky user WG creds (like private_key); subscription
		// must take it from there when present so clients stay in sync with creds.
		if hub.Profile == domain.WgProfilePathology || hub.Profile == "pathology" {
			if pk, _ := creds["pathology_key"].(string); strings.TrimSpace(pk) != "" {
				if nested, ok := ep["pathology"].(map[string]any); ok && nested != nil {
					nested["key"] = strings.TrimSpace(pk)
				}
			}
		}
	} else {
		wgawg.ClearEndpointObfuscation(ep)
	}
	return ep, nil
}

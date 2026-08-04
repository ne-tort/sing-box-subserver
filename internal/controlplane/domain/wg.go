//go:build with_controlplane

package domain

import (
	"fmt"
	"net/netip"
	"strings"
)

// WireGuard profile tags (singleton hub).
const (
	WgProfilePlain = "wg"
	WgProfileAWG2  = "wg_awg2"
	WgProfileAWG3  = "wg_awg3"

	WgDefaultSubnet     = "10.8.0.0/24"
	WgDefaultListenPort = uint16(41641)
	WgHubHostIndex      = 1
	WgMinHostIndex      = 2
	WgMaxHostIndex      = 254
)

// WgHub is the singleton WireGuard endpoint controlled by the agent CP.
type WgHub struct {
	Enabled    bool   `json:"enabled"`
	Profile    string `json:"profile"` // wg | wg_awg2 | wg_awg3
	Subnet     string `json:"subnet"`  // CIDR, default 10.8.0.0/24
	ListenPort uint16 `json:"listen_port"`
	System     bool   `json:"system,omitempty"`
	Name       string `json:"name,omitempty"` // iface name when system
	MTU        int    `json:"mtu,omitempty"`
	UpMbps     int    `json:"up_mbps,omitempty"`
	DownMbps   int    `json:"down_mbps,omitempty"`
	// PeerRelay enables L3 cryptokey relay between hub peers (sing-box-lx peer_relay).
	// false = hard peer isolation inside the WG endpoint.
	PeerRelay bool `json:"peer_relay,omitempty"`
	// InternetAllow controls client use_exit_node: nil/true → WAN via hub/exit; false → overlay only.
	InternetAllow *bool `json:"internet_allow,omitempty"`
	// ExitUserID selects the CP user whose hub peer is exit_node (sing-box-lx sugar).
	// Empty = hub itself is WAN egress when clients set use_exit_node.
	ExitUserID string `json:"exit_user_id,omitempty"`
	// PeerKeepalive is persistent_keepalive_interval on hub peers (seconds; 0 = off).
	PeerKeepalive int `json:"peer_keepalive,omitempty"`
	// ClientKeepalive is persistent_keepalive_interval on subscription clients (seconds; 0 = default 25).
	ClientKeepalive int `json:"client_keepalive,omitempty"`
	// HubPrivateKey / HubPublicKey are curve25519 (base64.RawURLEncoding).
	HubPrivateKey string `json:"hub_private_key,omitempty"`
	HubPublicKey  string `json:"hub_public_key,omitempty"`
	// AWG holds generated AmneziaWG + masquerade params (persisted).
	AWG map[string]any `json:"awg,omitempty"`

	// LegacyForwardAllow is accepted on load for migration only (maps to PeerRelay).
	LegacyForwardAllow bool `json:"forward_allow,omitempty"`
}

// DefaultWgHub returns a disabled hub with safe defaults.
func DefaultWgHub() WgHub {
	return WgHub{
		Enabled:    false,
		Profile:    WgProfilePlain,
		Subnet:     WgDefaultSubnet,
		ListenPort: WgDefaultListenPort,
	}
}

// Normalize fills defaults and canonicalizes profile/subnet.
func (h *WgHub) Normalize() {
	if h == nil {
		return
	}
	if h.Profile == "" {
		h.Profile = WgProfilePlain
	}
	switch h.Profile {
	case "awg2":
		h.Profile = WgProfileAWG2
	case "awg3":
		h.Profile = WgProfileAWG3
	case "plain", "wireguard":
		h.Profile = WgProfilePlain
	}
	if strings.TrimSpace(h.Subnet) == "" {
		h.Subnet = WgDefaultSubnet
	}
	if h.ListenPort == 0 {
		h.ListenPort = WgDefaultListenPort
	}
	// AmneziaWG profiles prefer a lower MTU; plain WG keeps template default unless set.
	if h.MTU <= 0 && (h.Profile == WgProfileAWG2 || h.Profile == WgProfileAWG3) {
		h.MTU = 1280
	}
	if h.ClientKeepalive <= 0 {
		h.ClientKeepalive = 25
	}
	h.ExitUserID = strings.TrimSpace(h.ExitUserID)
	// Migrate old OS-forward flag → peer_relay once.
	if h.LegacyForwardAllow && !h.PeerRelay {
		h.PeerRelay = true
	}
	h.LegacyForwardAllow = false
	// Sugar exit_node requires peer_relay on multi-peer hubs.
	if h.ExitUserID != "" {
		h.PeerRelay = true
	}
}

// Validate checks profile and subnet.
func (h WgHub) Validate() error {
	h.Normalize()
	switch h.Profile {
	case WgProfilePlain, WgProfileAWG2, WgProfileAWG3:
	default:
		return fmt.Errorf("cp_invalid_wg: unknown profile %q (want wg|wg_awg2|wg_awg3)", h.Profile)
	}
	prefix, err := netip.ParsePrefix(h.Subnet)
	if err != nil {
		return fmt.Errorf("cp_invalid_wg: subnet: %w", err)
	}
	if !prefix.Addr().Is4() {
		return fmt.Errorf("cp_invalid_wg: subnet must be IPv4")
	}
	if prefix.Bits() != 24 {
		return fmt.Errorf("cp_invalid_wg: subnet must be /24 (got /%d)", prefix.Bits())
	}
	return nil
}

// InternetAllowed returns whether clients get full-tunnel routes (default true).
func (h WgHub) InternetAllowed() bool {
	if h.InternetAllow == nil {
		return true
	}
	return *h.InternetAllow
}

// subnetPrefix parses the hub subnet after Normalize.
func (h WgHub) subnetPrefix() (netip.Prefix, error) {
	h.Normalize()
	return netip.ParsePrefix(h.Subnet)
}

// hostAddr builds 10.x.y.N from the hub subnet and host index.
func (h WgHub) hostAddr(hostIndex int) (netip.Addr, netip.Prefix, error) {
	prefix, err := h.subnetPrefix()
	if err != nil {
		return netip.Addr{}, netip.Prefix{}, err
	}
	base := prefix.Addr().As4()
	base[3] = byte(hostIndex)
	return netip.AddrFrom4(base), prefix, nil
}

// HubAddress returns the hub tunnel IP as a host address (no CIDR).
// sing-box-lx sugar normalizes host address → /32 in IPC.
func (h WgHub) HubAddress() (string, error) {
	addr, _, err := h.hostAddr(WgHubHostIndex)
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

// PeerHostIP builds 10.x.y.N (host IP, no CIDR) from host index.
func (h WgHub) PeerHostIP(hostIndex int) (string, error) {
	if hostIndex < WgMinHostIndex || hostIndex > WgMaxHostIndex {
		return "", fmt.Errorf("wg_host_index %d out of range %d-%d", hostIndex, WgMinHostIndex, WgMaxHostIndex)
	}
	addr, _, err := h.hostAddr(hostIndex)
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

// PeerInterfaceAddress is the client/creds tunnel address (host IP for sugar).
func (h WgHub) PeerInterfaceAddress(hostIndex int) (string, error) {
	return h.PeerHostIP(hostIndex)
}

// HostIPOnly strips a trailing /prefix from a stored address, if present.
func HostIPOnly(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Addr().String()
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return a.String()
	}
	return s
}

// IsWgProfile reports whether name is a WG hub profile tag or alias.
func IsWgProfile(name string) bool {
	switch strings.TrimSpace(name) {
	case WgProfilePlain, WgProfileAWG2, WgProfileAWG3, "awg2", "awg3", "wireguard", "plain":
		return true
	default:
		return false
	}
}

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
	WgDefaultListenPort = uint16(51820)
	WgHubHostIndex      = 1
	WgMinHostIndex      = 2
	WgMaxHostIndex      = 254
)

// WgHub is the singleton WireGuard endpoint controlled by the agent CP.
type WgHub struct {
	Enabled      bool   `json:"enabled"`
	Profile      string `json:"profile"` // wg | wg_awg2 | wg_awg3
	Subnet       string `json:"subnet"`  // CIDR, default 10.8.0.0/24
	ListenPort   uint16 `json:"listen_port"`
	System       bool   `json:"system,omitempty"`
	ForwardAllow bool   `json:"forward_allow,omitempty"`
	InternetAllow *bool `json:"internet_allow,omitempty"` // nil → true
	Name         string `json:"name,omitempty"`           // iface name when system
	MTU          int    `json:"mtu,omitempty"`
	UpMbps       int    `json:"up_mbps,omitempty"`
	DownMbps     int    `json:"down_mbps,omitempty"`
	// HubPrivateKey / HubPublicKey are curve25519 (base64.RawURLEncoding).
	HubPrivateKey string `json:"hub_private_key,omitempty"`
	HubPublicKey  string `json:"hub_public_key,omitempty"`
	// AWG holds generated AmneziaWG + masquerade params (persisted).
	AWG map[string]any `json:"awg,omitempty"`
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
}

// Validate checks profile, subnet, and forward/system coupling.
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
	if h.ForwardAllow && !h.System {
		return fmt.Errorf("cp_invalid_wg: forward_allow requires system=true")
	}
	if h.System && strings.TrimSpace(h.Name) == "" {
		// optional: default iface name applied at materialize
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

// HubAddress returns the hub interface address (…1/24).
func (h WgHub) HubAddress() (string, error) {
	h.Normalize()
	prefix, err := netip.ParsePrefix(h.Subnet)
	if err != nil {
		return "", err
	}
	base := prefix.Addr().As4()
	base[3] = byte(WgHubHostIndex)
	addr := netip.AddrFrom4(base)
	return netip.PrefixFrom(addr, 24).String(), nil
}

// PeerAllowedIP builds 10.x.y.N/32 from host index.
func (h WgHub) PeerAllowedIP(hostIndex int) (string, error) {
	if hostIndex < WgMinHostIndex || hostIndex > WgMaxHostIndex {
		return "", fmt.Errorf("wg_host_index %d out of range %d-%d", hostIndex, WgMinHostIndex, WgMaxHostIndex)
	}
	h.Normalize()
	prefix, err := netip.ParsePrefix(h.Subnet)
	if err != nil {
		return "", err
	}
	base := prefix.Addr().As4()
	base[3] = byte(hostIndex)
	return netip.AddrFrom4(base).String() + "/32", nil
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

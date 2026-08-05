//go:build with_controlplane

package domain

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/netip"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// WireGuard profile tags (singleton hub).
const (
	WgProfilePlain     = "wg"
	WgProfileAWG2      = "wg_awg2"
	WgProfileAWG3      = "wg_awg3"
	WgProfilePathology = "wg_pathology"

	WgDefaultSubnet = "10.8.0.0/24"
	// WgDefaultListenPort is only a last-resort fallback when randomization fails.
	// Prefer RandomWgListenPort(); never use the classic WireGuard 51820.
	WgDefaultListenPort = uint16(41641)
	WgHubHostIndex      = 1
	WgMinHostIndex      = 2
	WgMaxHostIndex      = 254
)

// RandomWgListenPort returns a high ephemeral UDP port for the WG hub (never 51820).
func RandomWgListenPort() uint16 {
	const lo, hi = 20000, 49999
	for attempt := 0; attempt < 32; attempt++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(hi-lo+1)))
		if err != nil {
			return WgDefaultListenPort
		}
		p := uint16(lo + int(n.Int64()))
		if p != 51820 {
			return p
		}
	}
	return WgDefaultListenPort
}

// WgHub is the singleton WireGuard endpoint controlled by the agent CP.
type WgHub struct {
	Enabled    bool   `json:"enabled"`
	Profile    string `json:"profile"` // wg | wg_awg2 | wg_awg3 | wg_pathology
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
	// HubPrivateKey / HubPublicKey are curve25519 in WireGuard form (base64.StdEncoding, padded).
	// Older agents may have stored RawURLEncoding; NormalizeWireGuardKey rewrites on load/apply.
	HubPrivateKey string `json:"hub_private_key,omitempty"`
	HubPublicKey  string `json:"hub_public_key,omitempty"`
	// Nested obfuscation blocks (mutex by profile). Legacy flat "awg" is ignored.
	AWG2      map[string]any `json:"awg2,omitempty"`
	AWG3      map[string]any `json:"awg3,omitempty"`
	Pathology map[string]any `json:"pathology,omitempty"`

	// LegacyForwardAllow is accepted on load for migration only (maps to PeerRelay).
	LegacyForwardAllow bool `json:"forward_allow,omitempty"`
}

// DefaultWgHub returns a disabled hub with safe defaults (random listen port).
func DefaultWgHub() WgHub {
	return WgHub{
		Enabled:    false,
		Profile:    WgProfilePlain,
		Subnet:     WgDefaultSubnet,
		ListenPort: RandomWgListenPort(),
	}
}

// NeedsObfuscation reports whether the profile requires a nested obfuscation block.
func NeedsObfuscation(profile string) bool {
	switch profile {
	case WgProfileAWG2, WgProfileAWG3, WgProfilePathology, "awg2", "awg3", "pathology":
		return true
	default:
		return false
	}
}

// HasObfuscation reports whether the hub has the nested block for its profile.
func (h WgHub) HasObfuscation() bool {
	h.Normalize()
	switch h.Profile {
	case WgProfileAWG2:
		return len(h.AWG2) > 0
	case WgProfileAWG3:
		return len(h.AWG3) > 0
	case WgProfilePathology:
		if len(h.Pathology) == 0 {
			return false
		}
		key := strings.TrimSpace(fmt.Sprint(h.Pathology["key"]))
		return key != "" && key != "<nil>"
	default:
		return false
	}
}

// ActiveObfuscation returns the nested block for the current profile.
func (h WgHub) ActiveObfuscation() map[string]any {
	h.Normalize()
	switch h.Profile {
	case WgProfileAWG2:
		return h.AWG2
	case WgProfileAWG3:
		return h.AWG3
	case WgProfilePathology:
		return h.Pathology
	default:
		return nil
	}
}

// SetActiveObfuscation stores block under the profile-matching nested field and clears others.
func (h *WgHub) SetActiveObfuscation(block map[string]any) {
	if h == nil {
		return
	}
	h.Normalize()
	h.AWG2, h.AWG3, h.Pathology = nil, nil, nil
	switch h.Profile {
	case WgProfileAWG2:
		h.AWG2 = block
	case WgProfileAWG3:
		h.AWG3 = block
	case WgProfilePathology:
		h.Pathology = block
	}
}

// ClearObfuscation drops all nested obfuscation blocks.
func (h *WgHub) ClearObfuscation() {
	if h == nil {
		return
	}
	h.AWG2, h.AWG3, h.Pathology = nil, nil, nil
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
	case "pathology":
		h.Profile = WgProfilePathology
	case "plain", "wireguard":
		h.Profile = WgProfilePlain
	}
	if strings.TrimSpace(h.Subnet) == "" {
		h.Subnet = WgDefaultSubnet
	}
	if h.ListenPort == 0 {
		h.ListenPort = RandomWgListenPort()
	}
	// Obfuscated profiles prefer a lower MTU; plain WG keeps template default unless set.
	if h.MTU <= 0 && NeedsObfuscation(h.Profile) {
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
	// Drop nested blocks that do not match the active profile.
	switch h.Profile {
	case WgProfilePlain:
		h.AWG2, h.AWG3, h.Pathology = nil, nil, nil
	case WgProfileAWG2:
		h.AWG3, h.Pathology = nil, nil
	case WgProfileAWG3:
		h.AWG2, h.Pathology = nil, nil
	case WgProfilePathology:
		h.AWG2, h.AWG3 = nil, nil
	}
}

// Validate checks profile and subnet.
func (h WgHub) Validate() error {
	h.Normalize()
	switch h.Profile {
	case WgProfilePlain, WgProfileAWG2, WgProfileAWG3, WgProfilePathology:
	default:
		return fmt.Errorf("cp_invalid_wg: unknown profile %q (want wg|wg_awg2|wg_awg3|wg_pathology)", h.Profile)
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
	case WgProfilePlain, WgProfileAWG2, WgProfileAWG3, WgProfilePathology,
		"awg2", "awg3", "pathology", "wireguard", "plain":
		return true
	default:
		return false
	}
}

// DecodeCurve25519Key32 accepts WireGuard StdEncoding and legacy RawURL/RawStd forms.
func DecodeCurve25519Key32(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return nil, fmt.Errorf("empty curve25519 key")
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.RawURLEncoding,
		base64.URLEncoding,
	} {
		raw, err := enc.DecodeString(b64)
		if err == nil && len(raw) == 32 {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("invalid curve25519 key encoding")
}

// EncodeWireGuardKey encodes 32 bytes as sing-box WireGuard expects (StdEncoding + padding).
func EncodeWireGuardKey(raw []byte) string {
	return base64.StdEncoding.EncodeToString(raw)
}

// NormalizeWireGuardKey rewrites any accepted curve25519 encoding to StdEncoding.
func NormalizeWireGuardKey(b64 string) (string, error) {
	raw, err := DecodeCurve25519Key32(b64)
	if err != nil {
		return "", err
	}
	return EncodeWireGuardKey(raw), nil
}

// RandomWireGuardPrivate returns a clamped X25519 private key in WireGuard StdEncoding.
func RandomWireGuardPrivate() (string, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return "", err
	}
	k[0] &= 248
	k[31] &= 127
	k[31] |= 64
	return EncodeWireGuardKey(k[:]), nil
}

// WireGuardPublicFromPrivate derives the public key (StdEncoding) from a private key.
func WireGuardPublicFromPrivate(privB64 string) (string, error) {
	raw, err := DecodeCurve25519Key32(privB64)
	if err != nil {
		return "", fmt.Errorf("wireguard private key: %w", err)
	}
	pub, err := curve25519.X25519(raw, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return EncodeWireGuardKey(pub), nil
}

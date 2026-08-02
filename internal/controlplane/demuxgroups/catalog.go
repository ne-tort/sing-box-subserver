//go:build with_controlplane

package demuxgroups

import (
	"fmt"
	"sync"
)

var (
	once   sync.Once
	all    []Group
	byTag  map[string]Group
)

func load() {
	all = builtInGroups()
	byTag = make(map[string]Group, len(all))
	for _, g := range all {
		byTag[g.Tag] = g
	}
}

// All returns demux groups in stable order.
func All() []Group {
	once.Do(load)
	out := make([]Group, len(all))
	copy(out, all)
	return out
}

// Get returns a group by tag.
func Get(tag string) (Group, error) {
	once.Do(load)
	g, ok := byTag[tag]
	if !ok {
		return Group{}, fmt.Errorf("unknown demux group %q", tag)
	}
	return g, nil
}

// Count returns catalog size.
func Count() int {
	once.Do(load)
	return len(all)
}

func builtInGroups() []Group {
	// Modern-only: no http/socks/vmess/trojan/hy1/ss.
	tcpRealitySubs := []string{
		"vless_reality", "vless_ws_reality", "vless_grpc_reality",
		"vless_http_reality", "vless_httpupgrade_reality",
	}
	tcpTLSSubs := []string{
		"vless_tls", "vless_ws_tls", "vless_grpc_tls", "vless_http_tls",
		"vless_httpupgrade_tls", "anytls", "trusttunnel_h2", "trusttunnel_auto",
	}
	// hy2_salamander intentionally omitted: salamander obfuscates QUIC first bytes,
	// so demux match protocol=quic / tls.sni cannot classify the flow.
	quicSubs := []string{
		"hy2", "tuic", "tuic_0rtt",
		"shadowquic_jls", "shadowquic_0rtt", "trusttunnel_h3",
	}
	tcpPlainSubs := []string{
		"sudoku_pad", "sudoku_httpmask", "sudoku_aes", "mieru_tcp", "ssh_password", "snell_v5", "snell_v6",
	}

	return []Group{
		{
			Tag: "dg_443_fullstack", ShortName: "443 Full stack", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 6, "setup": 5},
			I18n: map[string]I18n{
				"ru": {Title: "443 Full stack", Description: "Осознанный стек на :443: Reality + 2×TLS (.local/ALPN) + QUIC Hy2 + plain mieru."},
				"en": {Title: "443 Full stack", Description: "Full :443 stack: Reality + 2×TLS (.local/ALPN) + QUIC Hy2 + plain mieru."},
			},
			Slots: []Slot{
				{ID: "reality", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "tls_h2", Role: RoleTCPTLS, DefaultPreset: "vless_grpc_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "tls_h1", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"http/1.1"}},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
				{ID: "plain", Role: RoleTCPPlain, DefaultPreset: "mieru_tcp", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
			},
			Notes: "Per-slot demux_sni (.local / pool); Reality from SNI pool; plain first-bytes / always_plain.",
		},
		{
			Tag: "dg_443_dual", ShortName: "443 Dual", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 7, "setup": 8},
			I18n: map[string]I18n{
				"ru": {Title: "443 Dual (Reality + Hy2)", Description: "Классика: TCP Reality + QUIC Hysteria2 на одном :443."},
				"en": {Title: "443 Dual (Reality + Hy2)", Description: "TCP Reality + QUIC Hysteria2 on one :443."},
			},
			Slots: []Slot{
				{ID: "tcp", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_triple", ShortName: "443 Triple", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 7, "setup": 7},
			I18n: map[string]I18n{
				"ru": {Title: "443 Triple", Description: "Reality + классический TLS (другой SNI) + Hy2."},
				"en": {Title: "443 Triple", Description: "Reality + classic TLS (other SNI) + Hy2."},
			},
			Slots: []Slot{
				{ID: "reality", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "tls", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2", "http/1.1"}},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_sni_stack", ShortName: "443 SNI stack", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp"},
			Scores:   map[string]int{"dpi": 8, "speed": 7, "mobile": 7, "setup": 6},
			I18n: map[string]I18n{
				"ru": {Title: "443 SNI stack", Description: "До 5 TCP TLS/Reality слотов, разведённых по SNI."},
				"en": {Title: "443 SNI stack", Description: "Up to 5 TCP TLS/Reality slots split by SNI."},
			},
			Slots: []Slot{
				{ID: "r1", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "r2", Role: RoleTCPReality, DefaultPreset: "vless_grpc_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "t1", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "t2", Role: RoleTCPTLS, DefaultPreset: "vless_http_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "t3", Role: RoleTCPTLS, DefaultPreset: "vless_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
			},
		},
		{
			Tag: "dg_443_modern5", ShortName: "443 Modern ×5", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 6, "setup": 5},
			I18n: map[string]I18n{
				"ru": {Title: "443 Modern ×5", Description: "2×Reality + TLS + Hy2 + TUIC по уникальным SNI."},
				"en": {Title: "443 Modern ×5", Description: "2×Reality + TLS + Hy2 + TUIC via unique SNIs."},
			},
			Slots: []Slot{
				{ID: "reality", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "reality2", Role: RoleTCPReality, DefaultPreset: "vless_grpc_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "tls", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "hy2", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "sni_pool"},
				{ID: "tuic", Role: RoleQUIC, DefaultPreset: "tuic", Substitutes: []string{"tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
			},
			Notes: "vless_ws_reality is demux_compat=full (matrix); gRPC Reality / ShadowQUIC remain demux_lab (allow_lab).",
		},
		{
			Tag: "dg_443_dense8", ShortName: "443 Dense ×8", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 7, "mobile": 5, "setup": 3},
			I18n: map[string]I18n{
				"ru": {Title: "443 Dense ×8", Description: "2×Reality + 3×TLS + plain + Hy2 + TUIC — максимум релевантных замен на одном порту."},
				"en": {Title: "443 Dense ×8", Description: "2×Reality + 3×TLS + plain + Hy2 + TUIC on one port."},
			},
			Slots: []Slot{
				{ID: "r1", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "r2", Role: RoleTCPReality, DefaultPreset: "vless_grpc_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "t1", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2", "http/1.1"}},
				{ID: "t2", Role: RoleTCPTLS, DefaultPreset: "vless_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "t3", Role: RoleTCPTLS, DefaultPreset: "vless_ws_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "plain", Role: RoleTCPPlain, DefaultPreset: "sudoku_pad", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
				{ID: "hy2", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "sni_pool"},
				{ID: "tuic", Role: RoleQUIC, DefaultPreset: "tuic", Substitutes: []string{"tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
			},
			Notes: "Per-slot self-signed TLS under demux_sni; Reality uses unique SNIs from the pool.",
		},
		{
			Tag: "dg_443_ssh_hy2", ShortName: "SSH + Hy2", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 5, "speed": 7, "mobile": 4, "setup": 7},
			I18n: map[string]I18n{
				"ru": {Title: "SSH + Hy2", Description: "SSH banner (plain TCP) + Hysteria2."},
				"en": {Title: "SSH + Hy2", Description: "SSH banner (plain TCP) + Hysteria2."},
			},
			Slots: []Slot{
				{ID: "ssh", Role: RoleTCPPlain, DefaultPreset: "ssh_password", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_plain_tls", ShortName: "443 Plain+TLS", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 7, "speed": 8, "mobile": 6, "setup": 7},
			I18n: map[string]I18n{
				"ru": {Title: "443 Plain + TLS + QUIC", Description: "Sudoku/mieru (plain TCP) + Reality + Hy2."},
				"en": {Title: "443 Plain + TLS + QUIC", Description: "Sudoku/mieru + Reality + Hy2."},
			},
			Slots: []Slot{
				{ID: "plain", Role: RoleTCPPlain, DefaultPreset: "sudoku_pad", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
				{ID: "reality", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_anytls_hy2", ShortName: "AnyTLS + Hy2", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 8, "speed": 8, "mobile": 7, "setup": 8},
			I18n: map[string]I18n{
				"ru": {Title: "AnyTLS + Hy2", Description: "TCP AnyTLS + QUIC Hysteria2."},
				"en": {Title: "AnyTLS + Hy2", Description: "TCP AnyTLS + QUIC Hysteria2."},
			},
			Slots: []Slot{
				{ID: "tls", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_tt_hy2", ShortName: "VLESS-TLS + Hy2", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 8, "speed": 8, "mobile": 6, "setup": 7},
			I18n: map[string]I18n{
				"ru": {Title: "VLESS-TLS + Hy2", Description: "TCP VLESS TLS + Hysteria2 (TrustTunnel доступен как замена)."},
				"en": {Title: "VLESS-TLS + Hy2", Description: "TCP VLESS TLS + Hysteria2 (TrustTunnel as substitute)."},
			},
			Slots: []Slot{
				{ID: "tls", Role: RoleTCPTLS, DefaultPreset: "vless_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
			Notes: "TrustTunnel via demux dial is unstable in the matrix; kept as a substitute only.",
		},
		{
			Tag: "dg_8443_quic_pair", ShortName: "8443 QUIC pair", Status: "lab", SuggestedPort: 8443,
			Networks: []string{"udp"},
			Scores:   map[string]int{"dpi": 7, "speed": 9, "mobile": 5, "setup": 6},
			I18n: map[string]I18n{
				"ru": {Title: "8443 QUIC pair", Description: "Hy2 + TUIC на UDP :8443 (SNI где доступно)."},
				"en": {Title: "8443 QUIC pair", Description: "Hy2 + TUIC on UDP :8443."},
			},
			Slots: []Slot{
				{ID: "hy2", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: []string{"hy2", "tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
				{ID: "tuic", Role: RoleQUIC, DefaultPreset: "tuic", Substitutes: []string{"tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
			},
		},
		{
			Tag: "dg_443_stack6", ShortName: "443 Stack ×6", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 7, "mobile": 5, "setup": 4},
			I18n: map[string]I18n{
				"ru": {Title: "443 Stack ×6", Description: "2×Reality + 2×TLS + Hy2 + TUIC, всё по уникальным SNI."},
				"en": {Title: "443 Stack ×6", Description: "2×Reality + 2×TLS + Hy2 + TUIC via unique SNIs."},
			},
			Slots: []Slot{
				{ID: "r1", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "r2", Role: RoleTCPReality, DefaultPreset: "vless_grpc_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "t1", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "t2", Role: RoleTCPTLS, DefaultPreset: "vless_httpupgrade_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "hy2", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "sni_pool"},
				{ID: "tuic", Role: RoleQUIC, DefaultPreset: "tuic", Substitutes: []string{"tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
			},
			Notes: "Requires ≥2 valid Reality SNIs; TLS/QUIC use self-signed certs with demux_sni.",
		},
		{
			Tag: "dg_443_vless_family", ShortName: "VLESS family", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 8, "speed": 8, "mobile": 7, "setup": 6},
			I18n: map[string]I18n{
				"ru": {Title: "VLESS family", Description: "Несколько VLESS-транспортов Reality/TLS + Hy2 на одном порту."},
				"en": {Title: "VLESS family", Description: "Several VLESS transports (Reality/TLS) + Hy2 on one port."},
			},
			Slots: []Slot{
				{ID: "vr", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "vws", Role: RoleTCPReality, DefaultPreset: "vless_grpc_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "vtls", Role: RoleTCPTLS, DefaultPreset: "vless_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "vgrpc", Role: RoleTCPTLS, DefaultPreset: "vless_grpc_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_quic_pair_sni", ShortName: "443 QUIC pair SNI", Status: "lab", SuggestedPort: 443,
			Networks: []string{"udp"},
			Scores:   map[string]int{"dpi": 7, "speed": 9, "mobile": 5, "setup": 6},
			I18n: map[string]I18n{
				"ru": {Title: "443 QUIC pair (SNI)", Description: "Hy2 + TUIC только UDP, разведение по SNI."},
				"en": {Title: "443 QUIC pair (SNI)", Description: "Hy2 + TUIC UDP-only, split by SNI."},
			},
			Slots: []Slot{
				{ID: "hy2", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: []string{"hy2", "tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
				{ID: "tuic", Role: RoleQUIC, DefaultPreset: "tuic", Substitutes: []string{"tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
			},
		},
		{
			Tag: "dg_443_mieru_hy2", ShortName: "Mieru + Hy2", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 6, "speed": 8, "mobile": 5, "setup": 7},
			I18n: map[string]I18n{
				"ru": {Title: "Mieru + Hy2", Description: "Mieru TCP + Hysteria2 QUIC."},
				"en": {Title: "Mieru + Hy2", Description: "Mieru TCP + Hysteria2 QUIC."},
			},
			Slots: []Slot{
				{ID: "mieru", Role: RoleTCPPlain, DefaultPreset: "mieru_tcp", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_alpn_split", ShortName: "443 SNI+ALPN hint", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 8, "speed": 7, "mobile": 6, "setup": 5},
			I18n: map[string]I18n{
				"ru": {Title: "443 SNI + ALPN hint", Description: "Три слота по уникальным SNI; PreferredALPN задаёт inbound tls.alpn (demux НЕ матчит по ALPN)."},
				"en": {Title: "443 SNI + ALPN hint", Description: "Three slots via unique SNIs; PreferredALPN sets inbound tls.alpn (demux does NOT match on ALPN)."},
			},
			Slots: []Slot{
				{ID: "h2", Role: RoleTCPTLS, DefaultPreset: "vless_grpc_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "h1", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"http/1.1"}},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
			Notes: "Tag kept as dg_443_alpn_split for API stability. Demux match = tls.sni only.",
		},
		{
			Tag: "dg_443_reality_sq", ShortName: "Reality + SQ/Hy2", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 5, "setup": 5},
			I18n: map[string]I18n{
				"ru": {Title: "Reality + SQ/Hy2", Description: "TCP Reality + QUIC (default Hy2; ShadowQUIC — lab-замена)."},
				"en": {Title: "Reality + SQ/Hy2", Description: "TCP Reality + QUIC (Hy2 default; ShadowQUIC is lab substitute)."},
			},
			Slots: []Slot{
				{ID: "tcp", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: []string{"hy2", "shadowquic_jls", "shadowquic_0rtt", "tuic"}, MatchHint: "protocol_only"},
			},
			Notes: "ShadowQUIC via demux dial is unstable (demux_lab); default Hy2 passes the matrix. Salamander obfs is not used in demux.",
		},
		{
			Tag: "dg_443_snell_hy2", ShortName: "Snell + Hy2", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 5, "speed": 7, "mobile": 5, "setup": 7},
			I18n: map[string]I18n{
				"ru": {Title: "Snell + Hy2", Description: "Snell plain TCP + Hysteria2 QUIC на одном порту."},
				"en": {Title: "Snell + Hy2", Description: "Snell plain TCP + Hysteria2 QUIC on one port."},
			},
			Slots: []Slot{
				{ID: "snell", Role: RoleTCPPlain, DefaultPreset: "snell_v5", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_broad7", ShortName: "443 Broad ×7", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 7, "mobile": 5, "setup": 3},
			I18n: map[string]I18n{
				"ru": {Title: "443 Broad ×7", Description: "2×Reality + 2×TLS + plain + Hy2 + TUIC — широкий набор замен на одном :443."},
				"en": {Title: "443 Broad ×7", Description: "2×Reality + 2×TLS + plain + Hy2 + TUIC — wide substitute set on :443."},
			},
			Slots: []Slot{
				{ID: "r1", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "r2", Role: RoleTCPReality, DefaultPreset: "vless_grpc_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "t1", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2", "http/1.1"}},
				{ID: "t2", Role: RoleTCPTLS, DefaultPreset: "vless_grpc_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "plain", Role: RoleTCPPlain, DefaultPreset: "mieru_tcp", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
				{ID: "hy2", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "sni_pool"},
				{ID: "tuic", Role: RoleQUIC, DefaultPreset: "tuic", Substitutes: []string{"tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
			},
			Notes: "Unique SNIs on Reality/TLS/QUIC; plain uses always_plain first-match.",
		},
	}
}

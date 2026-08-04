//go:build with_controlplane

package demuxgroups

import (
	"fmt"
	"sync"
)

var (
	once    sync.Once
	all     []Group
	byTag   map[string]Group
	loadErr error
)

func load() {
	groups, err := loadFromCatalog()
	if err != nil {
		loadErr = err
		return
	}
	if len(groups) == 0 {
		// Runtime SoT is catalogsqlite only. BuiltinGroups is for dump-demux-groups seed.
		loadErr = fmt.Errorf("demux groups empty in catalogsqlite — dump ref/demux + regenerate catalog")
		return
	}
	all = groups
	byTag = make(map[string]Group, len(all))
	for _, g := range all {
		byTag[g.Tag] = g
	}
}

// BuiltinGroups is the Go seed source used to generate ref/demux/*.json.
// Runtime All()/Get require catalogsqlite (no silent fallback).
func BuiltinGroups() []Group {
	return builtInGroups()
}

func mustLoaded() {
	once.Do(load)
	if loadErr != nil {
		panic("demuxgroups: " + loadErr.Error())
	}
}

// All returns demux groups in stable order.
func All() []Group {
	mustLoaded()
	out := make([]Group, len(all))
	copy(out, all)
	return out
}

// Get returns a group by tag.
func Get(tag string) (Group, error) {
	mustLoaded()
	g, ok := byTag[tag]
	if !ok {
		return Group{}, fmt.Errorf("unknown demux group %q", tag)
	}
	return g, nil
}

// Count returns catalog size.
func Count() int {
	mustLoaded()
	return len(all)
}

func builtInGroups() []Group {
	// Modern-only: no http/socks/vmess/trojan/hy1/ss.
	tcpRealitySubs := []string{
		"vless_reality", "vless_ws_reality", "vless_grpc_reality",
		"vless_http_reality", "vless_httpupgrade_reality",
	}
	// Naive is a first-class TLS option (H2 via SNI). naive_quic (H2+H3) may claim the QUIC slot at build time.
	tcpTLSSubs := []string{
		"naive_tls", "naive_quic", "anytls",
		"vless_tls", "vless_ws_tls", "vless_grpc_tls", "vless_http_tls",
		"vless_httpupgrade_tls", "trusttunnel_h2", "trusttunnel_auto",
	}
	// hy2_salamander intentionally omitted: salamander obfuscates QUIC first bytes,
	// so demux match protocol=quic / tls.sni cannot classify the flow.
	quicSubs := []string{
		"hy2", "tuic", "tuic_0rtt",
		"shadowquic_jls", "shadowquic_0rtt", "shadowquic_cubic", "trusttunnel_h3",
	}
	tcpPlainSubs := []string{
		"sudoku_pad", "sudoku_httpmask", "sudoku_aes", "sudoku_aes256", "sudoku_mux",
		"mieru_tcp", "ssh_password", "snell_v5", "snell_v5_tls", "snell_v6",
	}

	return []Group{
		// --- stable ---
		{
			Tag: "dg_443_dual", ShortName: "Bypasser", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 7, "setup": 8},
			I18n: brandI18n("Bypasser", map[string]string{
				"en":    "Simple Reality with Hy2 fallback.",
				"ru":    "Простой Reality с запасным Hy2.",
				"ar":    "Reality بسيط مع Hy2 احتياطي.",
				"es":    "Reality simple con Hy2 de respaldo.",
				"fa":    "Reality ساده با پشتیبان Hy2.",
				"fr":    "Reality simple avec secours Hy2.",
				"id":    "Reality sederhana dengan cadangan Hy2.",
				"pt-BR": "Reality simples com Hy2 de reserva.",
				"tr":    "Hy2 yedekli basit Reality.",
				"zh-CN": "简洁 Reality，Hy2 作为备用。",
				"zh-TW": "簡潔 Reality，Hy2 作為備用。",
			}),
			Slots: []Slot{
				{ID: "tcp", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_triple", ShortName: "DPI Triple", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 7, "setup": 7},
			I18n: brandI18n("DPI Triple", map[string]string{
				"en":    "Reality, Naive TLS, and Hy2.",
				"ru":    "Reality, Naive TLS и Hy2.",
				"ar":    "Reality و Naive TLS و Hy2.",
				"es":    "Reality, Naive TLS y Hy2.",
				"fa":    "Reality، Naive TLS و Hy2.",
				"fr":    "Reality, Naive TLS et Hy2.",
				"id":    "Reality, Naive TLS, dan Hy2.",
				"pt-BR": "Reality, Naive TLS e Hy2.",
				"tr":    "Reality, Naive TLS ve Hy2.",
				"zh-CN": "Reality、Naive TLS 与 Hy2。",
				"zh-TW": "Reality、Naive TLS 與 Hy2。",
			}),
			Slots: []Slot{
				{ID: "reality", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "tls", Role: RoleTCPTLS, DefaultPreset: "naive_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
		},
		{
			Tag: "dg_443_fullstack", ShortName: "DPI Killer", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 6, "setup": 5},
			I18n: brandI18n("DPI Killer", map[string]string{
				"en":    "Broad stack with wide fallbacks.",
				"ru":    "Широкий стек с запасными вариантами.",
				"ar":    "مجموعة واسعة مع بدائل كثيرة.",
				"es":    "Pila amplia con muchos respaldos.",
				"fa":    "پشتهٔ گسترده با جایگزین‌های زیاد.",
				"fr":    "Pile large avec de nombreux secours.",
				"id":    "Tumpukan luas dengan banyak cadangan.",
				"pt-BR": "Pilha ampla com várias reservas.",
				"tr":    "Geniş yığın, bol yedekler.",
				"zh-CN": "宽覆盖协议栈，备选丰富。",
				"zh-TW": "寬覆蓋協議棧，備選豐富。",
			}),
			Slots: []Slot{
				{ID: "reality", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "tls_h2", Role: RoleTCPTLS, DefaultPreset: "naive_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "tls_h1", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"http/1.1"}},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
				{ID: "plain", Role: RoleTCPPlain, DefaultPreset: "mieru_tcp", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
			},
			Notes: "Flagship stable (5): Reality + Naive/AnyTLS + Hy2 + plain.",
		},
		{
			Tag: "dg_443_tls_quic", ShortName: "HTTPS Mask", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 8, "speed": 8, "mobile": 7, "setup": 8},
			I18n: brandI18n("HTTPS Mask", map[string]string{
				"en":    "Naive TLS with Hy2 fallback.",
				"ru":    "Naive TLS с запасным Hy2.",
				"ar":    "Naive TLS مع Hy2 احتياطي.",
				"es":    "Naive TLS con Hy2 de respaldo.",
				"fa":    "Naive TLS با پشتیبان Hy2.",
				"fr":    "Naive TLS avec secours Hy2.",
				"id":    "Naive TLS dengan cadangan Hy2.",
				"pt-BR": "Naive TLS com Hy2 de reserva.",
				"tr":    "Hy2 yedekli Naive TLS.",
				"zh-CN": "Naive TLS，Hy2 作为备用。",
				"zh-TW": "Naive TLS，Hy2 作為備用。",
			}),
			Slots: []Slot{
				{ID: "tls", Role: RoleTCPTLS, DefaultPreset: "naive_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
			},
			Notes: "Pick naive_quic on TLS to also claim QUIC (H2+H3); separate Hy2 slot is then skipped.",
		},
		{
			Tag: "dg_443_exotic", ShortName: "Oddball", Status: "stable", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 7, "speed": 7, "mobile": 5, "setup": 6},
			I18n: brandI18n("Oddball", map[string]string{
				"en":    "Exotic mix: Naive, Hy2, and plain.",
				"ru":    "Экзотика: Naive, Hy2 и plain.",
				"ar":    "مزيج غريب: Naive و Hy2 و plain.",
				"es":    "Mezcla exótica: Naive, Hy2 y plain.",
				"fa":    "ترکیب غیرمعمول: Naive، Hy2 و plain.",
				"fr":    "Mélange exotique : Naive, Hy2 et plain.",
				"id":    "Campuran eksotis: Naive, Hy2, dan plain.",
				"pt-BR": "Mistura exótica: Naive, Hy2 e plain.",
				"tr":    "Egzotik karışım: Naive, Hy2 ve plain.",
				"zh-CN": "非常规组合：Naive、Hy2 与 plain。",
				"zh-TW": "非常規組合：Naive、Hy2 與 plain。",
			}),
			Slots: []Slot{
				{ID: "reality", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "tls", Role: RoleTCPTLS, DefaultPreset: "naive_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "protocol_only"},
				{ID: "plain", Role: RoleTCPPlain, DefaultPreset: "sudoku_pad", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
			},
			Notes: "Naive as TLS base; plain sudoku/mieru/ssh via substitutes.",
		},

		// --- lab ---
		{
			Tag: "dg_443_modern5", ShortName: "Vision Pack", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 6, "setup": 5},
			I18n: brandI18n("Vision Pack", map[string]string{
				"en":    "Dual Reality, Naive, and dual QUIC.",
				"ru":    "Два Reality, Naive и два QUIC.",
				"ar":    "Reality مزدوج و Naive و QUIC مزدوج.",
				"es":    "Doble Reality, Naive y doble QUIC.",
				"fa":    "دو Reality، Naive و دو QUIC.",
				"fr":    "Double Reality, Naive et double QUIC.",
				"id":    "Reality ganda, Naive, dan QUIC ganda.",
				"pt-BR": "Reality duplo, Naive e QUIC duplo.",
				"tr":    "Çift Reality, Naive ve çift QUIC.",
				"zh-CN": "双 Reality、Naive 与双 QUIC。",
				"zh-TW": "雙 Reality、Naive 與雙 QUIC。",
			}),
			Slots: []Slot{
				{ID: "reality", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "reality2", Role: RoleTCPReality, DefaultPreset: "vless_grpc_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "tls", Role: RoleTCPTLS, DefaultPreset: "naive_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "hy2", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "sni_pool"},
				{ID: "tuic", Role: RoleQUIC, DefaultPreset: "tuic", Substitutes: []string{"tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
			},
		},
		{
			Tag: "dg_443_sni_stack", ShortName: "SNI Lattice", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp"},
			Scores:   map[string]int{"dpi": 8, "speed": 7, "mobile": 7, "setup": 6},
			I18n: brandI18n("SNI Lattice", map[string]string{
				"en":    "Multiple TLS and Reality by hostname.",
				"ru":    "Несколько TLS и Reality по имени хоста.",
				"ar":    "عدة TLS و Reality حسب اسم المضيف.",
				"es":    "Varios TLS y Reality por nombre de host.",
				"fa":    "چند TLS و Reality بر اساس نام میزبان.",
				"fr":    "Plusieurs TLS et Reality par nom d’hôte.",
				"id":    "Beberapa TLS dan Reality menurut hostname.",
				"pt-BR": "Vários TLS e Reality por hostname.",
				"tr":    "Ana bilgisayar adına göre birden fazla TLS ve Reality.",
				"zh-CN": "按主机名区分的多路 TLS / Reality。",
				"zh-TW": "依主機名區分的多路 TLS / Reality。",
			}),
			Slots: []Slot{
				{ID: "r1", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "r2", Role: RoleTCPReality, DefaultPreset: "vless_grpc_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "t1", Role: RoleTCPTLS, DefaultPreset: "naive_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "t2", Role: RoleTCPTLS, DefaultPreset: "anytls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
				{ID: "t3", Role: RoleTCPTLS, DefaultPreset: "vless_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool"},
			},
		},
		{
			Tag: "dg_443_broad7", ShortName: "Full Arsenal", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 7, "mobile": 5, "setup": 3},
			I18n: brandI18n("Full Arsenal", map[string]string{
				"en":    "Largest lab mix.",
				"ru":    "Самый полный lab-набор.",
				"ar":    "أكبر مزيج lab.",
				"es":    "La mezcla lab más completa.",
				"fa":    "کامل‌ترین ترکیب lab.",
				"fr":    "Le plus grand mix lab.",
				"id":    "Campuran lab terbesar.",
				"pt-BR": "Maior mistura lab.",
				"tr":    "En geniş lab karışımı.",
				"zh-CN": "最完整的 lab 组合。",
				"zh-TW": "最完整的 lab 組合。",
			}),
			Slots: []Slot{
				{ID: "r1", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "r2", Role: RoleTCPReality, DefaultPreset: "vless_grpc_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "t1", Role: RoleTCPTLS, DefaultPreset: "naive_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "t2", Role: RoleTCPTLS, DefaultPreset: "vless_grpc_tls", Substitutes: tcpTLSSubs, MatchHint: "sni_pool", PreferredALPN: []string{"h2"}},
				{ID: "plain", Role: RoleTCPPlain, DefaultPreset: "mieru_tcp", Substitutes: tcpPlainSubs, MatchHint: "always_plain"},
				{ID: "hy2", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "sni_pool"},
				{ID: "tuic", Role: RoleQUIC, DefaultPreset: "tuic", Substitutes: []string{"tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
			},
			Notes: "Largest lab set (7). Prefer Vision Pack / DPI Killer unless you need two QUIC + plain.",
		},
		{
			Tag: "dg_443_quic_storm", ShortName: "QUIC Storm", Status: "lab", SuggestedPort: 443,
			Networks: []string{"udp"},
			Scores:   map[string]int{"dpi": 7, "speed": 9, "mobile": 5, "setup": 6},
			I18n: brandI18n("QUIC Storm", map[string]string{
				"en":    "Two QUIC profiles.",
				"ru":    "Два QUIC-профиля.",
				"ar":    "ملفّا QUIC.",
				"es":    "Dos perfiles QUIC.",
				"fa":    "دو پروفایل QUIC.",
				"fr":    "Deux profils QUIC.",
				"id":    "Dua profil QUIC.",
				"pt-BR": "Dois perfis QUIC.",
				"tr":    "İki QUIC profili.",
				"zh-CN": "两个 QUIC 配置。",
				"zh-TW": "兩個 QUIC 設定。",
			}),
			Slots: []Slot{
				{ID: "hy2", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: quicSubs, MatchHint: "sni_pool"},
				{ID: "tuic", Role: RoleQUIC, DefaultPreset: "tuic", Substitutes: []string{"tuic", "tuic_0rtt"}, MatchHint: "sni_pool"},
			},
		},
		{
			Tag: "dg_443_reality_sq", ShortName: "Shadow Lane", Status: "lab", SuggestedPort: 443,
			Networks: []string{"tcp", "udp"},
			Scores:   map[string]int{"dpi": 9, "speed": 8, "mobile": 5, "setup": 5},
			I18n: brandI18n("Shadow Lane", map[string]string{
				"en":    "Reality with lab QUIC.",
				"ru":    "Reality с lab QUIC.",
				"ar":    "Reality مع QUIC lab.",
				"es":    "Reality con QUIC lab.",
				"fa":    "Reality با QUIC lab.",
				"fr":    "Reality avec QUIC lab.",
				"id":    "Reality dengan QUIC lab.",
				"pt-BR": "Reality com QUIC lab.",
				"tr":    "Lab QUIC’li Reality.",
				"zh-CN": "Reality 搭配 lab QUIC。",
				"zh-TW": "Reality 搭配 lab QUIC。",
			}),
			Slots: []Slot{
				{ID: "tcp", Role: RoleTCPReality, DefaultPreset: "vless_reality", Substitutes: tcpRealitySubs, MatchHint: "sni_pool"},
				{ID: "quic", Role: RoleQUIC, DefaultPreset: "hy2", Substitutes: []string{"hy2", "shadowquic_jls", "shadowquic_0rtt", "shadowquic_cubic", "tuic"}, MatchHint: "protocol_only"},
			},
			Notes: "ShadowQUIC is demux_lab; default Hy2. Salamander obfs is not used in demux.",
		},
	}
}

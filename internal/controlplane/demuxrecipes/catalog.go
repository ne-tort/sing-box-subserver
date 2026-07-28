//go:build with_controlplane

package demuxrecipes

import (
	"fmt"
	"sync"
)

// Recipe is a named demux_template skeleton operators can copy into a set.
type Recipe struct {
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	RequiredPresets  []string       `json:"required_presets"`
	SuggestedPort    uint16         `json:"suggested_port,omitempty"`
	DemuxTemplate    map[string]any `json:"demux_template"`
}

var (
	once   sync.Once
	all    []Recipe
	byName map[string]Recipe
)

func load() {
	all = []Recipe{
		{
			Name:            "tls-trojan-plain-vless",
			Description:     "TLS ClientHello → trojan-tcp; everything else → vless-tcp",
			RequiredPresets: []string{"trojan-tcp", "vless-tcp"},
			SuggestedPort:   443,
			DemuxTemplate: map[string]any{
				"network": []any{"tcp"},
				"rules": []any{
					map[string]any{
						"name":   "tls-to-trojan",
						"match":  map[string]any{"protocol": "tls"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:trojan-tcp}}"}},
					},
					map[string]any{
						"name":   "plain-to-vless",
						"match":  map[string]any{"always": true},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:vless-tcp}}"}},
					},
				},
			},
		},
		{
			Name:            "tls-trojan-plain-ss",
			Description:     "TLS → trojan-tcp; plaintext TCP → shadowsocks-tcp",
			RequiredPresets: []string{"trojan-tcp", "shadowsocks-tcp"},
			SuggestedPort:   443,
			DemuxTemplate: map[string]any{
				"network": []any{"tcp"},
				"rules": []any{
					map[string]any{
						"name":   "tls-to-trojan",
						"match":  map[string]any{"protocol": "tls"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:trojan-tcp}}"}},
					},
					map[string]any{
						"name":   "plain-to-ss",
						"match":  map[string]any{"always": true},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:shadowsocks-tcp}}"}},
					},
				},
			},
		},
		{
			Name:            "sni-trojan-or-vless-tls",
			Description:     "SNI wiki/default host → trojan; other TLS SNI → vless-tls; reject non-TLS",
			RequiredPresets: []string{"trojan-tcp", "vless-tls"},
			SuggestedPort:   443,
			DemuxTemplate: map[string]any{
				"network": []any{"tcp"},
				"rules": []any{
					map[string]any{
						"name": "sni-wiki-trojan",
						"match": map[string]any{
							"tls": map[string]any{"sni": []any{"wiki.ai-qwerty.ru", "localhost"}},
						},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:trojan-tcp}}"}},
					},
					map[string]any{
						"name":   "other-tls-vless",
						"match":  map[string]any{"protocol": "tls"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:vless-tls}}"}},
					},
					map[string]any{
						"name":   "reject-plain",
						"match":  map[string]any{"always": true},
						"action": map[string]any{"reject": true},
					},
				},
			},
		},
		{
			Name:            "tls-trojan-plain-vmess",
			Description:     "TLS → trojan-tcp; plaintext → vmess-tcp",
			RequiredPresets: []string{"trojan-tcp", "vmess-tcp"},
			SuggestedPort:   8443,
			DemuxTemplate: map[string]any{
				"network": []any{"tcp"},
				"rules": []any{
					map[string]any{
						"name":   "tls-to-trojan",
						"match":  map[string]any{"protocol": "tls"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:trojan-tcp}}"}},
					},
					map[string]any{
						"name":   "plain-to-vmess",
						"match":  map[string]any{"always": true},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:vmess-tcp}}"}},
					},
				},
			},
		},
		{
			Name:            "quic-hy2-tls-trojan",
			Description:     "QUIC → hysteria2; TCP TLS → trojan-tcp",
			RequiredPresets: []string{"hysteria2", "trojan-tcp"},
			SuggestedPort:   443,
			DemuxTemplate: map[string]any{
				"network": []any{"tcp", "udp"},
				"rules": []any{
					map[string]any{
						"name":   "quic-to-hy2",
						"match":  map[string]any{"protocol": "quic"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:hysteria2}}"}},
					},
					map[string]any{
						"name":   "tls-to-trojan",
						"match":  map[string]any{"protocol": "tls"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:trojan-tcp}}"}},
					},
					map[string]any{
						"name":   "reject-rest",
						"match":  map[string]any{"always": true},
						"action": map[string]any{"reject": true},
					},
				},
			},
		},
		{
			Name:            "tls-only-trojan",
			Description:     "Single TLS path to trojan-tcp; reject non-TLS (port hygiene)",
			RequiredPresets: []string{"trojan-tcp"},
			SuggestedPort:   443,
			DemuxTemplate: map[string]any{
				"network": []any{"tcp"},
				"rules": []any{
					map[string]any{
						"name":   "tls-to-trojan",
						"match":  map[string]any{"protocol": "tls"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:trojan-tcp}}"}},
					},
					map[string]any{
						"name":   "reject-plain",
						"match":  map[string]any{"always": true},
						"action": map[string]any{"reject": true},
					},
				},
			},
		},
		{
			Name:            "tls-anytls-plain-socks",
			Description:     "TLS → anytls; plaintext TCP → socks",
			RequiredPresets: []string{"anytls", "socks"},
			SuggestedPort:   1080,
			DemuxTemplate: map[string]any{
				"network": []any{"tcp"},
				"rules": []any{
					map[string]any{
						"name":   "tls-to-anytls",
						"match":  map[string]any{"protocol": "tls"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:anytls}}"}},
					},
					map[string]any{
						"name":   "plain-to-socks",
						"match":  map[string]any{"always": true},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:socks}}"}},
					},
				},
			},
		},
		{
			Name:            "tls-vmess-plain-http",
			Description:     "TLS → vmess-tls; plaintext → http CONNECT",
			RequiredPresets: []string{"vmess-tls", "http"},
			SuggestedPort:   8081,
			DemuxTemplate: map[string]any{
				"network": []any{"tcp"},
				"rules": []any{
					map[string]any{
						"name":   "tls-to-vmess",
						"match":  map[string]any{"protocol": "tls"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:vmess-tls}}"}},
					},
					map[string]any{
						"name":   "plain-to-http",
						"match":  map[string]any{"always": true},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:http}}"}},
					},
				},
			},
		},
		{
			Name:            "quic-tuic-tls-trojan",
			Description:     "QUIC → tuic; TCP TLS → trojan-tcp",
			RequiredPresets: []string{"tuic", "trojan-tcp"},
			SuggestedPort:   443,
			DemuxTemplate: map[string]any{
				"network": []any{"tcp", "udp"},
				"rules": []any{
					map[string]any{
						"name":   "quic-to-tuic",
						"match":  map[string]any{"protocol": "quic"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:tuic}}"}},
					},
					map[string]any{
						"name":   "tls-to-trojan",
						"match":  map[string]any{"protocol": "tls"},
						"action": map[string]any{"inbound": map[string]any{"tag": "{{tag:trojan-tcp}}"}},
					},
					map[string]any{
						"name":   "reject-rest",
						"match":  map[string]any{"always": true},
						"action": map[string]any{"reject": true},
					},
				},
			},
		},
	}
	byName = make(map[string]Recipe, len(all))
	for _, r := range all {
		byName[r.Name] = r
	}
}

func All() []Recipe {
	once.Do(load)
	out := make([]Recipe, len(all))
	copy(out, all)
	return out
}

func Get(name string) (Recipe, error) {
	once.Do(load)
	r, ok := byName[name]
	if !ok {
		return Recipe{}, fmt.Errorf("unknown demux recipe %q", name)
	}
	return r, nil
}

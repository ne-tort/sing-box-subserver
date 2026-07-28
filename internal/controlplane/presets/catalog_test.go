//go:build with_controlplane

package presets

import "testing"

func TestCatalogLoads(t *testing.T) {
	all := All()
	if len(all) < 8 {
		t.Fatalf("expected >=8 presets, got %d", len(all))
	}
	seen := map[string]struct{}{}
	for _, p := range all {
		if p.Name == "" || p.Protocol == "" {
			t.Fatalf("invalid preset %+v", p)
		}
		if _, ok := seen[p.Name]; ok {
			t.Fatalf("duplicate %s", p.Name)
		}
		seen[p.Name] = struct{}{}
		if len(p.InboundTemplate) == 0 || len(p.OutboundTemplate) == 0 {
			t.Fatalf("%s: missing templates", p.Name)
		}
		if len(p.CredFields) == 0 {
			t.Fatalf("%s: cred_fields empty", p.Name)
		}
	}
	for _, want := range []string{
		"shadowsocks-tcp", "shadowsocks-aes-256-gcm", "shadowsocks-chacha20",
		"trojan-tcp", "vless-tcp", "vless-tls", "vmess-tcp", "vmess-tls",
		"hysteria2", "tuic", "anytls", "socks", "http",
	} {
		if _, err := Get(want); err != nil {
			t.Fatalf("missing preset %s: %v", want, err)
		}
	}
}

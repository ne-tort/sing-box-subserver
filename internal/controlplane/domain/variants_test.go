//go:build with_controlplane

package domain

import "testing"

func TestUserVariantsForProtocolDefaultCatalog(t *testing.T) {
	t.Parallel()
	b := SetBinding{Preset: "vless-tcp"}
	got := UserVariantsForProtocol("vless", b, nil)
	if len(got) != len(VLESSUserVariantSpecs) {
		t.Fatalf("got %d want %d", len(got), len(VLESSUserVariantSpecs))
	}
}

func TestUserVariantsForProtocolPresetDefaults(t *testing.T) {
	t.Parallel()
	b := SetBinding{Preset: "vless_ws_tls"}
	got := UserVariantsForProtocol("vless", b, []string{"flow-none"})
	if len(got) != 1 || got[0].Name != "flow-none" {
		t.Fatalf("got %#v", got)
	}
}

func TestUserVariantsForProtocolEnabledSubset(t *testing.T) {
	t.Parallel()
	b := SetBinding{
		Preset:              "vless-tcp",
		EnabledUserVariants: []string{"flow-udp-vision", "flow-none"},
	}
	got := UserVariantsForProtocol("vless", b, []string{"flow-none"})
	if len(got) != 2 || got[0].Name != "flow-udp-vision" || got[1].Name != "flow-none" {
		t.Fatalf("got %#v", got)
	}
}

func TestUserVariantsForProtocolUnknownEnabledFallback(t *testing.T) {
	t.Parallel()
	b := SetBinding{
		Preset:              "vless-tcp",
		EnabledUserVariants: []string{"flow-nope"},
	}
	got := UserVariantsForProtocol("vless", b, nil)
	if len(got) != len(VLESSUserVariantSpecs) {
		t.Fatalf("fallback catalog: got %d", len(got))
	}
}

func TestUserVariantsForProtocolNonVless(t *testing.T) {
	t.Parallel()
	if got := UserVariantsForProtocol("trojan", SetBinding{Preset: "trojan-tcp"}, nil); got != nil {
		t.Fatalf("got %#v want nil", got)
	}
}

func TestClientProfilesForProtocolDefault(t *testing.T) {
	t.Parallel()
	got := ClientProfilesForProtocol("vless", SetBinding{Preset: "vless_tls"}, nil)
	if len(got) != 1 || got[0].Name != "pkt-none" {
		t.Fatalf("got %#v", got)
	}
}

func TestClientProfilesForProtocolPresetDefaults(t *testing.T) {
	t.Parallel()
	got := ClientProfilesForProtocol("vless", SetBinding{}, []string{"pkt-xudp", "pkt-none"})
	if len(got) != 2 || got[0].Name != "pkt-xudp" || got[1].Name != "pkt-none" {
		t.Fatalf("got %#v", got)
	}
}

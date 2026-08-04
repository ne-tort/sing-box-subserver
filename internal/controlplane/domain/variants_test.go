//go:build with_controlplane

package domain

import (
	"os"
	"testing"
)

// Fixture VLESS catalogs for unit-testing resolution logic without SQLite.
var fixtureVLESSUserVariants = []UserVariantSpec{
	{
		Name:                       "flow-none",
		ProtocolFamily:             "vless",
		Scope:                      FieldScopeUserSymmetric,
		CredentialField:            "uuid",
		RequiresUserSymmetricEntry: true,
		QueryTags:                  []string{"variant:flow-none", "flow:none", "tag:flow-none"},
	},
	{
		Name:                       "flow-xtls-rprx-vision",
		ProtocolFamily:             "vless",
		Scope:                      FieldScopeUserSymmetric,
		CredentialField:            "uuid_xtls",
		FlowValue:                  "xtls-rprx-vision",
		RequiresUserSymmetricEntry: true,
		SubscriptionDefault:        true,
		QueryTags:                  []string{"variant:flow-xtls-rprx-vision", "flow:xtls-rprx-vision", "tag:flow-xtls"},
	},
	{
		Name:                       "flow-udp-vision",
		ProtocolFamily:             "vless",
		Scope:                      FieldScopeUserSymmetric,
		CredentialField:            "uuid_udp",
		FlowValue:                  "xtls-rprx-vision-udp443",
		RequiresUserSymmetricEntry: true,
		QueryTags:                  []string{"variant:flow-udp-vision", "flow:udp-vision", "tag:flow-udp"},
	},
}

var fixtureVLESSClientProfiles = []ClientProfileSpec{
	{
		Name:                "pkt-none",
		ProtocolFamily:      "vless",
		Scope:               FieldScopeOutboundOnly,
		SubscriptionDefault: true,
		QueryTags:           []string{"profile:pkt-none", "tag:pkt-none"},
	},
	{
		Name:                "pkt-xudp",
		ProtocolFamily:      "vless",
		Scope:               FieldScopeOutboundOnly,
		QueryTags:           []string{"profile:pkt-xudp", "tag:pkt-xudp"},
		OutboundOverrides:   map[string]any{"packet_encoding": "xudp"},
	},
	{
		Name:              "pkt-packetaddr",
		ProtocolFamily:    "vless",
		Scope:             FieldScopeOutboundOnly,
		QueryTags:         []string{"profile:pkt-packetaddr", "tag:pkt-packetaddr"},
		OutboundOverrides: map[string]any{"packet_encoding": "packetaddr"},
	},
}

type fixtureVariantProvider struct{}

func (fixtureVariantProvider) UserVariants(protocol string) ([]UserVariantSpec, bool) {
	if protocol != "vless" {
		return nil, false
	}
	out := make([]UserVariantSpec, len(fixtureVLESSUserVariants))
	copy(out, fixtureVLESSUserVariants)
	return out, true
}

func (fixtureVariantProvider) ClientProfiles(protocol string) ([]ClientProfileSpec, bool) {
	if protocol != "vless" {
		return nil, false
	}
	out := make([]ClientProfileSpec, len(fixtureVLESSClientProfiles))
	copy(out, fixtureVLESSClientProfiles)
	return out, true
}

func TestMain(m *testing.M) {
	SetVariantCatalogProvider(fixtureVariantProvider{})
	os.Exit(m.Run())
}

func TestUserVariantsForProtocolDefaultCatalog(t *testing.T) {
	t.Parallel()
	b := SetBinding{Preset: "vless-tcp"}
	got := UserVariantsForProtocol("vless", b, nil)
	// Empty enabled → SubscriptionDefault only (not full catalog).
	want := 0
	for _, vv := range fixtureVLESSUserVariants {
		if vv.SubscriptionDefault {
			want++
		}
	}
	if len(got) != want {
		t.Fatalf("got %d want %d SubscriptionDefault", len(got), want)
	}
	for _, vv := range got {
		if !vv.SubscriptionDefault {
			t.Fatalf("non-default variant %q in empty-enabled result", vv.Name)
		}
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
	want := 0
	for _, vv := range fixtureVLESSUserVariants {
		if vv.SubscriptionDefault {
			want++
		}
	}
	if len(got) != want {
		t.Fatalf("fallback SubscriptionDefault: got %d want %d", len(got), want)
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

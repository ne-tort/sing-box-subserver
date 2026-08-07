//go:build with_controlplane

package domain

import "testing"

func TestTLSProfileValidate(t *testing.T) {
	if err := (TLSProfile{}).Validate(); err == nil {
		t.Fatal("expected error")
	}
	p := DefaultSelfSigned("vpn.example.com")
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.ServerNameForTLS("") != "vpn.example.com" {
		t.Fatalf("server name=%q", p.ServerNameForTLS(""))
	}
}

func TestConfigFragments(t *testing.T) {
	if err := ValidateDNSFragment(DefaultDNSFragment()); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRouteFragment(DefaultRouteFragment()); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOutboundsFragment(DefaultOutboundsFragment()); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDNSFragment([]byte(`{"servers":[]}`)); err == nil {
		t.Fatal("empty servers")
	}
	if err := ValidateDNSFragment([]byte(`{"servers":[{"type":"local"}],"x":"{{tag}}"}`)); err == nil {
		t.Fatal("template token")
	}
	if err := ValidateRouteFragment([]byte(`{"rules":[]}`)); err == nil {
		t.Fatal("final required")
	}
	if err := ValidateRouteFragment([]byte(`{"final":"","rules":[]}`)); err == nil {
		t.Fatal("empty final")
	}
	if err := ValidateOutboundsFragment([]byte(`{}`)); err == nil {
		t.Fatal("object not array")
	}
	if err := ValidateOutboundsFragment([]byte(`[]`)); err == nil {
		t.Fatal("empty array")
	}
	if err := ValidateOutboundsFragment([]byte(`[{"type":"direct"}]`)); err == nil {
		t.Fatal("tag required")
	}
	if err := ValidateOutboundsFragment([]byte(`[{"type":"direct","tag":"direct"},{"type":"block","tag":"direct"}]`)); err == nil {
		t.Fatal("duplicate tag")
	}
	empty := ConfigFragments{}
	if !empty.DNSIsDefault() || !empty.RouteIsDefault() || !empty.OutboundsIsDefault() {
		t.Fatal("empty should be default")
	}
	custom := ConfigFragments{Outbounds: []byte(`[{"type":"direct","tag":"direct"}]`)}
	if custom.OutboundsIsDefault() {
		t.Fatal("custom outbounds")
	}
	if string(custom.EffectiveOutbounds()) != `[{"type":"direct","tag":"direct"}]` {
		t.Fatalf("effective=%s", custom.EffectiveOutbounds())
	}
}

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

func TestCertManagerValidate(t *testing.T) {
	empty := CertManager{}
	if err := empty.Validate(); err != nil {
		t.Fatal(err)
	}
	cm := CertManager{Email: "a@b.c", Domains: []string{"vpn.example.com"}}
	if err := cm.Validate(); err != nil {
		t.Fatal(err)
	}
	if !cm.HasDomain("VPN.example.com") {
		t.Fatal("HasDomain")
	}
	bad := CertManager{Domains: []string{"vpn.example.com"}}
	if err := bad.Validate(); err == nil {
		t.Fatal("email required")
	}
	mix := CertManager{Email: "a@b.c", Domains: []string{"1.2.3.4", "vpn.example.com"}}
	if err := mix.Validate(); err == nil {
		t.Fatal("mix ip/dns")
	}
}

func TestNeedsTLSReportsInsecure(t *testing.T) {
	cm := CertManager{Email: "a@b.c", Domains: []string{"vpn.example.com"}}
	if !NeedsTLSReportsInsecure("", cm) {
		t.Fatal("empty sni => insecure")
	}
	if NeedsTLSReportsInsecure("vpn.example.com", cm) {
		t.Fatal("acme sni => secure")
	}
}

func TestConfigFragments(t *testing.T) {
	if err := ValidateDNSFragment(DefaultDNSFragment()); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRouteFragment(DefaultRouteFragment()); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDNSFragment([]byte(`{"servers":[]}`)); err == nil {
		t.Fatal("empty servers")
	}
	if err := ValidateDNSFragment([]byte(`{"servers":[{"type":"local"}],"x":"{{tag}}"}`)); err == nil {
		t.Fatal("template token")
	}
}

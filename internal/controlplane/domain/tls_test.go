//go:build with_controlplane

package domain

import "testing"

func TestTLSProfileValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		profile TLSProfile
		ok      bool
	}{
		{
			name:    "default self_signed ip",
			profile: DefaultSelfSigned("203.0.113.10"),
			ok:      true,
		},
		{
			name:    "default self_signed dns",
			profile: DefaultSelfSigned("vpn.example.com"),
			ok:      true,
		},
		{
			name: "self_signed missing spec",
			profile: TLSProfile{Mode: TLSModeSelfSigned},
			ok:      false,
		},
		{
			name: "self_signed bad key",
			profile: TLSProfile{Mode: TLSModeSelfSigned, SelfSigned: &SelfSignedSpec{
				CommonName: "x", KeyType: "ed25519", ValidDays: 1,
			}},
			ok: false,
		},
		{
			name: "acme_domain ok",
			profile: TLSProfile{Mode: TLSModeACMEDomain, ACME: &ACMESpec{
				Email: "a@b.c", Domains: []string{"vpn.example.com"},
			}},
			ok: true,
		},
		{
			name: "acme_domain bare ip",
			profile: TLSProfile{Mode: TLSModeACMEDomain, ACME: &ACMESpec{
				Email: "a@b.c", Domains: []string{"1.2.3.4"},
			}},
			ok: false,
		},
		{
			name: "acme_ip ok",
			profile: TLSProfile{Mode: TLSModeACMEIP, ACME: &ACMESpec{
				Email: "a@b.c", Domains: []string{"1.2.3.4"}, Provider: "letsencrypt",
			}},
			ok: true,
		},
		{
			name: "acme_ip zerossl",
			profile: TLSProfile{Mode: TLSModeACMEIP, ACME: &ACMESpec{
				Email: "a@b.c", Domains: []string{"1.2.3.4"}, Provider: "zerossl",
			}},
			ok: false,
		},
		{
			name: "acme_ip dns01",
			profile: TLSProfile{Mode: TLSModeACMEIP, ACME: &ACMESpec{
				Email: "a@b.c", Domains: []string{"1.2.3.4"}, Provider: "letsencrypt",
				DNS01Challenge: map[string]any{"provider": "cloudflare"},
			}},
			ok: false,
		},
		{
			name:    "unknown mode",
			profile: TLSProfile{Mode: "file"},
			ok:      false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.profile.Validate()
			if tc.ok && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected error")
			}
		})
	}

	p := DefaultSelfSigned("203.0.113.10")
	if len(p.SelfSigned.IPSANs) != 1 {
		t.Fatalf("ip sans: %+v", p.SelfSigned.IPSANs)
	}
	if !p.NeedsTLSReportsInsecure() {
		t.Fatal("self_signed should report insecure")
	}
	acme := TLSProfile{Mode: TLSModeACMEDomain, ACME: &ACMESpec{Email: "a@b.c", Domains: []string{"vpn.example.com"}}}
	if acme.NeedsTLSReportsInsecure() {
		t.Fatal("acme must not report insecure")
	}
	if acme.ServerNameForTLS("fallback") != "vpn.example.com" {
		t.Fatalf("sni=%s", acme.ServerNameForTLS("fallback"))
	}
}

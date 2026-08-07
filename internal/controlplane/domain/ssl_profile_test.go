//go:build with_controlplane

package domain

import (
	"strings"
	"testing"
)

func TestACMEEmailAutoAndNormalize(t *testing.T) {
	p := SSLProfile{ID: "x", Name: "n", Type: SSLTypeACME, Domain: "vpn.example.com", Email: "Auto"}.Normalize()
	if p.Email != "" {
		t.Fatalf("auto must clear email, got %q", p.Email)
	}
	if !p.ACMEEmailAuto() {
		t.Fatal("expected auto")
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	p.Email = "not-an-email"
	if err := p.Validate(); err == nil {
		t.Fatal("expected invalid email")
	}
	p.Email = "ops@example.com"
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.ACMEEmailAuto() {
		t.Fatal("explicit email is not auto")
	}
}

func TestPickAutoACMEEmailStable(t *testing.T) {
	pool := []string{"www.apple.com", "www.amazon.com", "github.com"}
	a := PickAutoACMEEmail("profile-aaa", pool)
	b := PickAutoACMEEmail("profile-aaa", pool)
	if a != b {
		t.Fatalf("unstable: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "admin@") {
		t.Fatalf("want admin@… got %q", a)
	}
	host := strings.TrimPrefix(a, "admin@")
	ok := false
	for _, s := range pool {
		if s == host {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("host %q not from pool", host)
	}
	c := PickAutoACMEEmail("profile-bbb", pool)
	// Different IDs usually differ; allow collision but check format.
	if !strings.HasPrefix(c, "admin@") {
		t.Fatalf("want admin@… got %q", c)
	}
}

func TestEffectiveACMEEmailOverride(t *testing.T) {
	p := SSLProfile{ID: "x", Name: "n", Type: SSLTypeACME, Domain: "d.example", Email: "me@corp.example"}
	if got := EffectiveACMEEmail(p, []string{"www.apple.com"}); got != "me@corp.example" {
		t.Fatalf("got %q", got)
	}
	p.Email = ""
	got := EffectiveACMEEmail(p, []string{"www.apple.com"})
	if got != "admin@www.apple.com" && !strings.HasPrefix(got, "admin@") {
		t.Fatalf("got %q", got)
	}
}

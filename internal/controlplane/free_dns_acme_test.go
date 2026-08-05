//go:build with_controlplane

package controlplane

import (
	"net"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func TestMergeDomainLists(t *testing.T) {
	got := mergeDomainLists([]string{"B.example", "a.example"}, []string{"a.example", "c.example"})
	want := []string{"a.example", "b.example", "c.example"}
	if len(got) != len(want) {
		t.Fatalf("%v vs %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v vs %v", got, want)
		}
	}
}

func TestCertManagerInIPMode(t *testing.T) {
	if !certManagerInIPMode(domain.CertManager{Domains: []string{"1.2.3.4"}}) {
		t.Fatal("expected ip mode")
	}
	if certManagerInIPMode(domain.CertManager{Domains: []string{"a.example"}}) {
		t.Fatal("dns mode")
	}
	if certManagerInIPMode(domain.CertManager{}) {
		t.Fatal("empty")
	}
	_ = net.IPv4(1, 2, 3, 4)
}

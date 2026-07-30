//go:build with_controlplane

package controlplane

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func TestPortNetworksTCPUDPShare(t *testing.T) {
	tcpSet := domain.InboundSet{
		Name: "tcp1", ListenPort: 443,
		Presets: []string{"vless_reality"},
		Bindings: []domain.SetBinding{{Preset: "vless_reality"}},
	}
	udpSet := domain.InboundSet{
		Name: "udp1", ListenPort: 443,
		Presets: []string{"hy2"},
		Bindings: []domain.SetBinding{{Preset: "hy2"}},
	}
	tcpN := portNetworks(tcpSet)
	udpN := portNetworks(udpSet)
	if len(tcpN) == 0 || len(udpN) == 0 {
		t.Fatalf("empty networks tcp=%v udp=%v", tcpN, udpN)
	}
	// Same port OK if networks don't overlap.
	overlap := false
	for _, a := range tcpN {
		for _, b := range udpN {
			if a == b {
				overlap = true
			}
		}
	}
	if overlap {
		t.Fatalf("expected no overlap between vless_reality and hy2 networks: %v vs %v", tcpN, udpN)
	}
}

func TestValidateSetAllowsTCPAndUDPSamePort(t *testing.T) {
	s := &Service{}
	tcpSet := domain.InboundSet{
		Name: "tcp1", Listen: "::", ListenPort: 443,
		Presets: []string{"vless_reality"},
		Bindings: []domain.SetBinding{{Preset: "vless_reality"}},
	}
	udpSet := domain.InboundSet{
		Name: "udp1", Listen: "::", ListenPort: 443,
		Presets: []string{"hy2"},
		Bindings: []domain.SetBinding{{Preset: "hy2"}},
	}
	if err := s.validateSet(udpSet, []domain.InboundSet{tcpSet}); err != nil {
		t.Fatalf("tcp+udp same port should be allowed: %v", err)
	}
	tcp2 := domain.InboundSet{
		Name: "tcp2", Listen: "::", ListenPort: 443,
		Presets: []string{"anytls"},
		Bindings: []domain.SetBinding{{Preset: "anytls"}},
	}
	if err := s.validateSet(tcp2, []domain.InboundSet{tcpSet}); err == nil {
		t.Fatal("two TCP on same port should conflict")
	}
}

func TestDemuxOccupiesBothNetworks(t *testing.T) {
	set := domain.InboundSet{
		Name: "dg", ListenPort: 443,
		DemuxTemplate: map[string]any{"network": []any{"tcp", "udp"}, "rules": []any{}},
		Presets:       []string{"vless_reality", "hy2"},
	}
	nets := portNetworks(set)
	if len(nets) != 2 {
		t.Fatalf("demux should occupy tcp+udp, got %v", nets)
	}
}

func TestSuggestListenPortSkipsOccupied(t *testing.T) {
	sets := []domain.InboundSet{
		{
			Name: "a", ListenPort: 443,
			Bindings: []domain.SetBinding{{Preset: "vless_reality"}},
		},
	}
	p, err := suggestListenPort(sets, []string{"tcp"})
	if err != nil {
		t.Fatal(err)
	}
	if p == 443 {
		t.Fatalf("expected non-443, got %d", p)
	}
}

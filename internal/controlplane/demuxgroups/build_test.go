//go:build with_controlplane

package demuxgroups

import (
	"fmt"
	"strings"
	"testing"
)

func TestCatalogNonEmpty(t *testing.T) {
	gs := All()
	if len(gs) < 5 {
		t.Fatalf("expected several demux groups, got %d", len(gs))
	}
	for _, g := range gs {
		if g.Tag == "" || len(g.Slots) == 0 {
			t.Fatalf("invalid group %#v", g)
		}
		for _, s := range g.Slots {
			if !s.AllowsPreset(s.DefaultPreset) {
				t.Fatalf("default not in substitutes: %s/%s", g.Tag, s.ID)
			}
		}
	}
}

func TestBuildInstallDual(t *testing.T) {
	res, err := BuildInstall(InstallRequest{
		GroupTag: "dg_443_dual",
		SetName:  "test-dual",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Set.ListenPort != 443 {
		t.Fatalf("port=%d", res.Set.ListenPort)
	}
	if !res.Set.HasDemux() {
		t.Fatal("expected demux")
	}
	if len(res.MemberPorts) != 2 {
		t.Fatalf("ports=%v", res.MemberPorts)
	}
	raw := fmt.Sprintf("%v", res.Set.DemuxTemplate)
	if !strings.Contains(raw, "dial") {
		t.Fatalf("expected dial actions: %s", raw)
	}
	if strings.Contains(raw, "inbound:") {
		t.Fatalf("inject inbound action should not appear: %s", raw)
	}
}

func TestBuildInstallDisabledSlots(t *testing.T) {
	res, err := BuildInstall(InstallRequest{
		GroupTag:      "dg_443_dual",
		SetName:       "test-disabled",
		DisabledSlots: []string{"quic"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.MemberPorts) != 1 {
		t.Fatalf("expected 1 member port after disabling quic, got %v", res.MemberPorts)
	}
	_, err = BuildInstall(InstallRequest{
		GroupTag:      "dg_443_dual",
		SetName:       "test-all-disabled",
		DisabledSlots: []string{"tcp", "quic"},
	}, nil)
	if err == nil {
		t.Fatal("expected error when all slots disabled")
	}
}

func TestBuildInstallSlotSNIOverride(t *testing.T) {
	res, err := BuildInstall(InstallRequest{
		GroupTag: "dg_443_triple",
		SetName:  "test-sni",
		SlotSNI:  map[string]string{"tls": "vpn.example.com"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range res.Set.Bindings {
		if b.Params["demux_sni"] == "vpn.example.com" && b.Params["sni"] == "vpn.example.com" {
			found = true
		}
		if strings.Contains(b.Preset, "reality") && b.Params["sni"] != "" {
			t.Fatalf("reality binding must not get params.sni: %#v", b)
		}
	}
	if !found {
		t.Fatalf("expected TLS slot_sni to set demux_sni+sni, bindings=%v slot_snis=%v", res.Set.Bindings, res.SlotSNIs)
	}
}

func TestSlotRejectUnknownPreset(t *testing.T) {
	_, err := BuildInstall(InstallRequest{
		GroupTag:   "dg_443_dual",
		SlotPreset: map[string]string{"tcp": "vmess_tls"},
	}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSlotRejectSalamander(t *testing.T) {
	_, err := BuildInstall(InstallRequest{
		GroupTag:   "dg_443_dual",
		SlotPreset: map[string]string{"quic": "hy2_salamander"},
	}, nil)
	if err == nil {
		t.Fatal("expected salamander rejected")
	}
	if !strings.Contains(err.Error(), "cp_invalid_slot") {
		t.Fatalf("err=%v", err)
	}
}

func TestCatalogOmitsSalamander(t *testing.T) {
	for _, g := range All() {
		for _, s := range g.Slots {
			for _, p := range s.AllPresets() {
				if p == "hy2_salamander" {
					t.Fatalf("%s/%s still lists hy2_salamander", g.Tag, s.ID)
				}
			}
		}
	}
}

func TestSubstitutions(t *testing.T) {
	v, err := Substitutions("dg_443_triple", "ru")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Slots) != 3 {
		t.Fatalf("slots=%d", len(v.Slots))
	}
}

func TestCatalogModernOnlySubstitutes(t *testing.T) {
	// Exact legacy tags / prefixes we refuse in demux group substitutes.
	forbiddenExact := map[string]struct{}{
		"http": {}, "socks": {}, "mixed": {}, "shadowsocks": {}, "ss": {},
	}
	forbiddenPrefix := []string{"vmess_", "trojan_", "hy1_", "hysteria1_", "shadowsocks_", "ss_"}
	for _, g := range All() {
		for _, s := range g.Slots {
			for _, p := range s.AllPresets() {
				low := strings.ToLower(p)
				if _, ok := forbiddenExact[low]; ok {
					t.Fatalf("%s/%s includes banned preset %q", g.Tag, s.ID, p)
				}
				for _, pref := range forbiddenPrefix {
					if strings.HasPrefix(low, pref) {
						t.Fatalf("%s/%s includes banned preset %q", g.Tag, s.ID, p)
					}
				}
			}
		}
	}
}

func TestModern5HasFiveSlots(t *testing.T) {
	g, err := Get("dg_443_modern5")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Slots) != 5 {
		t.Fatalf("slots=%d want 5", len(g.Slots))
	}
}

func TestBuildInstallNewGroups(t *testing.T) {
	for _, tag := range []string{"dg_443_reality_sq", "dg_443_snell_hy2", "dg_443_broad7"} {
		res, err := BuildInstall(InstallRequest{GroupTag: tag, SetName: "t-" + tag}, nil)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if !res.Set.HasDemux() {
			t.Fatalf("%s: expected demux", tag)
		}
		if len(res.MemberPorts) != len(res.Set.Presets) && len(res.MemberPorts) == 0 {
			t.Fatalf("%s: empty member ports", tag)
		}
	}
}

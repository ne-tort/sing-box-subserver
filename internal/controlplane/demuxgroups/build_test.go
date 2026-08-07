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
		if b.Params["demux_sni"] == "vpn.example.com" {
			found = true
		}
		if strings.Contains(b.Preset, "reality") && b.Params["sni"] != "" {
			t.Fatalf("reality binding must not get params.sni: %#v", b)
		}
	}
	if !found {
		t.Fatalf("expected TLS slot_sni to set demux_sni, bindings=%v slot_snis=%v", res.Set.Bindings, res.SlotSNIs)
	}
}

func TestBuildInstallSlotParams(t *testing.T) {
	res, err := BuildInstall(InstallRequest{
		GroupTag: "dg_443_dual",
		SetName:  "test-slot-params",
		SlotParams: map[string]map[string]string{
			"tcp": {"custom_note": "hello"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range res.Set.Bindings {
		if b.Params["custom_note"] == "hello" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected slot_params merged into bindings, got %#v", res.Set.Bindings)
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

func TestNaiveQUICOnTLSClaimsQUICSlot(t *testing.T) {
	res, err := BuildInstall(InstallRequest{
		GroupTag:   "dg_443_tls_quic",
		SetName:    "naive-claim",
		SlotPreset: map[string]string{"tls": "naive_quic"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.MemberPorts["naive_quic"]; !ok {
		t.Fatalf("expected naive_quic member, ports=%v", res.MemberPorts)
	}
	if _, ok := res.MemberPorts["hy2"]; ok {
		t.Fatalf("hy2 should be skipped when naive_quic claims QUIC, ports=%v", res.MemberPorts)
	}
	tmpl, ok := res.Set.DemuxTemplate["rules"].([]any)
	if !ok {
		t.Fatalf("rules type %T", res.Set.DemuxTemplate["rules"])
	}
	hasQuic := false
	for _, raw := range tmpl {
		m, _ := raw.(map[string]any)
		name, _ := m["name"].(string)
		if strings.HasPrefix(name, "quic-") {
			hasQuic = true
		}
	}
	if !hasQuic {
		t.Fatalf("expected quic rule from naive claim, rules=%v", tmpl)
	}
}

func TestUniqueSlotSNIsForMultiTLSGroups(t *testing.T) {
	for _, tag := range []string{"dg_443_fullstack", "dg_443_sni_stack", "dg_443_modern5"} {
		tag := tag
		t.Run(tag, func(t *testing.T) {
			res, err := BuildInstall(InstallRequest{GroupTag: tag, SetName: "sni-" + tag}, nil)
			if err != nil {
				t.Fatal(err)
			}
			g, err := Get(tag)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]string{}
			needSNI := 0
			for _, slot := range g.Slots {
				switch slot.Role {
				case RoleTCPReality, RoleTCPTLS:
					needSNI++
				case RoleQUIC:
					if slot.MatchHint == "sni_pool" {
						needSNI++
					}
				}
				sni := res.SlotSNIs[slot.ID]
				if slot.Role == RoleTCPPlain || (slot.Role == RoleQUIC && slot.MatchHint == "protocol_only") {
					if sni != "" {
						t.Fatalf("slot %s should not get SNI, got %q", slot.ID, sni)
					}
					continue
				}
			if sni == "" {
				t.Fatalf("slot %s missing SNI", slot.ID)
			}
			if strings.HasSuffix(sni, ".local") {
				t.Fatalf("slot %s must not use .local SNI, got %q", slot.ID, sni)
			}
			if other, ok := seen[sni]; ok {
					t.Fatalf("duplicate SNI %q on slots %s and %s", sni, other, slot.ID)
				}
				seen[sni] = slot.ID
			}
			if len(seen) != needSNI {
				t.Fatalf("unique SNIs=%d want %d map=%v", len(seen), needSNI, res.SlotSNIs)
			}
			raw := fmt.Sprintf("%v", res.Set.DemuxTemplate)
			if !strings.Contains(raw, "tls") || !strings.Contains(raw, "sni") {
				t.Fatalf("demux template should match tls.sni: %s", raw)
			}
			for sni := range seen {
				if !strings.Contains(raw, sni) {
					t.Fatalf("demux template missing SNI %q: %s", sni, raw)
				}
			}
		})
	}
}

func TestBuildInstallAllGroupsDefaults(t *testing.T) {
	for _, g := range All() {
		g := g
		t.Run(g.Tag, func(t *testing.T) {
			res, err := BuildInstall(InstallRequest{GroupTag: g.Tag, SetName: "t-" + g.Tag}, nil)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if !res.Set.HasDemux() {
				t.Fatal("expected demux")
			}
			wantMembers := len(g.Slots)
			if len(res.MemberPorts) != wantMembers {
				t.Fatalf("member ports=%d want %d (%v)", len(res.MemberPorts), wantMembers, res.MemberPorts)
			}
			raw := fmt.Sprintf("%v", res.Set.DemuxTemplate)
			if !strings.Contains(raw, "dial") {
				t.Fatalf("expected dial actions: %s", raw)
			}
			if strings.Contains(raw, "{{tag:") {
				t.Fatalf("legacy inject tag must not appear: %s", raw)
			}
		})
	}
}

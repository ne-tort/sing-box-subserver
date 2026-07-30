//go:build with_controlplane

package demuxgroups

import (
	"slices"
	"testing"
)

func TestCatalogAllSlotsHaveKnownMatchMeta(t *testing.T) {
	t.Parallel()
	known := map[string]struct{}{
		"tls.sni": {}, "tls.alpn": {}, "protocol.quic": {}, "protocol.quic+sni": {}, "always": {},
	}
	for _, g := range All() {
		meta := BuildGroupMatchMeta(g)
		if len(meta.Plan) != len(g.Slots) {
			t.Fatalf("%s: plan=%d slots=%d", g.Tag, len(meta.Plan), len(g.Slots))
		}
		for _, slot := range g.Slots {
			m := DeriveSlotMatchMeta(slot)
			if _, ok := known[m.MatchShape]; !ok {
				t.Fatalf("%s/%s unknown shape %q role=%s hint=%q", g.Tag, slot.ID, m.MatchShape, slot.Role, slot.MatchHint)
			}
			if len(m.SeparationTags) == 0 || len(m.InterchangeTags) == 0 {
				t.Fatalf("%s/%s empty tags: %+v", g.Tag, slot.ID, m)
			}
			for _, tag := range m.SeparationTags {
				if !slices.Contains(MatchTagVocab, tag) {
					t.Fatalf("%s/%s separation tag %q not in vocab", g.Tag, slot.ID, tag)
				}
			}
		}
		// Plan priorities non-decreasing
		for i := 1; i < len(meta.Plan); i++ {
			if meta.Plan[i].MatchPriority < meta.Plan[i-1].MatchPriority {
				t.Fatalf("%s plan not sorted: %+v", g.Tag, meta.Plan)
			}
		}
	}
}

func TestDeriveSlotMatchMetaTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		slot       Slot
		wantShape  string
		wantSep    []string
		wantInter  []string
		wantPrioLo int // priority band check
		wantPrioHi int
	}{
		{
			name: "reality_sni",
			slot: Slot{ID: "r", Role: RoleTCPReality, MatchHint: "sni_pool"},
			wantShape: "tls.sni",
			wantSep:   []string{"tcp", "tls", "reality", "sni"},
			wantInter: []string{"tcp_reality", "tls_clienthello"},
			wantPrioLo: 100, wantPrioHi: 100,
		},
		{
			name: "tls_sni",
			slot: Slot{ID: "t", Role: RoleTCPTLS, MatchHint: "sni_pool"},
			wantShape: "tls.sni",
			wantSep:   []string{"tcp", "tls", "sni"},
			wantInter: []string{"tcp_tls", "tls_clienthello"},
			wantPrioLo: 100, wantPrioHi: 100,
		},
		{
			name: "tls_alpn",
			slot: Slot{ID: "a", Role: RoleTCPTLS, MatchHint: "alpn", PreferredALPN: []string{"h2"}},
			wantShape: "tls.alpn",
			wantSep:   []string{"tcp", "tls", "alpn"},
			wantInter: []string{"tcp_tls", "tls_alpn"},
			wantPrioLo: 50, wantPrioHi: 50,
		},
		{
			name: "quic_protocol",
			slot: Slot{ID: "q", Role: RoleQUIC, MatchHint: "protocol_only"},
			wantShape: "protocol.quic",
			wantSep:   []string{"udp", "quic"},
			wantInter: []string{"quic", "udp"},
			wantPrioLo: 200, wantPrioHi: 200,
		},
		{
			name: "quic_sni",
			slot: Slot{ID: "qs", Role: RoleQUIC, MatchHint: "sni_pool"},
			wantShape: "protocol.quic+sni",
			wantSep:   []string{"udp", "quic", "sni"},
			wantInter: []string{"quic", "quic_sni"},
			wantPrioLo: 100, wantPrioHi: 100,
		},
		{
			name: "plain",
			slot: Slot{ID: "p", Role: RoleTCPPlain, MatchHint: "always_plain"},
			wantShape: "always",
			wantSep:   []string{"tcp", "plain", "catch_all"},
			wantInter: []string{"tcp_plain"},
			wantPrioLo: 300, wantPrioHi: 300,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := DeriveSlotMatchMeta(tc.slot)
			if m.MatchShape != tc.wantShape {
				t.Fatalf("shape=%q want %q", m.MatchShape, tc.wantShape)
			}
			if !slices.Equal(m.SeparationTags, tc.wantSep) {
				t.Fatalf("sep=%v want %v", m.SeparationTags, tc.wantSep)
			}
			if !slices.Equal(m.InterchangeTags, tc.wantInter) {
				t.Fatalf("inter=%v want %v", m.InterchangeTags, tc.wantInter)
			}
			if m.MatchPriority < tc.wantPrioLo || m.MatchPriority > tc.wantPrioHi {
				t.Fatalf("prio=%d want [%d,%d]", m.MatchPriority, tc.wantPrioLo, tc.wantPrioHi)
			}
		})
	}
}

func TestGroupMatchPlanOrderDual(t *testing.T) {
	t.Parallel()
	g, err := Get("dg_443_dual")
	if err != nil {
		t.Fatal(err)
	}
	meta := BuildGroupMatchMeta(g)
	if len(meta.Plan) != 2 {
		t.Fatalf("plan=%d", len(meta.Plan))
	}
	// Reality SNI before protocol QUIC
	if meta.Plan[0].MatchPriority >= meta.Plan[1].MatchPriority && meta.Plan[0].MatchShape != "tls.sni" {
		t.Fatalf("expected tls.sni before quic, plan=%+v", meta.Plan)
	}
	if meta.Plan[0].MatchShape != "tls.sni" {
		t.Fatalf("first=%s", meta.Plan[0].MatchShape)
	}
	if meta.Plan[1].MatchShape != "protocol.quic" {
		t.Fatalf("second=%s", meta.Plan[1].MatchShape)
	}
	for _, want := range []string{"tcp", "tls", "quic", "udp", "sni", "reality"} {
		if !slices.Contains(meta.SeparationSummary, want) {
			t.Fatalf("summary missing %q: %v", want, meta.SeparationSummary)
		}
	}
}

func TestGroupMatchPlanTriple(t *testing.T) {
	t.Parallel()
	g, err := Get("dg_443_triple")
	if err != nil {
		t.Fatal(err)
	}
	meta := BuildGroupMatchMeta(g)
	if len(meta.Plan) != 3 {
		t.Fatalf("plan=%d", len(meta.Plan))
	}
	shapes := map[string]int{}
	for _, step := range meta.Plan {
		shapes[step.MatchShape]++
	}
	if shapes["tls.sni"] != 2 || shapes["protocol.quic"] != 1 {
		t.Fatalf("shapes=%v plan=%+v", shapes, meta.Plan)
	}
	// QUIC must be after both TLS SNI slots
	if meta.Plan[2].MatchShape != "protocol.quic" {
		t.Fatalf("last should be quic: %+v", meta.Plan)
	}
}

func TestGroupMatchPlanALPNSplit(t *testing.T) {
	t.Parallel()
	g, err := Get("dg_443_alpn_split")
	if err != nil {
		t.Fatal(err)
	}
	meta := BuildGroupMatchMeta(g)
	// Catalog uses sni_pool + PreferredALPN (inbound hint); demux match is still tls.sni.
	for _, step := range meta.Plan {
		if step.SlotID == "h2" || step.SlotID == "h1" {
			if step.MatchShape != "tls.sni" {
				t.Fatalf("slot %s shape=%s want tls.sni", step.SlotID, step.MatchShape)
			}
		}
	}
	for _, slot := range g.Slots {
		if slot.ID == "h2" && len(slot.PreferredALPN) == 0 {
			t.Fatal("h2 should expose preferred_alpn")
		}
	}
}

func TestGroupMatchPlanPlainCatchAll(t *testing.T) {
	t.Parallel()
	g, err := Get("dg_443_plain_tls")
	if err != nil {
		t.Fatal(err)
	}
	meta := BuildGroupMatchMeta(g)
	if len(meta.Plan) < 2 {
		t.Fatalf("plan=%+v", meta.Plan)
	}
	last := meta.Plan[len(meta.Plan)-1]
	// plain catch-all last
	hasPlain := false
	for _, step := range meta.Plan {
		if step.MatchShape == "always" {
			hasPlain = true
			if step.MatchPriority < 300 {
				t.Fatalf("plain prio too early: %d", step.MatchPriority)
			}
		}
	}
	if !hasPlain {
		t.Fatalf("expected always catch-all, last=%+v plan=%+v", last, meta.Plan)
	}
}

func TestFitsInterchange(t *testing.T) {
	t.Parallel()
	reality := Slot{Role: RoleTCPReality, MatchHint: "sni_pool"}
	if !FitsInterchange(reality, []string{"tcp", "tls", "reality"}, "tls_clienthello") {
		t.Fatal("reality should fit")
	}
	if FitsInterchange(reality, []string{"tcp", "tls"}, "tls_clienthello") {
		t.Fatal("tls without reality must not fit reality slot")
	}
	quic := Slot{Role: RoleQUIC, MatchHint: "protocol_only"}
	if !FitsInterchange(quic, []string{"udp", "quic", "tls"}, "quic") {
		t.Fatal("hy2-like should fit quic")
	}
	plain := Slot{Role: RoleTCPPlain, MatchHint: "always_plain"}
	if FitsInterchange(plain, []string{"tcp", "tls"}, "") {
		t.Fatal("tls must not fit plain")
	}
}

func TestEnrichSlotAPI(t *testing.T) {
	t.Parallel()
	item := EnrichSlotAPI(Slot{
		ID: "tls", Role: RoleTCPTLS, DefaultPreset: "anytls",
		Substitutes: []string{"anytls", "vless_tls"}, MatchHint: "sni_pool",
	})
	if item["match_shape"] != "tls.sni" {
		t.Fatalf("%v", item)
	}
	tags, _ := item["separation_tags"].([]string)
	if !slices.Contains(tags, "sni") {
		t.Fatalf("tags=%v", tags)
	}
}

func TestSubstitutionsIncludesMatchMeta(t *testing.T) {
	t.Parallel()
	view, err := Substitutions("dg_443_triple")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Slots) != 3 {
		t.Fatalf("slots=%d", len(view.Slots))
	}
	for _, s := range view.Slots {
		if s.MatchShape == "" || len(s.SeparationTags) == 0 || len(s.InterchangeTags) == 0 {
			t.Fatalf("missing meta: %+v", s)
		}
		foundDefault := false
		for _, opt := range s.Options {
			if opt.Tag == s.DefaultPreset {
				foundDefault = true
				if !opt.FitsInterchange {
					t.Fatalf("default preset must fit: slot=%s opt=%s", s.ID, opt.Tag)
				}
			}
		}
		if !foundDefault {
			t.Fatalf("default %s missing in options", s.DefaultPreset)
		}
	}
}

//go:build with_controlplane

package smoke

import (
	"encoding/json"
	"testing"
)

func TestRewriteServersToHairpin(t *testing.T) {
	t.Parallel()
	obs := []any{
		map[string]any{
			"tag":    "cp-out-v1-vless-tcp-none",
			"type":   "vless",
			"server": "203.0.113.10",
			"tls":    map[string]any{"server_name": "vpn.example.com", "insecure": true},
		},
	}
	if err := RewriteServersToHairpin(obs); err != nil {
		t.Fatal(err)
	}
	ob := obs[0].(map[string]any)
	if ob["server"] != HairpinLocalHost {
		t.Fatalf("server=%v", ob["server"])
	}
	tls := ob["tls"].(map[string]any)
	if tls["server_name"] != "vpn.example.com" {
		t.Fatalf("server_name rewritten: %v", tls["server_name"])
	}
}

func TestExtractAndClone(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"outbounds":[{"tag":"a","server":"x"}],"meta":{"matched":1}}`)
	obs, err := ExtractOutbounds(raw)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := CloneOutbounds(obs)
	if err != nil {
		t.Fatal(err)
	}
	cp[0].(map[string]any)["server"] = "y"
	if obs[0].(map[string]any)["server"] != "x" {
		t.Fatal("clone must be deep")
	}
	b, _ := json.Marshal(cp)
	if string(b) == "" {
		t.Fatal("empty")
	}
}

func TestSkipReasonCarrier(t *testing.T) {
	t.Parallel()
	// Unknown preset
	if SkipReason("no-such-preset-xyz") != "unknown_preset" {
		t.Fatalf("got %q", SkipReason("no-such-preset-xyz"))
	}
}

func TestMatchSetPreset(t *testing.T) {
	t.Parallel()
	testable := []setPreset{{Set: "v1", Preset: "vless-tcp"}, {Set: "vws", Preset: "vless_ws_tls"}}
	sp, ok := matchSetPreset("cp-out-v1-vless-tcp-none-pkt-xudp", testable)
	if !ok || sp.Set != "v1" || sp.Preset != "vless-tcp" {
		t.Fatalf("got %+v ok=%v", sp, ok)
	}
	sp, ok = matchSetPreset("cp-out-vws-vless_ws_tls-none", testable)
	if !ok || sp.Set != "vws" {
		t.Fatalf("ws got %+v ok=%v", sp, ok)
	}
}

func TestVariantProfileFromTag(t *testing.T) {
	t.Parallel()
	v, p := variantProfileFromTag("cp-out-v1-vless-tcp-none-pkt-xudp", "vless-tcp")
	if v != "none" || p != "pkt-xudp" {
		t.Fatalf("variant=%q profile=%q", v, p)
	}
	v, p = variantProfileFromTag("cp-out-vws-vless_ws_tls-none", "vless_ws_tls")
	if v != "none" || p != "" {
		t.Fatalf("variant=%q profile=%q", v, p)
	}
}

func TestPrimaryOutboundScorePrefersDefaults(t *testing.T) {
	t.Parallel()
	noneXUDP := primaryOutboundScore("vless_tcp", "none", "pkt-xudp")
	noneNone := primaryOutboundScore("vless_tcp", "none", "pkt-none")
	visionXUDP := primaryOutboundScore("vless_tcp", "xtls-rprx-vision", "pkt-xudp")
	if noneXUDP <= noneNone {
		t.Fatalf("pkt-xudp score=%d should beat pkt-none=%d", noneXUDP, noneNone)
	}
	if noneXUDP <= visionXUDP {
		t.Fatalf("flow-none score=%d should beat vision=%d", noneXUDP, visionXUDP)
	}
	muxNone := primaryOutboundScore("vless_tls_mux", "none", "pkt-xudp")
	muxVision := primaryOutboundScore("vless_tls_mux", "xtls-rprx-vision", "pkt-xudp")
	if muxNone <= muxVision {
		t.Fatalf("mux must prefer flow-none: %d vs %d", muxNone, muxVision)
	}
}

func TestRequestDefaults(t *testing.T) {
	t.Parallel()
	r := Request{}
	if r.EffectiveTimeout() != defaultTimeout {
		t.Fatalf("timeout=%v", r.EffectiveTimeout())
	}
	if len(r.EffectiveURLs()) < len(DefaultURLs) {
		t.Fatalf("urls=%v", r.EffectiveURLs())
	}
}

func TestReportSmokeFor(t *testing.T) {
	t.Parallel()
	var nilRep *Report
	if nilRep.SmokeFor("a", "b") != nil {
		t.Fatal("nil report")
	}
	lat := 10
	rep := &Report{
		FinishedAt: "2026-08-03T12:00:00Z",
		Results: []Result{
			{Set: "s1", Preset: "vless-tcp", OK: true, LatencyMs: &lat},
			{Set: "s1", Preset: "vless-tcp", OK: false}, // ignored (first wins)
			{Set: "s1", Preset: "hy2", Skipped: true, SkipReason: "inbound_only"},
		},
	}
	sm := rep.SmokeFor("s1", "vless-tcp")
	if sm == nil || !sm.OK || sm.LatencyMs == nil || *sm.LatencyMs != 10 {
		t.Fatalf("vless %+v", sm)
	}
	sk := rep.SmokeFor("s1", "hy2")
	if sk == nil || !sk.Skipped || sk.FinishedAt != rep.FinishedAt {
		t.Fatalf("hy2 %+v", sk)
	}
	if rep.SmokeFor("missing", "x") != nil {
		t.Fatal("missing")
	}
}

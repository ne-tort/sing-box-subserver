//go:build with_controlplane

package wgawg

import (
	"fmt"
	"strings"
	"testing"
)

func TestGenerateInitPacketsFromBank(t *testing.T) {
	t.Parallel()
	i1, _, _, _, _ := GenerateInitPackets()
	if strings.TrimSpace(i1) == "" {
		t.Fatal("i1 empty")
	}
	if strings.Count(i1, "<b 0x") > 8 && !strings.Contains(i1, "<r ") {
		t.Fatalf("i1 looks like per-byte tags: %q", truncate(i1, 120))
	}
}

func TestRandomizeDNS(t *testing.T) {
	t.Parallel()
	in := "<b 0xabcd01000001000000000000076578616d706c6503636f6d0000010001>"
	out := RandomizeCPS("dns", in)
	if !strings.HasPrefix(out, "<r 2>") {
		t.Fatalf("dns TXID not randomized: %s", out)
	}
	if strings.Contains(out, "<b 0xabcd") {
		t.Fatalf("static TXID leaked: %s", out)
	}
}

func TestRandomizeSTUN(t *testing.T) {
	t.Parallel()
	in := "<b 0x000100002112a44200112233445566778899aabb>"
	out := RandomizeCPS("stun", in)
	if !strings.Contains(out, "<r 12>") {
		t.Fatalf("stun TXID missing: %s", out)
	}
	if !strings.Contains(out, "2112a442") {
		t.Fatalf("stun magic cookie lost: %s", out)
	}
}

func TestRandomizeQUICKeepsCleartextHeader(t *testing.T) {
	t.Parallel()
	in := "<b 0xc900000001084e4af44433d62fcd000044d0" + strings.Repeat("ab", 616) + ">"
	out := RandomizeCPS("quic", in)
	if strings.Contains(out, "<r 1249>") {
		t.Fatalf("short-header parody: %s", truncate(out, 100))
	}
	if !strings.Contains(out, "<b 0xc90000000108>") {
		t.Fatalf("lost flags+version+dcid_len: %s", truncate(out, 160))
	}
	if !strings.Contains(out, "<r 8>") {
		t.Fatalf("dcid not randomized: %s", truncate(out, 160))
	}
	if !strings.Contains(out, "0044d0") {
		t.Fatalf("length field not preserved: %s", truncate(out, 200))
	}
}

func TestBrokenQUICVariantRejected(t *testing.T) {
	t.Parallel()
	b, err := loadSignaturesBank()
	if err != nil {
		t.Fatal(err)
	}
	vars := listVariants("quic", b.Profiles["quic"])
	if len(vars) != 1 || vars[0] != "1" {
		t.Fatalf("quic usable variants=%v want only [1]", vars)
	}
	if n := listVariants("quic_tls_browser", b.Profiles["quic_tls_browser"]); len(n) != 0 {
		t.Fatalf("quic_tls_browser should be filtered out, got %v", n)
	}
}

func TestGenerateInitPacketSingleBlob(t *testing.T) {
	t.Parallel()
	s := GenerateInitPacket(8, 8)
	if strings.Count(s, "<b ") != 1 {
		t.Fatalf("want one <b> tag, got %q", s)
	}
}

func TestBundleManualHasCPSNoSugar(t *testing.T) {
	t.Parallel()
	m, err := BundleManual(false, "dns")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(fmt.Sprint(m["i1"])) == "" {
		t.Fatal("manual bundle missing i1")
	}
	for _, k := range []string{"ip", "ib", "id"} {
		if _, ok := m[k]; ok {
			t.Fatalf("manual bundle must not set %s", k)
		}
	}
	i1 := fmt.Sprint(m["i1"])
	if !strings.HasPrefix(i1, "<r 2>") {
		t.Fatalf("expected dns TXID <r 2>, got %s", truncate(i1, 80))
	}
}

func TestBundleSugarNoISlots(t *testing.T) {
	t.Parallel()
	m, err := Bundle(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4", "ip", "ib"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
	if _, ok := m["id"]; ok {
		t.Fatal("id must be filled from Reality SNI on the client, not in Bundle")
	}
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5", "header_protection_key"} {
		if _, ok := m[k]; ok {
			t.Fatalf("unexpected %s", k)
		}
	}
}

func TestBundleAWG3HasHP(t *testing.T) {
	t.Parallel()
	m, err := Bundle(true)
	if err != nil {
		t.Fatal(err)
	}
	if m["header_protection_key"] == "" {
		t.Fatal("missing HP key")
	}
	if m["ip"] == "" || m["ib"] == "" {
		t.Fatalf("masquerade ip/ib missing: %v", m)
	}
}

func TestBundleFromExistingPreservesManual(t *testing.T) {
	t.Parallel()
	prev, err := BundleManual(false, "stun")
	if err != nil {
		t.Fatal(err)
	}
	next, err := BundleFromExisting(false, prev, "")
	if err != nil {
		t.Fatal(err)
	}
	if !HasManualCPS(next) {
		t.Fatalf("expected manual CPS: %v", next)
	}
	if _, ok := next["ip"]; ok {
		t.Fatal("sugar ip leaked into manual regenerate")
	}
}

func TestPreferredQuicUsesBankInitial(t *testing.T) {
	t.Parallel()
	m, err := BundleWith(BundleOpts{Mode: "manual", Preferred: "quic"})
	if err != nil {
		t.Fatal(err)
	}
	i1 := fmt.Sprint(m["i1"])
	if strings.Contains(i1, "<r 1249>") {
		t.Fatalf("parody short-header CPS: %s", truncate(i1, 100))
	}
	if !strings.Contains(i1, "00000001") {
		t.Fatalf("expected QUIC v1 in cleartext header: %s", truncate(i1, 120))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

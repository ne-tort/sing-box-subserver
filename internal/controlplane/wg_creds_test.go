//go:build with_controlplane

package controlplane

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/wgawg"
)

func TestEnsureWgUserCreds_StickyAndMirror(t *testing.T) {
	t.Parallel()
	s := &Service{}
	u1 := domain.User{Name: "a", Enabled: true, CreatedAt: time.Now().UTC(), Creds: map[string]map[string]any{}}
	u2 := domain.User{Name: "b", Enabled: true, CreatedAt: time.Now().UTC(), Creds: map[string]map[string]any{
		"wg_awg2": {"private_key": mustPriv(t), "public_key": "x", "wg_host_index": 5},
	}}
	out, changed, err := s.ensureWgUserCreds([]domain.User{u1, u2})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected change")
	}
	c1 := out[0].Creds["wg"]
	c2 := out[1].Creds["wg"]
	if c1["wg_host_index"] == nil || c2["wg_host_index"].(int) != 5 {
		t.Fatalf("c1=%v c2=%v", c1, c2)
	}
	if domain.HostIPOnly(fmt.Sprint(c1["address"])) != fmt.Sprint(c1["address"]) || !strings.HasPrefix(fmt.Sprint(c1["address"]), "10.8.0.") {
		t.Fatalf("expected derived host address, got %v", c1["address"])
	}
	if c2["address"] != "10.8.0.5" {
		t.Fatalf("sticky index 5 → 10.8.0.5, got %v", c2["address"])
	}
	if c1["wg_host_index"] == 5 {
		t.Fatal("new user must not steal sticky index 5")
	}
	// mirrored
	if out[1].Creds["wg_awg3"] == nil || out[1].Creds["wg"] == nil {
		t.Fatal("expected mirror keys")
	}
	// second pass: sticky preserved, no force change of index
	out2, _, err := s.ensureWgUserCreds(out)
	if err != nil {
		t.Fatal(err)
	}
	if out2[0].Creds["wg"]["wg_host_index"] != c1["wg_host_index"] {
		t.Fatal("sticky broken")
	}
	if out2[1].Creds["wg"]["wg_host_index"] != 5 {
		t.Fatal("sticky 5 broken")
	}
}

func TestEnsureWgUserCreds_StickyReservedBeforeAlloc(t *testing.T) {
	t.Parallel()
	s := &Service{}
	// Empty user first in slice; sticky claim on .2 must still win.
	u1 := domain.User{Name: "new", Enabled: true, CreatedAt: time.Now().UTC(), Creds: map[string]map[string]any{}}
	u2 := domain.User{Name: "old", Enabled: true, CreatedAt: time.Now().UTC(), Creds: map[string]map[string]any{
		"wg": {"private_key": mustPriv(t), "wg_host_index": 2},
	}}
	out, _, err := s.ensureWgUserCreds([]domain.User{u1, u2})
	if err != nil {
		t.Fatal(err)
	}
	if out[1].Creds["wg"]["wg_host_index"] != 2 {
		t.Fatal("sticky 2 lost")
	}
	if out[0].Creds["wg"]["wg_host_index"] == 2 {
		t.Fatal("allocator stole sticky 2")
	}
}

func TestEnsureWgUserCreds_DuplicateIndexRejected(t *testing.T) {
	t.Parallel()
	s := &Service{}
	u1 := domain.User{Name: "a", Enabled: true, CreatedAt: time.Now().UTC(), Creds: map[string]map[string]any{
		"wg": {"private_key": mustPriv(t), "wg_host_index": 4},
	}}
	u2 := domain.User{Name: "b", Enabled: true, CreatedAt: time.Now().UTC(), Creds: map[string]map[string]any{
		"wg_awg3": {"private_key": mustPriv(t), "wg_host_index": 4},
	}}
	_, _, err := s.ensureWgUserCreds([]domain.User{u1, u2})
	if err == nil || !strings.Contains(err.Error(), "cp_wg_peer_conflict") {
		t.Fatalf("err=%v", err)
	}
}

func mustPriv(t *testing.T) string {
	t.Helper()
	p, err := domain.RandomWireGuardPrivate()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnsureWgHubSecrets_ClearAWGOnPlain(t *testing.T) {
	t.Parallel()
	s := &Service{}
	h := domain.WgHub{Profile: domain.WgProfilePlain, AWG2: map[string]any{"jc": 1}}
	changed, err := s.ensureWgHubSecrets(&h, false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || h.AWG2 != nil {
		t.Fatalf("changed=%v awg2=%v", changed, h.AWG2)
	}
	h2 := domain.WgHub{Profile: domain.WgProfileAWG2}
	_, err = s.ensureWgHubSecrets(&h2, false)
	if err != nil {
		t.Fatal(err)
	}
	if h2.AWG2["ip"] == nil || h2.AWG2["jc"] == nil {
		t.Fatalf("%v", h2.AWG2)
	}
	if _, ok := h2.AWG2["id"]; ok {
		t.Fatal("id must come from client Reality SNI, not Bundle")
	}
	if _, ok := h2.AWG2["header_protection_key"]; ok {
		t.Fatal("awg2 must not have HP")
	}
	h3 := domain.WgHub{Profile: domain.WgProfilePathology}
	_, err = s.ensureWgHubSecrets(&h3, false)
	if err != nil {
		t.Fatal(err)
	}
	if !wgawg.PathologyHasKey(h3.Pathology) || h3.Pathology["auto"] != true {
		t.Fatalf("%v", h3.Pathology)
	}
}

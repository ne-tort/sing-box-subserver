//go:build with_traffic && with_controlplane

package cpbridge_test

import (
	"testing"

	cpdomain "github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/cpbridge"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
)

func TestDataplaneKeysForUserIncludesName(t *testing.T) {
	t.Parallel()
	u := cpdomain.User{ID: "abc", Name: "alice"}
	keys := cpbridge.DataplaneKeysForUser(u, nil)
	if len(keys) != 1 || keys[0] != "alice" {
		t.Fatalf("keys=%v", keys)
	}
	keys = cpbridge.DataplaneKeysForUser(u, []cpdomain.InboundSet{{
		Name: "s1", Presets: []string{"shadowsocks-tcp"},
	}})
	if len(keys) != 1 || keys[0] != "alice" {
		t.Fatalf("ss keys=%v", keys)
	}
}

func TestBridgeLifecycleViaModuleAPIs(t *testing.T) {
	t.Parallel()
	mod := traffic.New(traffic.Deps{DataDir: t.TempDir()})
	if mod == nil {
		t.Fatal("nil module")
	}
	u := cpdomain.User{
		ID: "u1", Name: "alice",
		SpeedUpBytesPerSec: 1000, SpeedDownBytesPerSec: 2000,
	}
	sets := []cpdomain.InboundSet{{Name: "s1", Presets: []string{"shadowsocks-tcp"}}}
	keys := cpbridge.DataplaneKeysForUser(u, sets)
	if err := mod.RegisterManifest("controlplane", []domain.Subject{{
		ID: "cp:user:u1", DataplaneKeys: keys,
	}}); err != nil {
		t.Fatal(err)
	}
	mod.SetLimits(map[string]domain.SpeedLimit{
		"alice": {UpBytesPerSec: 1000, DownBytesPerSec: 2000},
	})
	mod.Service().InjectUserTraffic("alice", 100, 50)
	if err := mod.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := mod.PollSubjectUsage("cp:user:u1").Total; got != 150 {
		t.Fatalf("usage=%d", got)
	}
	// Live bytes injected before zero must not resurrect on the next Flush.
	mod.Service().InjectUserTraffic("alice", 10, 10)
	if err := mod.ZeroSubject("cp:user:u1"); err != nil {
		t.Fatal(err)
	}
	if err := mod.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := mod.PollSubjectUsage("cp:user:u1").Total; got != 0 {
		t.Fatalf("after zero discarded live, usage=%d", got)
	}
	if err := mod.SetSubjectUsage("cp:user:u1", 77); err != nil {
		t.Fatal(err)
	}
	if got := mod.PollSubjectUsage("cp:user:u1").Total; got != 77 {
		t.Fatalf("set usage=%d", got)
	}
}

//go:build with_traffic

package service_test

import (
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/service"
)

func TestFlushAggregatesSubjectUsage(t *testing.T) {
	t.Parallel()
	svc, err := service.New(service.Config{DataDir: t.TempDir(), FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterManifest("c1", []domain.Subject{{
		ID: "cp:user:1", DataplaneKeys: []string{"alice", "alice-vision"},
	}}); err != nil {
		t.Fatal(err)
	}
	svc.InjectUserTraffic("alice", 100, 50)
	svc.InjectUserTraffic("alice-vision", 20, 30)
	if err := svc.Flush(); err != nil {
		t.Fatal(err)
	}
	u := svc.PollSubjectUsage("cp:user:1")
	if u.Total != 200 {
		t.Fatalf("total=%d want 200 (%+v)", u.Total, u)
	}
	on := svc.Onlines()
	users, _ := on["dataplane_users"].([]string)
	if len(users) == 0 {
		t.Fatalf("expected online users, got %v", on)
	}
}

func TestZeroSubjectDiscardsLiveBeforeFlush(t *testing.T) {
	t.Parallel()
	svc, err := service.New(service.Config{DataDir: t.TempDir(), FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterManifest("c1", []domain.Subject{{
		ID: "cp:user:1", DataplaneKeys: []string{"alice"},
	}}); err != nil {
		t.Fatal(err)
	}
	svc.InjectUserTraffic("alice", 1000, 2000)
	if err := svc.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := svc.PollSubjectUsage("cp:user:1").Total; got != 3000 {
		t.Fatalf("after flush total=%d", got)
	}
	// Live bytes that must not resurrect after zero.
	svc.InjectUserTraffic("alice", 500, 500)
	if err := svc.ZeroSubject("cp:user:1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := svc.PollSubjectUsage("cp:user:1").Total; got != 0 {
		t.Fatalf("after zero+flush total=%d want 0", got)
	}
}

func TestSetSubjectUsageAbsolute(t *testing.T) {
	t.Parallel()
	svc, err := service.New(service.Config{DataDir: t.TempDir(), FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterManifest("c1", []domain.Subject{{
		ID: "cp:user:1", DataplaneKeys: []string{"alice"},
	}}); err != nil {
		t.Fatal(err)
	}
	svc.InjectUserTraffic("alice", 999, 999)
	if err := svc.SetSubjectUsage("cp:user:1", 42); err != nil {
		t.Fatal(err)
	}
	if got := svc.PollSubjectUsage("cp:user:1").Total; got != 42 {
		t.Fatalf("total=%d want 42", got)
	}
	// Discarded live must not inflate on flush.
	if err := svc.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := svc.PollSubjectUsage("cp:user:1").Total; got != 42 {
		t.Fatalf("after flush total=%d want 42", got)
	}
	if err := svc.SetSubjectUsage("cp:user:1", 0); err != nil {
		t.Fatal(err)
	}
	if got := svc.PollSubjectUsage("cp:user:1").Total; got != 0 {
		t.Fatalf("reset total=%d", got)
	}
}

func TestShapingLimitsApplied(t *testing.T) {
	t.Parallel()
	svc, err := service.New(service.Config{DataDir: t.TempDir(), FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetManualLimits(map[string]domain.SpeedLimit{
		"alice": {UpBytesPerSec: 1024, DownBytesPerSec: 2048},
	})
	got := svc.CurrentLimits()
	if got["alice"].UpBytesPerSec != 1024 || got["alice"].DownBytesPerSec != 2048 {
		t.Fatalf("alice limits=%+v", got["alice"])
	}
	svc.SetManualLimits(map[string]domain.SpeedLimit{
		"carol": {UpBytesPerSec: 512, DownBytesPerSec: 512},
	})
	got = svc.CurrentLimits()
	if _, ok := got["alice"]; ok {
		t.Fatal("alice should be removed when SetManualLimits replaces map")
	}
	if got["carol"].UpBytesPerSec != 512 {
		t.Fatalf("carol=%+v", got["carol"])
	}
}

func TestCPAndManualLimitsMerge(t *testing.T) {
	t.Parallel()
	svc, err := service.New(service.Config{DataDir: t.TempDir(), FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetCPLimits(map[string]domain.SpeedLimit{
		"alice": {UpBytesPerSec: 1000, DownBytesPerSec: 1000},
		"bob":   {UpBytesPerSec: 2000, DownBytesPerSec: 2000},
	})
	svc.SetManualLimits(map[string]domain.SpeedLimit{
		"bob": {UpBytesPerSec: 50, DownBytesPerSec: 50}, // ops override
	})
	got := svc.CurrentLimits()
	if got["alice"].UpBytesPerSec != 1000 {
		t.Fatalf("alice from CP=%+v", got["alice"])
	}
	if got["bob"].UpBytesPerSec != 50 {
		t.Fatalf("bob manual should win, got %+v", got["bob"])
	}
	// CP rematerialize must not wipe manual override.
	svc.SetCPLimits(map[string]domain.SpeedLimit{
		"alice": {UpBytesPerSec: 3000, DownBytesPerSec: 3000},
		"bob":   {UpBytesPerSec: 2000, DownBytesPerSec: 2000},
	})
	got = svc.CurrentLimits()
	if got["alice"].UpBytesPerSec != 3000 {
		t.Fatalf("alice updated=%+v", got["alice"])
	}
	if got["bob"].UpBytesPerSec != 50 {
		t.Fatalf("bob manual must survive CP refresh, got %+v", got["bob"])
	}
	payload := svc.LimitsPayload()
	manual, _ := payload["manual"].(map[string]domain.SpeedLimit)
	if manual["bob"].UpBytesPerSec != 50 {
		t.Fatalf("payload manual=%v", payload["manual"])
	}
}

func TestOnlinesIncludesDownloadOnly(t *testing.T) {
	t.Parallel()
	svc, err := service.New(service.Config{DataDir: t.TempDir(), FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	svc.InjectUserTraffic("dl-only", 0, 500)
	if err := svc.Flush(); err != nil {
		t.Fatal(err)
	}
	on := svc.Onlines()
	users, _ := on["dataplane_users"].([]string)
	found := false
	for _, u := range users {
		if u == "dl-only" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dl-only online, got %v", on)
	}
}

func TestStatsQueryFiltersBySubject(t *testing.T) {
	t.Parallel()
	svc, err := service.New(service.Config{DataDir: t.TempDir(), FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterManifest("c1", []domain.Subject{
		{ID: "cp:user:1", DataplaneKeys: []string{"alice"}},
		{ID: "cp:user:2", DataplaneKeys: []string{"bob"}},
	}); err != nil {
		t.Fatal(err)
	}
	svc.InjectUserTraffic("alice", 10, 20)
	svc.InjectUserTraffic("bob", 100, 200)
	svc.InjectUserTraffic("inbound-noise", 1, 1) // not a subject key; still dataplane_user
	if err := svc.Flush(); err != nil {
		t.Fatal(err)
	}
	out := svc.StatsQuery("cp:user:1", "", "", time.Now().UTC().Add(-time.Hour))
	cum, _ := out["cumulative"].([]domain.CounterTotal)
	for _, c := range cum {
		if c.SeriesType == domain.SeriesDataplaneUser && c.Key != "alice" {
			t.Fatalf("leaked counter %+v", c)
		}
		if c.SeriesType == domain.SeriesSubject && c.Key != "cp:user:1" {
			t.Fatalf("leaked subject %+v", c)
		}
	}
	usage, _ := out["subject_usage"].(domain.Usage)
	if usage.Total != 30 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestManualLimitsExpandBareNameToVariants(t *testing.T) {
	t.Parallel()
	svc, err := service.New(service.Config{DataDir: t.TempDir(), FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterManifest("controlplane", []domain.Subject{{
		ID:            "cp:user:1",
		DataplaneKeys: []string{"alice", "alice-flow-none", "alice-flow-xtls-rprx-vision"},
		Labels:        map[string]string{"consumer": "controlplane"},
	}}); err != nil {
		t.Fatal(err)
	}
	svc.SetCPLimits(map[string]domain.SpeedLimit{
		"alice":                      {UpBytesPerSec: 1000, DownBytesPerSec: 1000},
		"alice-flow-none":            {UpBytesPerSec: 1000, DownBytesPerSec: 1000},
		"alice-flow-xtls-rprx-vision": {UpBytesPerSec: 1000, DownBytesPerSec: 1000},
	})
	svc.SetManualLimits(map[string]domain.SpeedLimit{
		"alice": {UpBytesPerSec: 4096, DownBytesPerSec: 4096},
	})
	got := svc.CurrentLimits()
	for _, k := range []string{"alice", "alice-flow-none", "alice-flow-xtls-rprx-vision"} {
		if got[k].DownBytesPerSec != 4096 {
			t.Fatalf("%s not expanded: %+v (all=%v)", k, got[k], got)
		}
	}
	// Exact variant-only put must not fan out to siblings.
	svc.SetManualLimits(map[string]domain.SpeedLimit{
		"alice-flow-none": {UpBytesPerSec: 111, DownBytesPerSec: 111},
	})
	got = svc.CurrentLimits()
	if got["alice-flow-none"].DownBytesPerSec != 111 {
		t.Fatalf("exact key=%+v", got["alice-flow-none"])
	}
	if got["alice"].DownBytesPerSec != 1000 {
		t.Fatalf("bare should fall back to CP after replace, got %+v", got["alice"])
	}
	if got["alice-flow-xtls-rprx-vision"].DownBytesPerSec != 1000 {
		t.Fatalf("sibling must stay CP, got %+v", got["alice-flow-xtls-rprx-vision"])
	}
}

func TestSetSubjectUsageUsesSubjectSeriesNotBareKey(t *testing.T) {
	t.Parallel()
	svc, err := service.New(service.Config{DataDir: t.TempDir(), FlushInterval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterManifest("c1", []domain.Subject{{
		ID: "cp:user:1", DataplaneKeys: []string{"alice", "alice-flow-none"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetSubjectUsage("cp:user:1", 42); err != nil {
		t.Fatal(err)
	}
	if got := svc.PollSubjectUsage("cp:user:1").Total; got != 42 {
		t.Fatalf("total=%d want 42", got)
	}
	cum := svc.StatsQuery("cp:user:1", "", "", time.Now().UTC().Add(-time.Hour))
	for _, c := range cum["cumulative"].([]domain.CounterTotal) {
		if c.SeriesType == domain.SeriesDataplaneUser && c.Up+c.Down != 0 {
			t.Fatalf("dataplane key should be zero after absolute set: %+v", c)
		}
	}
	svc.InjectUserTraffic("alice-flow-none", 0, 10)
	if err := svc.Flush(); err != nil {
		t.Fatal(err)
	}
	// Baseline 42 + live 10 tracked on subject series → 52; keys alone are 10.
	if got := svc.PollSubjectUsage("cp:user:1").Total; got != 52 {
		t.Fatalf("after small flush total=%d want 52 (baseline+live)", got)
	}
	svc.InjectUserTraffic("alice-flow-none", 0, 100)
	if err := svc.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := svc.PollSubjectUsage("cp:user:1").Total; got < 110 {
		t.Fatalf("total=%d want at least key sum 110", got)
	}
}

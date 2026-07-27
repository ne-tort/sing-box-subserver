package subscribe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/subscribe"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

func TestSubscribeRefreshAndCancelOnDirect(t *testing.T) {
	dir := t.TempDir()
	store, err := configstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	sup := supervisor.NewWithOptions(store, box.NewEngine(context.Background()), obs.Setup("error").Logger, &obs.Metrics{}, supervisor.Options{Probe: 50 * time.Millisecond})
	defer sup.Shutdown()

	cfgA := []byte(`{"log":{"level":"error"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct-a"}]}`)
	cfgB := []byte(`{"log":{"level":"error"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct-b"}]}`)
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("If-None-Match") != "" && hits.Load() > 1 {
			// after first successful body, allow 304 path exercised separately
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/b" {
			_, _ = w.Write(cfgB)
			return
		}
		_, _ = w.Write(cfgA)
	}))
	defer srv.Close()

	m := subscribe.New(dir, "tok", sup)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	if err := m.Subscribe(subscribe.Spec{URL: srv.URL + "/a", IntervalSec: 3600, TimeoutSec: 5}); err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	st := m.Status()
	if !st.Enabled || st.Mode != "subscribed" {
		t.Fatalf("status: %+v", st)
	}
	raw, meta, err := sup.LastGoodConfig()
	if err != nil {
		t.Fatal(err)
	}
	if meta.Source != configstore.SourceSubscribe {
		t.Fatalf("source=%s", meta.Source)
	}
	if string(raw) != string(cfgA) {
		t.Fatalf("raw=%s", raw)
	}

	// Switch URL and refresh → overwrite
	if err := m.Subscribe(subscribe.Spec{URL: srv.URL + "/b", IntervalSec: 3600}); err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	raw, _, err = sup.LastGoodConfig()
	if err != nil || string(raw) != string(cfgB) {
		t.Fatalf("expected B, got %s err=%v", raw, err)
	}

	// Direct apply cancels subscription
	direct := []byte(`{"log":{"level":"error"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct-push"}]}`)
	if _, err := sup.Apply(ctx, supervisor.ApplyRequest{Raw: direct, Source: configstore.SourcePush}); err != nil {
		t.Fatal(err)
	}
	m.CancelOnDirectConfig()
	if m.Status().Enabled {
		t.Fatal("expected unsubscribed after direct")
	}
	if err := m.Refresh(ctx); err == nil || !errors.Is(err, subscribe.ErrNotSubscribed) {
		t.Fatalf("refresh should fail when idle: %v", err)
	}
	raw, meta, err = sup.LastGoodConfig()
	if err != nil || meta.Source != configstore.SourcePush {
		t.Fatalf("direct should remain: src=%v err=%v", meta.Source, err)
	}
	if string(raw) != string(direct) {
		t.Fatal("direct overwritten")
	}

	// Persist restore: disabled must stay disabled even if YAML pull is present
	m2 := subscribe.New(dir, "tok", sup)
	yamlPull := &agentcfg.Config{}
	yamlPull.Pull.Enabled = true
	yamlPull.Pull.URL = "http://should-not-seed.example/config.json"
	if err := m2.BootstrapFromYAML(yamlPull); err != nil {
		t.Fatal(err)
	}
	if m2.Status().Enabled {
		t.Fatal("persisted should be disabled after cancel; YAML must not re-seed")
	}
	if !m2.Status().Configured {
		t.Fatal("configured flag should remain true")
	}
}

func TestSubscribeStateFile(t *testing.T) {
	dir := t.TempDir()
	store, _ := configstore.New(dir)
	sup := supervisor.New(store, box.NewEngine(context.Background()), nil, nil)
	defer sup.Shutdown()
	m := subscribe.New(dir, "", sup)
	if err := m.Subscribe(subscribe.Spec{URL: "http://example.invalid/x", IntervalSec: 30}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "subscribe-state.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

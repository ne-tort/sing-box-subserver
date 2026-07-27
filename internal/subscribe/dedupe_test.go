package subscribe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/subscribe"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

func TestSubscribeSameBodyIsNoop(t *testing.T) {
	dir := t.TempDir()
	store, err := configstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	sup := supervisor.NewWithOptions(store, box.NewEngine(context.Background()), obs.Setup("error").Logger, &obs.Metrics{}, supervisor.Options{Probe: 20 * time.Millisecond})
	defer sup.Shutdown()

	cfg := []byte(`{"log":{"level":"error"},"inbounds":[],"outbounds":[{"type":"direct","tag":"d"}]}`)
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		// Ignore If-None-Match intentionally — client must still dedupe by SHA.
		_, _ = w.Write(cfg)
	}))
	defer srv.Close()

	m := subscribe.New(dir, "tok", sup)
	if err := m.Subscribe(subscribe.Spec{URL: srv.URL, IntervalSec: 3600, TimeoutSec: 5}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	rev1 := sup.Status().Revision
	if err := m.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	rev2 := sup.Status().Revision
	if rev2 != rev1 {
		t.Fatalf("revision should not bump on identical body: %d -> %d", rev1, rev2)
	}
	if !m.Status().LastNoop {
		t.Fatal("expected last_noop after identical refresh")
	}
	if hits.Load() < 2 {
		t.Fatal("expected two HTTP fetches")
	}
}

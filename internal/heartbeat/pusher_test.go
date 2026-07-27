package heartbeat_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/heartbeat"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

func TestHeartbeatYAMLNotReseedAfterDisable(t *testing.T) {
	dir := t.TempDir()
	store, _ := configstore.New(dir)
	sup := supervisor.New(store, box.NewEngine(context.Background()), nil, nil)
	defer sup.Shutdown()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	}))
	defer srv.Close()

	p := heartbeat.New(dir, "n1", "127.0.0.1:1", "tok", sup)
	if err := p.Configure(heartbeat.Spec{URL: srv.URL, IntervalSec: 3600}, true); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Run(ctx)
	time.Sleep(50 * time.Millisecond)
	if err := p.Disable(); err != nil {
		t.Fatal(err)
	}

	p2 := heartbeat.New(dir, "n1", "127.0.0.1:1", "tok", sup)
	yamlHB := &agentcfg.Config{}
	yamlHB.Heartbeat.Enabled = true
	yamlHB.Heartbeat.URL = srv.URL + "/yaml-should-not-win"
	if err := p2.BootstrapFromYAML(yamlHB); err != nil {
		t.Fatal(err)
	}
	if p2.Status().Enabled {
		t.Fatal("YAML must not re-enable after REST disable")
	}
}

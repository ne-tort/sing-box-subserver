//go:build with_traffic && with_controlplane

package cpbridge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"github.com/ne-tort/sing-box-subserver/internal/testutil"
	"github.com/ne-tort/sing-box-subserver/internal/traffic"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/cpbridge"
)

func TestAttachFlushSyncRematerialize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := &agentcfg.Config{
		NodeID: "n1", Token: "secret", Listen: "127.0.0.1:8080", DataDir: dir,
		Controlplane: agentcfg.ControlplaneConfig{PublicHost: "10.0.0.1", ExpiryTickSec: 60},
	}
	store, err := configstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	o := obs.Setup("error")
	sup := supervisor.NewWithOptions(store, &testutil.FakeEngine{}, o.Logger, o.Metrics, supervisor.Options{Probe: 0})
	owner, err := configowner.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cp := controlplane.New(controlplane.Deps{Cfg: cfg, DataDir: dir, Supervisor: sup, Owner: owner, Logger: o.Logger})
	mod := traffic.New(traffic.Deps{DataDir: dir, FlushInterval: time.Hour, Logger: o.Logger})
	b := cpbridge.Attach(cp, mod, o.Logger)
	if b == nil {
		t.Fatal("nil bridge")
	}

	mux := http.NewServeMux()
	cp.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return http.HandlerFunc(next)
	})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(
		`{"name":"alice","traffic_limit_bytes":1000}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	id, _ := env.Data["id"].(string)

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets", bytes.NewReader([]byte(
		`{"name":"s1","listen":"0.0.0.0","listen_port":8443,"presets":["shadowsocks-tcp"]}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("set: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets/s1/activate", nil))
	if rr.Code != 200 {
		t.Fatalf("activate: %d %s", rr.Code, rr.Body.String())
	}

	mod.Service().InjectUserTraffic("alice", 700, 400) // 1100 > 1000
	b.SyncNow(context.Background())

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users/"+id, nil))
	if rr.Code != 200 {
		t.Fatalf("get user: %d %s", rr.Code, rr.Body.String())
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	used, _ := env.Data["traffic_used_bytes"].(float64)
	if used < 1100 {
		t.Fatalf("used=%v after SyncNow", used)
	}
	raw, _, err := store.ReadLastGood()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"name":"alice"`)) || bytes.Contains(raw, []byte(`"name": "alice"`)) {
		t.Fatal("alice should be omitted after quota via bridge SyncNow")
	}
}

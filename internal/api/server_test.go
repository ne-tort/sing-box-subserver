package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"github.com/ne-tort/sing-box-subserver/internal/testutil"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	cfg := &agentcfg.Config{
		NodeID: "edge-1",
		Token:  "secret",
		Listen: "127.0.0.1:0",
	}
	hp := true
	cfg.HealthPublic = &hp
	dir := t.TempDir()
	store, err := configstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	o := obs.Setup("error")
	sup := supervisor.NewWithOptions(store, &testutil.FakeEngine{}, o.Logger, o.Metrics, supervisor.Options{Probe: 0})
	s := New(cfg, sup, o)
	owner, err := configowner.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Owner = owner
	return s
}

func TestHealthPublicAndAuth(t *testing.T) {
	t.Parallel()
	s := testServer(t)

	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if rr.Code != 200 {
		t.Fatalf("health: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if rr.Code != 401 {
		t.Fatalf("status without auth: %d", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status with auth: %d body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if env.Data["listen"] != "127.0.0.1:0" {
		t.Fatalf("listen missing: %+v", env.Data)
	}
}

func TestPutConfig(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	body := []byte(`{"inbounds":[],"outbounds":[{"type":"direct","tag":"d"}]}`)
	req := httptest.NewRequest(http.MethodPut, "/v1/config", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("put: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Revision uint64 `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil || !env.OK || env.Data.Revision != 1 {
		t.Fatalf("envelope: %+v err=%v body=%s", env, err, rr.Body.String())
	}
}

func TestValidateClashRejected(t *testing.T) {
	t.Parallel()
	cfg := &agentcfg.Config{NodeID: "e", Token: "secret", Listen: "127.0.0.1:0"}
	hp := true
	cfg.HealthPublic = &hp
	store, err := configstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o := obs.Setup("error")
	sup := supervisor.NewWithOptions(store, box.NewEngine(context.Background()), o.Logger, o.Metrics, supervisor.Options{Probe: 0})
	s := New(cfg, sup, o)
	body := []byte(`{"experimental":{"clash_api":{"external_controller":"127.0.0.1:9090"}},"outbounds":[{"type":"direct","tag":"d"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d %s", rr.Code, rr.Body.String())
	}
}

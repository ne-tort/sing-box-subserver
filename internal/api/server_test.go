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
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

type apiFakeEngine struct{}

func (apiFakeEngine) Validate(ctx context.Context, raw []byte) error { return nil }
func (apiFakeEngine) Start(ctx context.Context, raw []byte) (box.Instance, error) {
	return &apiFakeInst{done: make(chan struct{})}, nil
}

type apiFakeInst struct{ done chan struct{} }

func (i *apiFakeInst) Close() error {
	select {
	case <-i.done:
	default:
		close(i.done)
	}
	return nil
}
func (i *apiFakeInst) Done() <-chan struct{} { return i.done }

func testServer(t *testing.T) *Server {
	t.Helper()
	cfg := &agentcfg.Config{
		NodeID: "edge-1",
		Token:  "secret",
		Listen: "127.0.0.1:0",
	}
	hp := true
	cfg.HealthPublic = &hp
	store, err := configstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o := obs.Setup("error")
	sup := supervisor.New(store, apiFakeEngine{}, o.Logger, o.Metrics)
	return New(cfg, sup, o)
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

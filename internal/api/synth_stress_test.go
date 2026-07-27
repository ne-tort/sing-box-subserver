package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/api"
	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

func TestAPI_ConcurrentStatusAndValidate(t *testing.T) {
	dir := t.TempDir()
	store, err := configstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	o := obs.Setup("error")
	sup := supervisor.NewWithOptions(store, box.NewEngine(context.Background()), o.Logger, o.Metrics, supervisor.Options{
		Probe: 50 * time.Millisecond,
	})
	defer sup.Shutdown()

	cfg := &agentcfg.Config{
		NodeID: "synth-1",
		Token:  "tok",
		Listen: "127.0.0.1:0",
	}
	srv := api.New(cfg, sup, o)
	h := srv.Handler()

	good := []byte(`{"log":{"level":"error"},"inbounds":[],"outbounds":[{"type":"direct","tag":"direct"}]}`)
	badClash := []byte(`{"experimental":{"clash_api":{"external_controller":"127.0.0.1:9"}},"outbounds":[{"type":"direct","tag":"d"}]}`)

	var okN, unauthorizedN, rejectN atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				switch (i + j) % 4 {
				case 0:
					req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
					rr := httptest.NewRecorder()
					h.ServeHTTP(rr, req)
					if rr.Code == 200 {
						okN.Add(1)
					}
				case 1:
					req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
					rr := httptest.NewRecorder()
					h.ServeHTTP(rr, req)
					if rr.Code == 401 {
						unauthorizedN.Add(1)
					}
				case 2:
					req := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader(good))
					req.Header.Set("Authorization", "Bearer tok")
					req.Header.Set("Content-Type", "application/json")
					rr := httptest.NewRecorder()
					h.ServeHTTP(rr, req)
					if rr.Code == 200 {
						okN.Add(1)
					}
				default:
					req := httptest.NewRequest(http.MethodPost, "/v1/validate", bytes.NewReader(badClash))
					req.Header.Set("Authorization", "Bearer tok")
					req.Header.Set("Content-Type", "application/json")
					rr := httptest.NewRecorder()
					h.ServeHTTP(rr, req)
					if rr.Code == 422 {
						rejectN.Add(1)
					}
				}
			}
		}(i)
	}
	wg.Wait()
	if okN.Load() == 0 || unauthorizedN.Load() == 0 || rejectN.Load() == 0 {
		t.Fatalf("expected mixed outcomes, ok=%d unauth=%d reject=%d", okN.Load(), unauthorizedN.Load(), rejectN.Load())
	}
}

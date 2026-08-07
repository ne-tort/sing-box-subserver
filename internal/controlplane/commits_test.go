//go:build with_controlplane

package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"github.com/ne-tort/sing-box-subserver/internal/testutil"
)

func TestBlockCommitsAcceptAndApply(t *testing.T) {
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
	svc := New(Deps{Cfg: cfg, DataDir: dir, Supervisor: sup, Owner: owner, Logger: o.Logger})
	if svc == nil {
		t.Fatal("nil service")
	}
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return http.HandlerFunc(next)
	})

	// Seed reality via commit.
	body := map[string]any{
		"source": "client",
		"blocks": map[string]any{
			"reality": map[string]any{
				"body": map[string]any{
					"profiles": []any{
						map[string]any{"sni": "www.apple.com"},
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(body)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/commits", bytes.NewReader(raw)))
	if rr.Code != 202 {
		t.Fatalf("post commit: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatal("missing commit id")
	}

	deadline := time.Now().Add(5 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		rr = httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/commits/"+id, nil))
		if rr.Code != 200 {
			t.Fatalf("get commit: %d %s", rr.Code, rr.Body.String())
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &env)
		status, _ = env.Data["status"].(string)
		if status == string(domain.CommitApplied) || status == string(domain.CommitFailed) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != string(domain.CommitApplied) && status != string(domain.CommitFailed) {
		t.Fatalf("commit stuck status=%s", status)
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/heads", nil))
	if rr.Code != 200 {
		t.Fatalf("heads: %d %s", rr.Code, rr.Body.String())
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	blocks, _ := env.Data["blocks"].(map[string]any)
	if _, ok := blocks["reality"]; !ok {
		t.Fatalf("expected reality head: %#v", env.Data)
	}

	// Conflict: wrong materialize base when heads already have materialize sha after apply.
	// Second commit while pending should 409 — force pending artificially.
	meta, _ := svc.store.LoadCommitMeta()
	meta.PendingID = "c_busy"
	_ = svc.store.SaveCommitMeta(meta)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/commits", bytes.NewReader(raw)))
	if rr.Code != 409 {
		t.Fatalf("want apply_in_progress 409, got %d %s", rr.Code, rr.Body.String())
	}
	meta.PendingID = ""
	_ = svc.store.SaveCommitMeta(meta)

	// Bad base sha → conflict
	conflictBody := map[string]any{
		"base": map[string]any{
			"blocks": map[string]any{"reality": "deadbeef"},
		},
		"blocks": map[string]any{
			"reality": map[string]any{
				"body": map[string]any{
					"profiles": []any{map[string]any{"sni": "www.cloudflare.com"}},
				},
			},
		},
	}
	raw, _ = json.Marshal(conflictBody)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/commits", bytes.NewReader(raw)))
	if rr.Code != 409 {
		t.Fatalf("want commit_conflict 409, got %d %s", rr.Code, rr.Body.String())
	}
}

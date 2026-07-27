package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/api"
	"github.com/ne-tort/sing-box-subserver/internal/auth"
	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

func TestAuthCRUDAPI(t *testing.T) {
	dir := t.TempDir()
	store, _ := configstore.New(dir)
	sup := supervisor.NewWithOptions(store, box.NewEngine(context.Background()), obs.Setup("error").Logger, &obs.Metrics{}, supervisor.Options{Probe: 20 * time.Millisecond})
	defer sup.Shutdown()
	cfg := &agentcfg.Config{NodeID: "n1", Token: "bootstrap-secret-16+", Listen: "127.0.0.1:0"}
	creds, err := auth.Open(dir, cfg.Token)
	if err != nil {
		t.Fatal(err)
	}
	srv := api.New(cfg, sup, obs.Setup("error"))
	srv.Auth = creds
	h := srv.Handler()

	// 401 without token
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/auth/tokens", nil))
	if rr.Code != 401 {
		t.Fatalf("want 401 got %d", rr.Code)
	}

	authHdr := func(tok string, req *http.Request) *http.Request {
		req.Header.Set("Authorization", "Bearer "+tok)
		return req
	}

	// list with bootstrap
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, authHdr(cfg.Token, httptest.NewRequest(http.MethodGet, "/v1/auth/tokens", nil)))
	if rr.Code != 200 {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}

	// create managed
	rr = httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"panel"}`)
	h.ServeHTTP(rr, authHdr(cfg.Token, httptest.NewRequest(http.MethodPost, "/v1/auth/tokens", body)))
	if rr.Code != 200 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created struct {
		OK   bool `json:"ok"`
		Data struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil || !created.OK || created.Data.Token == "" {
		t.Fatalf("parse create: %s", rr.Body.String())
	}
	panelTok := created.Data.Token
	id := created.Data.ID

	// managed works
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, authHdr(panelTok, httptest.NewRequest(http.MethodGet, "/v1/status", nil)))
	if rr.Code != 200 {
		t.Fatalf("status with panel: %d", rr.Code)
	}

	// disable bootstrap
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, authHdr(panelTok, httptest.NewRequest(http.MethodPost, "/v1/auth/bootstrap/disable", nil)))
	if rr.Code != 200 {
		t.Fatalf("disable: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, authHdr(cfg.Token, httptest.NewRequest(http.MethodGet, "/v1/status", nil)))
	if rr.Code != 401 {
		t.Fatalf("bootstrap should 401, got %d", rr.Code)
	}

	// rotate revoke others
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, authHdr(panelTok, httptest.NewRequest(http.MethodPost, "/v1/auth/rotate", bytes.NewBufferString(`{"name":"panel","revoke_others":true}`))))
	if rr.Code != 200 {
		t.Fatalf("rotate: %d %s", rr.Code, rr.Body.String())
	}
	var rot struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &rot)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, authHdr(panelTok, httptest.NewRequest(http.MethodGet, "/v1/status", nil)))
	if rr.Code != 401 {
		t.Fatalf("old panel token should die: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, authHdr(rot.Data.Token, httptest.NewRequest(http.MethodGet, "/v1/status", nil)))
	if rr.Code != 200 {
		t.Fatalf("new token: %d", rr.Code)
	}

	// cannot delete last
	rr = httptest.NewRecorder()
	// get id from list
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, authHdr(rot.Data.Token, httptest.NewRequest(http.MethodGet, "/v1/auth/tokens", nil)))
	var listed struct {
		Data struct {
			Tokens []struct {
				ID string `json:"id"`
			} `json:"tokens"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &listed)
	if len(listed.Data.Tokens) == 0 {
		t.Fatal("no tokens")
	}
	lastID := listed.Data.Tokens[0].ID
	_ = id
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, authHdr(rot.Data.Token, httptest.NewRequest(http.MethodDelete, "/v1/auth/tokens/"+lastID, nil)))
	if rr.Code != 409 {
		t.Fatalf("want 409 on last delete, got %d %s", rr.Code, rr.Body.String())
	}
}

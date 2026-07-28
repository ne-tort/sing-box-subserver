//go:build with_controlplane

package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"github.com/ne-tort/sing-box-subserver/internal/testutil"
)

func TestUsersCRUDAndSub(t *testing.T) {
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

	body := []byte(`{"name":"alice"}`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader(body)))
	if rr.Code != 200 {
		t.Fatalf("create user: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	tok, _ := env.Data["sub_token"].(string)
	if tok == "" {
		t.Fatal("no sub_token")
	}

	setBody := []byte(`{"name":"s1","listen":"0.0.0.0","listen_port":8443,"presets":["shadowsocks-tcp"]}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets", bytes.NewReader(setBody)))
	if rr.Code != 200 {
		t.Fatalf("create set: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets/s1/activate", nil))
	if rr.Code != 200 {
		t.Fatalf("activate: %d %s", rr.Code, rr.Body.String())
	}
	if owner.Owner() != configowner.ModeControlplane {
		t.Fatalf("mode=%s", owner.Owner())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sub/"+tok, nil))
	if rr.Code != 200 {
		t.Fatalf("sub: %d %s", rr.Code, rr.Body.String())
	}
	var sub map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &sub); err != nil {
		t.Fatal(err)
	}
	outs, _ := sub["outbounds"].([]any)
	if len(outs) < 1 {
		t.Fatalf("no outbounds: %s", rr.Body.String())
	}
}

func TestTLSProfileAPI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := &agentcfg.Config{
		NodeID: "n1", Token: "secret", Listen: "127.0.0.1:8080", DataDir: dir,
		Controlplane: agentcfg.ControlplaneConfig{PublicHost: "203.0.113.10", ExpiryTickSec: 60},
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
	svc.Bootstrap(nil)
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return http.HandlerFunc(next)
	})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/tls", nil))
	if rr.Code != 200 {
		t.Fatalf("GET tls: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data["mode"] != "self_signed" {
		t.Fatalf("mode=%v", env.Data["mode"])
	}
	ms, _ := env.Data["material_status"].(map[string]any)
	if ms["self_signed_cert_present"] != true {
		t.Fatalf("material_status=%v", ms)
	}

	put := []byte(`{
		"mode":"acme_domain",
		"acme":{"email":"admin@example.com","domains":["vpn.example.com"],"provider":"letsencrypt"}
	}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/controlplane/tls", bytes.NewReader(put)))
	if rr.Code != 200 {
		t.Fatalf("PUT tls: %d %s", rr.Code, rr.Body.String())
	}

	bad := []byte(`{
		"mode":"acme_ip",
		"acme":{"email":"admin@example.com","domains":["203.0.113.10"],"provider":"zerossl"}
	}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/controlplane/tls", bytes.NewReader(bad)))
	if rr.Code != 400 {
		t.Fatalf("expected 400 for zerossl+ip, got %d %s", rr.Code, rr.Body.String())
	}

	// Restore self_signed then regenerate.
	ss := []byte(`{
		"mode":"self_signed",
		"self_signed":{
			"common_name":"203.0.113.10",
			"dns_sans":["localhost"],
			"ip_sans":["203.0.113.10"],
			"key_type":"p256",
			"valid_days":3650
		}
	}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/controlplane/tls", bytes.NewReader(ss)))
	if rr.Code != 200 {
		t.Fatalf("PUT self_signed: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/tls/regenerate", nil))
	if rr.Code != 200 {
		t.Fatalf("regenerate: %d %s", rr.Code, rr.Body.String())
	}

	badDNS := []byte(`{
		"mode":"acme_ip",
		"acme":{"email":"admin@example.com","domains":["203.0.113.10"],"provider":"letsencrypt","dns01_challenge":{"provider":"cloudflare","api_token":"secret"}}
	}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/controlplane/tls", bytes.NewReader(badDNS)))
	if rr.Code != 400 {
		t.Fatalf("expected 400 for dns01+acme_ip, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestTLSRegenerateForcesBoxReload(t *testing.T) {
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
	eng := &testutil.FakeEngine{}
	sup := supervisor.NewWithOptions(store, eng, o.Logger, o.Metrics, supervisor.Options{Probe: 0})
	owner, err := configowner.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(Deps{Cfg: cfg, DataDir: dir, Supervisor: sup, Owner: owner, Logger: o.Logger})
	svc.Bootstrap(nil)
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return http.HandlerFunc(next)
	})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(`{"name":"bob"}`))))
	if rr.Code != 200 {
		t.Fatalf("user: %d %s", rr.Code, rr.Body.String())
	}
	var uenv struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &uenv)
	tok, _ := uenv.Data["sub_token"].(string)

	setBody := []byte(`{"name":"t1","listen":"0.0.0.0","listen_port":8443,"presets":["trojan-tcp"]}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets", bytes.NewReader(setBody)))
	if rr.Code != 200 {
		t.Fatalf("set: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets/t1/activate", nil))
	if rr.Code != 200 {
		t.Fatalf("activate: %d %s", rr.Code, rr.Body.String())
	}
	startsAfterActivate := eng.Starts.Load()
	if startsAfterActivate < 1 {
		t.Fatal("activate must Apply/start box")
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sub/"+tok, nil))
	if rr.Code != 200 {
		t.Fatalf("sub: %d %s", rr.Code, rr.Body.String())
	}
	var sub map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &sub); err != nil {
		t.Fatal(err)
	}
	outs, _ := sub["outbounds"].([]any)
	if len(outs) < 1 {
		t.Fatal("no outbounds")
	}
	ob, _ := outs[0].(map[string]any)
	tlsObj, _ := ob["tls"].(map[string]any)
	if tlsObj["insecure"] != true {
		t.Fatalf("self_signed sub must set insecure: %#v", tlsObj)
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/tls/regenerate", nil))
	if rr.Code != 200 {
		t.Fatalf("regenerate: %d %s", rr.Code, rr.Body.String())
	}
	if eng.Starts.Load() != startsAfterActivate+1 {
		t.Fatalf("regenerate must Force reload: starts=%d afterActivate=%d", eng.Starts.Load(), startsAfterActivate)
	}
}

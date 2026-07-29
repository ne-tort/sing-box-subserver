//go:build with_controlplane

package controlplane

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
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

func TestManualUserCreds(t *testing.T) {
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
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return http.HandlerFunc(next)
	})

	const manualUUID = "11111111-2222-3333-4444-555555555555"
	createBody := []byte(`{
		"name":"carol",
		"creds":{
			"vless-tcp":{"uuid":"` + manualUUID + `"},
			"trojan-tcp":{"password":"operator-secret"},
			"socks":{"username":"u1","password":"p1"}
		}
	}`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader(createBody)))
	if rr.Code != 200 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatal("missing id")
	}
	creds, _ := env.Data["creds"].(map[string]any)
	vless, _ := creds["vless-tcp"].(map[string]any)
	if vless["uuid"] != manualUUID {
		t.Fatalf("vless uuid=%v want %s", vless["uuid"], manualUUID)
	}
	trojan, _ := creds["trojan-tcp"].(map[string]any)
	if trojan["password"] != "operator-secret" {
		t.Fatalf("trojan password=%v", trojan["password"])
	}
	socks, _ := creds["socks"].(map[string]any)
	if socks["username"] != "u1" || socks["password"] != "p1" {
		t.Fatalf("socks=%v", socks)
	}
	tuic, _ := creds["tuic"].(map[string]any)
	if tuic["uuid"] == nil || tuic["uuid"] == "" || tuic["password"] == nil || tuic["password"] == "" {
		t.Fatalf("tuic should be auto-filled: %v", tuic)
	}

	bad := []byte(`{"creds":{"no-such-preset":{"uuid":"x"}}}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/controlplane/users/"+id+"/creds", bytes.NewReader(bad)))
	if rr.Code != 400 {
		t.Fatalf("unknown preset: want 400 got %d %s", rr.Code, rr.Body.String())
	}
	var errEnv struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &errEnv)
	if errEnv.Error.Code != "cp_invalid_creds" {
		t.Fatalf("code=%q body=%s", errEnv.Error.Code, rr.Body.String())
	}

	put := []byte(`{"creds":{"vless-tcp":{"uuid":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}}}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/controlplane/users/"+id+"/creds", bytes.NewReader(put)))
	if rr.Code != 200 {
		t.Fatalf("put creds: %d %s", rr.Code, rr.Body.String())
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	creds, _ = env.Data["creds"].(map[string]any)
	vless, _ = creds["vless-tcp"].(map[string]any)
	if vless["uuid"] != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("after put uuid=%v", vless["uuid"])
	}
	socks, _ = creds["socks"].(map[string]any)
	if socks["username"] != "u1" {
		t.Fatalf("put must preserve other presets: socks=%v", socks)
	}
}

func TestEnsureCredsFieldLevelFill(t *testing.T) {
	t.Parallel()
	svc := &Service{}
	u := domain.User{
		Creds: map[string]map[string]any{
			"tuic": {"uuid": "11111111-2222-3333-4444-555555555555"},
		},
	}
	changed, err := svc.ensureCreds(&u)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed")
	}
	if u.Creds["tuic"]["uuid"] != "11111111-2222-3333-4444-555555555555" {
		t.Fatalf("uuid overwritten: %v", u.Creds["tuic"]["uuid"])
	}
	pw, _ := u.Creds["tuic"]["password"].(string)
	if pw == "" {
		t.Fatal("tuic password should be filled")
	}
	if u.Creds["vless-tcp"] == nil || u.Creds["vless-tcp"]["uuid"] == nil {
		t.Fatal("missing presets should be generated")
	}
	if u.Creds["vless-tcp"]["uuid_xtls"] == nil || u.Creds["vless-tcp"]["uuid_xtls"] == "" {
		t.Fatal("missing vless uuid_xtls should be generated")
	}
}

func TestUserRenameConflictAndTrafficReset(t *testing.T) {
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
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return http.HandlerFunc(next)
	})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(`{"name":"a1"}`))))
	if rr.Code != 200 {
		t.Fatalf("a1: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(`{
		"name":"a2",
		"traffic_reset_at":"2099-01-01T00:00:00Z",
		"traffic_reset_period_sec":86400
	}`))))
	if rr.Code != 200 {
		t.Fatalf("a2: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	id2, _ := env.Data["id"].(string)
	if env.Data["traffic_reset_period_sec"] != float64(86400) {
		t.Fatalf("period=%v", env.Data["traffic_reset_period_sec"])
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/v1/controlplane/users/"+id2, bytes.NewReader([]byte(`{"name":"a1"}`))))
	if rr.Code != 409 {
		t.Fatalf("rename conflict: want 409 got %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/status", nil))
	if rr.Code != 200 {
		t.Fatalf("status: %d %s", rr.Code, rr.Body.String())
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	if env.Data["demux_in_binary"] != demuxInBinary {
		t.Fatalf("demux_in_binary=%v", env.Data["demux_in_binary"])
	}
	if _, ok := env.Data["tls_material_status"]; !ok {
		// Bootstrap not called — ensureTLSProfile still creates default.
		t.Fatalf("missing tls_material_status: %v", env.Data)
	}
}

func TestScrubStaleActiveSets(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := &agentcfg.Config{
		NodeID: "n1", Token: "secret", Listen: "127.0.0.1:8080", DataDir: dir,
		Controlplane: agentcfg.ControlplaneConfig{PublicHost: "10.0.0.1"},
	}
	cs, err := configstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	o := obs.Setup("error")
	sup := supervisor.NewWithOptions(cs, &testutil.FakeEngine{}, o.Logger, o.Metrics, supervisor.Options{Probe: 0})
	owner, err := configowner.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(Deps{Cfg: cfg, DataDir: dir, Supervisor: sup, Owner: owner, Logger: o.Logger})
	if err := svc.store.SaveState(domain.State{ActiveSets: []string{"gone", "also-gone"}}); err != nil {
		t.Fatal(err)
	}
	sets, err := svc.activeSetObjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 0 {
		t.Fatalf("sets=%v", sets)
	}
	st, err := svc.store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.ActiveSets) != 0 {
		t.Fatalf("active_sets not scrubbed: %v", st.ActiveSets)
	}
}

func TestParsePresetQuery(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/v1/sub/x?preset=vless-tcp,trojan-tcp&preset=socks", nil)
	got := parsePresetQuery(r)
	if len(got) != 3 || got[0] != "vless-tcp" || got[1] != "trojan-tcp" || got[2] != "socks" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseRepeatCommaQuery(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/v1/sub/x?variant=flow-none,flow-udp-vision&variant=flow-none&tag=mobile,stable&profile=ios", nil)
	variants := parseRepeatCommaQuery(r, "variant")
	if len(variants) != 2 || variants[0] != "flow-none" || variants[1] != "flow-udp-vision" {
		t.Fatalf("variants=%v", variants)
	}
	tags := parseRepeatCommaQuery(r, "tag")
	if len(tags) != 2 || tags[0] != "mobile" || tags[1] != "stable" {
		t.Fatalf("tags=%v", tags)
	}
	profiles := parseRepeatCommaQuery(r, "profile")
	if len(profiles) != 1 || profiles[0] != "ios" {
		t.Fatalf("profiles=%v", profiles)
	}
}

func TestSubVariantTagProfileFilters(t *testing.T) {
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
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc { return http.HandlerFunc(next) })

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(`{"name":"bob"}`))))
	if rr.Code != 200 {
		t.Fatalf("create user: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	tok, _ := env.Data["sub_token"].(string)
	if tok == "" {
		t.Fatal("missing sub token")
	}

	setBody := []byte(`{
		"name":"vb",
		"listen":"0.0.0.0",
		"listen_port":9443,
		"bindings":[
			{
				"preset":"vless-tcp",
				"subscription_tags":["mobile"],
				"enabled_user_variants":["flow-none","flow-udp-vision"],
				"enabled_client_profiles":["ios"]
			}
		]
	}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets", bytes.NewReader(setBody)))
	if rr.Code != 200 {
		t.Fatalf("create set: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets/vb/activate", nil))
	if rr.Code != 200 {
		t.Fatalf("activate: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sub/"+tok+"?variant=flow-udp-vision&tag=mobile&profile=ios", nil))
	if rr.Code != 200 {
		t.Fatalf("sub filters: %d %s", rr.Code, rr.Body.String())
	}
	var sub map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &sub); err != nil {
		t.Fatal(err)
	}
	outs, _ := sub["outbounds"].([]any)
	if len(outs) != 1 {
		t.Fatalf("outbounds=%d body=%s", len(outs), rr.Body.String())
	}
	ob, _ := outs[0].(map[string]any)
	if ob["flow"] != "xtls-rprx-vision-udp443" {
		t.Fatalf("flow=%v", ob["flow"])
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sub/"+tok+"?profile=android", nil))
	if rr.Code != 200 {
		t.Fatalf("sub profile miss: %d %s", rr.Code, rr.Body.String())
	}
	sub = map[string]any{}
	if err := json.Unmarshal(rr.Body.Bytes(), &sub); err != nil {
		t.Fatal(err)
	}
	outs, _ = sub["outbounds"].([]any)
	if len(outs) != 0 {
		t.Fatalf("expected no outbounds for profile mismatch: %s", rr.Body.String())
	}
}

func TestSetSubscriptionTagsAPI(t *testing.T) {
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
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc { return http.HandlerFunc(next) })

	setBody := []byte(`{
		"name":"s-tags",
		"listen":"0.0.0.0",
		"listen_port":9443,
		"bindings":[
			{
				"preset":"vless-tcp",
				"subscription_tags":["mobile","stable"],
				"enabled_user_variants":["flow-none","flow-udp-vision"],
				"enabled_client_profiles":["ios"]
			}
		]
	}`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets", bytes.NewReader(setBody)))
	if rr.Code != 200 {
		t.Fatalf("create set: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/sets/s-tags/subscription-tags", nil))
	if rr.Code != 200 {
		t.Fatalf("tags api: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data["set"] != "s-tags" {
		t.Fatalf("set=%v", env.Data["set"])
	}
	bindings, _ := env.Data["bindings"].([]any)
	if len(bindings) != 1 {
		t.Fatalf("bindings=%v", env.Data["bindings"])
	}
	b, _ := bindings[0].(map[string]any)
	if b["inbound_tag"] != "cp-in-s-tags-vless-tcp" {
		t.Fatalf("inbound_tag=%v", b["inbound_tag"])
	}
	variants, _ := b["enabled_user_variants"].([]any)
	if len(variants) != 2 {
		t.Fatalf("variants=%v", b["enabled_user_variants"])
	}
	tags, _ := b["subscription_tags"].([]any)
	if len(tags) < 2 {
		t.Fatalf("tags=%v", b["subscription_tags"])
	}
}

func TestRealityAPIAndStickyAssignment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := &agentcfg.Config{
		NodeID: "n1", Token: "secret", Listen: "127.0.0.1:8080", DataDir: dir,
		Controlplane: agentcfg.ControlplaneConfig{PublicHost: "127.0.0.1", ExpiryTickSec: 60},
	}
	cs, err := configstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	o := obs.Setup("error")
	sup := supervisor.NewWithOptions(cs, &testutil.FakeEngine{}, o.Logger, o.Metrics, supervisor.Options{Probe: 0})
	owner, err := configowner.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(Deps{Cfg: cfg, DataDir: dir, Supervisor: sup, Owner: owner, Logger: o.Logger})
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc { return http.HandlerFunc(next) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	putReality := []byte(`{"profiles":[{"sni":"localhost","handshake_server":"localhost","handshake_port":` + strconv.Itoa(port) + `}]}`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/v1/controlplane/reality", bytes.NewReader(putReality)))
	if rr.Code != 200 {
		t.Fatalf("put reality: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(`{"name":"ruser"}`))))
	if rr.Code != 200 {
		t.Fatalf("user: %d %s", rr.Code, rr.Body.String())
	}
	setBody := []byte(`{"name":"rset","listen":"0.0.0.0","listen_port":8443,"presets":["vless-reality-tcp"]}`)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets", bytes.NewReader(setBody)))
	if rr.Code != 200 {
		t.Fatalf("set: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets/rset/activate", nil))
	if rr.Code != 200 {
		t.Fatalf("activate: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/reality", nil))
	if rr.Code != 200 {
		t.Fatalf("get reality: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	active, _ := env.Data["active_assignments"].([]any)
	if len(active) == 0 {
		t.Fatalf("expected active assignments, body=%s", rr.Body.String())
	}
	first := active[0].(map[string]any)
	shortID, _ := first["short_id"].(string)
	if shortID == "" {
		t.Fatalf("empty short_id in assignment: %v", first)
	}

	// Trigger rematerialize and ensure sticky assignment is stable.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/v1/controlplane/users/"+envUserIDFromCreate(t, mux), bytes.NewReader([]byte(`{"enabled":true}`))))
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/reality", nil))
	if rr.Code != 200 {
		t.Fatalf("get reality 2: %d %s", rr.Code, rr.Body.String())
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	active, _ = env.Data["active_assignments"].([]any)
	if len(active) == 0 {
		t.Fatal("no assignments after rematerialize")
	}
	second := active[0].(map[string]any)
	if second["short_id"] != shortID {
		t.Fatalf("sticky mismatch: before=%v after=%v", shortID, second["short_id"])
	}
}

func envUserIDFromCreate(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users", nil))
	if rr.Code != 200 {
		t.Fatalf("list users: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	for _, u := range env.Data {
		if u["name"] == "ruser" {
			if id, _ := u["id"].(string); id != "" {
				return id
			}
		}
	}
	t.Fatal("ruser id not found")
	return ""
}

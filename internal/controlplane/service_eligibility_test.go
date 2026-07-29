//go:build with_controlplane

package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

type recordingTrafficHooks struct {
	materializeN int
	resets       []string
	usedPatches  []struct {
		id   string
		used uint64
	}
	ineligible   []string
	lastUsers    []domain.User
}

func (h *recordingTrafficHooks) OnMaterialize(users []domain.User, _ []domain.InboundSet) {
	h.materializeN++
	h.lastUsers = append([]domain.User{}, users...)
}

func (h *recordingTrafficHooks) OnTrafficReset(userID string) {
	h.resets = append(h.resets, userID)
}

func (h *recordingTrafficHooks) OnTrafficUsedPatched(userID string, used uint64) {
	h.usedPatches = append(h.usedPatches, struct {
		id   string
		used uint64
	}{userID, used})
}

func (h *recordingTrafficHooks) OnBecameIneligible(userIDs []string) {
	h.ineligible = append(h.ineligible, userIDs...)
}

func setupCPTrafficTest(t *testing.T) (*Service, *http.ServeMux, *configstore.Store, *recordingTrafficHooks) {
	t.Helper()
	dir := t.TempDir()
	cfg := &agentcfg.Config{
		NodeID: "n1", Token: "secret", Listen: "127.0.0.1:8080", DataDir: dir,
		Controlplane: agentcfg.ControlplaneConfig{PublicHost: "10.0.0.1", ExpiryTickSec: 1},
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
	hooks := &recordingTrafficHooks{}
	svc.SetTrafficHooks(hooks)
	mux := http.NewServeMux()
	svc.Register(mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return http.HandlerFunc(next)
	})
	return svc, mux, store, hooks
}

func createUserAndActivateSS(t *testing.T, mux *http.ServeMux, name string) (id, token string) {
	t.Helper()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(`{"name":"`+name+`"}`))))
	if rr.Code != 200 {
		t.Fatalf("create user: %d %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &env)
	id, _ = env.Data["id"].(string)
	token, _ = env.Data["sub_token"].(string)
	if id == "" {
		t.Fatal("no id")
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets", bytes.NewReader([]byte(
		`{"name":"s1","listen":"0.0.0.0","listen_port":8443,"presets":["shadowsocks-tcp"]}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("create set: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/sets/s1/activate", nil))
	if rr.Code != 200 {
		t.Fatalf("activate: %d %s", rr.Code, rr.Body.String())
	}
	return id, token
}

func lastGoodConfigHasUser(t *testing.T, store *configstore.Store, name string) bool {
	t.Helper()
	raw, _, err := store.ReadLastGood()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(raw), `"name":"`+name+`"`) || strings.Contains(string(raw), `"name": "`+name+`"`)
}

func TestQuotaExpiryResetRematerialize(t *testing.T) {
	t.Parallel()
	svc, mux, store, hooks := setupCPTrafficTest(t)
	id, tok := createUserAndActivateSS(t, mux, "alice")
	if !lastGoodConfigHasUser(t, store, "alice") {
		t.Fatal("alice should be in config after activate")
	}
	if hooks.materializeN < 1 {
		t.Fatal("expected OnMaterialize after activate")
	}

	// Set quota and cross it via ApplyTrafficUsage.
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/v1/controlplane/users/"+id, bytes.NewReader([]byte(
		`{"traffic_limit_bytes":1000}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("patch limit: %d %s", rr.Code, rr.Body.String())
	}

	changed, err := svc.ApplyTrafficUsage(context.Background(), map[string]uint64{id: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected rematerialize when hitting quota")
	}
	if lastGoodConfigHasUser(t, store, "alice") {
		t.Fatal("alice must be omitted from config over quota")
	}
	kicked := false
	for _, kid := range hooks.ineligible {
		if kid == id {
			kicked = true
		}
	}
	if !kicked {
		t.Fatalf("expected OnBecameIneligible(%s), got %v", id, hooks.ineligible)
	}

	// Sub must be forbidden while ineligible.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/sub/"+tok, nil))
	if rr.Code != 403 {
		t.Fatalf("sub while over quota: %d", rr.Code)
	}

	// Admin PATCH used→0 should restore + call OnTrafficUsedPatched.
	beforeMat := hooks.materializeN
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/v1/controlplane/users/"+id, bytes.NewReader([]byte(
		`{"traffic_used_bytes":0}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("patch used: %d %s", rr.Code, rr.Body.String())
	}
	if len(hooks.usedPatches) == 0 || hooks.usedPatches[len(hooks.usedPatches)-1].used != 0 {
		t.Fatalf("usedPatches=%v", hooks.usedPatches)
	}
	if hooks.materializeN <= beforeMat {
		t.Fatal("expected rematerialize after restore")
	}
	if !lastGoodConfigHasUser(t, store, "alice") {
		t.Fatal("alice must return to config after used reset")
	}

	// Expiry omit.
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/v1/controlplane/users/"+id, bytes.NewReader([]byte(
		`{"expires_at":"`+past+`"}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("patch expires: %d %s", rr.Code, rr.Body.String())
	}
	if lastGoodConfigHasUser(t, store, "alice") {
		t.Fatal("alice must be omitted when expired")
	}

	// Clear expiry.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/v1/controlplane/users/"+id, bytes.NewReader([]byte(
		`{"expires_at":null}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("clear expires: %d %s", rr.Code, rr.Body.String())
	}
	if !lastGoodConfigHasUser(t, store, "alice") {
		t.Fatal("alice must return after clearing expiry")
	}

	// Periodic traffic reset restores over-quota user.
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/v1/controlplane/users/"+id, bytes.NewReader([]byte(
		`{"traffic_limit_bytes":100,"traffic_used_bytes":100}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("patch over: %d %s", rr.Code, rr.Body.String())
	}
	if lastGoodConfigHasUser(t, store, "alice") {
		t.Fatal("alice should be omitted at exact limit")
	}

	pastReset := time.Now().UTC().Add(-time.Minute)
	users, err := svc.store.LoadUsers()
	if err != nil {
		t.Fatal(err)
	}
	for i := range users {
		if users[i].ID == id {
			users[i].TrafficResetAt = &pastReset
			period := uint64(3600)
			users[i].TrafficResetPeriodSec = &period
		}
	}
	if err := svc.store.SaveUsers(users); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	ok := svc.applyTrafficResetsLocked(time.Now().UTC())
	svc.mu.Unlock()
	if !ok {
		t.Fatal("expected reset applied")
	}
	if len(hooks.resets) == 0 || hooks.resets[len(hooks.resets)-1] != id {
		t.Fatalf("resets=%v want %s", hooks.resets, id)
	}
	// Fingerprint rematerialize (same as Run tick).
	if err := svc.rematerialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !lastGoodConfigHasUser(t, store, "alice") {
		t.Fatal("alice must return after traffic reset period")
	}
}

func TestSpeedPatchPublishesTrafficPolicy(t *testing.T) {
	t.Parallel()
	_, mux, _, hooks := setupCPTrafficTest(t)
	id, _ := createUserAndActivateSS(t, mux, "bob")
	before := hooks.materializeN
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/v1/controlplane/users/"+id, bytes.NewReader([]byte(
		`{"speed_up_bytes_per_sec":1024,"speed_down_bytes_per_sec":2048}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("patch speed: %d %s", rr.Code, rr.Body.String())
	}
	if hooks.materializeN <= before {
		t.Fatal("expected OnMaterialize after speed patch")
	}
	found := false
	for _, u := range hooks.lastUsers {
		if u.ID == id && u.SpeedUpBytesPerSec == 1024 && u.SpeedDownBytesPerSec == 2048 {
			found = true
		}
	}
	if !found {
		t.Fatalf("speed not in last OnMaterialize users: %+v", hooks.lastUsers)
	}
}

func TestPublishTrafficPolicyWithoutActiveSets(t *testing.T) {
	t.Parallel()
	svc, mux, _, hooks := setupCPTrafficTest(t)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(`{"name":"carol"}`))))
	if rr.Code != 200 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	// Idle owner → rematerialize early-returns but must still publish policy.
	if hooks.materializeN < 1 {
		t.Fatal("expected publishTrafficPolicy on create without active sets")
	}
	_ = svc
}

func TestDisabledUserOmitted(t *testing.T) {
	t.Parallel()
	_, mux, store, _ := setupCPTrafficTest(t)
	id, _ := createUserAndActivateSS(t, mux, "dave")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPatch, "/v1/controlplane/users/"+id, bytes.NewReader([]byte(
		`{"enabled":false}`,
	))))
	if rr.Code != 200 {
		t.Fatalf("disable: %d %s", rr.Code, rr.Body.String())
	}
	if lastGoodConfigHasUser(t, store, "dave") {
		t.Fatal("disabled user must be omitted")
	}
}

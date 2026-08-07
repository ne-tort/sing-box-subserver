//go:build with_controlplane

package controlplane

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func newSyncTestSvc(t *testing.T) (*Service, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	cfg := &agentcfg.Config{
		NodeID: "node-a", Token: "secret", Listen: "127.0.0.1:8080", DataDir: dir,
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
	return svc, mux
}

func TestUsersSyncImportExportIdempotent(t *testing.T) {
	t.Parallel()
	_, mux := newSyncTestSvc(t)

	create := httptest.NewRecorder()
	mux.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(
		`{"name":"alice","sync_mode":"identity"}`,
	))))
	if create.Code != 200 {
		t.Fatalf("create %d %s", create.Code, create.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(create.Body.Bytes(), &created)
	data := created["data"].(map[string]any)
	syncID := data["sync_id"].(string)
	if syncID == "" {
		t.Fatal("expected sync_id")
	}

	exp := httptest.NewRecorder()
	mux.ServeHTTP(exp, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users/export?sync=1&mode=identity", nil))
	if exp.Code != 200 {
		t.Fatalf("export %d %s", exp.Code, exp.Body.String())
	}
	var expEnv map[string]any
	_ = json.Unmarshal(exp.Body.Bytes(), &expEnv)
	bundle := expEnv["data"].(map[string]any)
	if bundle["node_id"] != "node-a" {
		t.Fatalf("node_id=%v", bundle["node_id"])
	}
	users := bundle["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("users=%d", len(users))
	}

	impBody, _ := json.Marshal(map[string]any{
		"source": "client",
		"policy": map[string]any{"secrets": "identity", "on_name_conflict": "rename"},
		"users": []any{
			map[string]any{
				"sync_id":  syncID,
				"name":     "alice",
				"sync_mode": "identity",
				"revision": data["revision"],
				"enabled":  true,
			},
		},
	})
	imp := httptest.NewRecorder()
	mux.ServeHTTP(imp, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/import", bytes.NewReader(impBody)))
	if imp.Code != 200 {
		t.Fatalf("import %d %s", imp.Code, imp.Body.String())
	}
	var impEnv map[string]any
	_ = json.Unmarshal(imp.Body.Bytes(), &impEnv)
	res := impEnv["data"].(map[string]any)
	if int(res["skipped"].(float64)) != 1 {
		t.Fatalf("expected skipped=1 got %v", res)
	}
}

func TestUsersSyncMetricsNoDoubleCountAfterGlobalPush(t *testing.T) {
	t.Parallel()
	svc, mux := newSyncTestSvc(t)

	create := httptest.NewRecorder()
	mux.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(
		`{"name":"bob","sync_mode":"identity"}`,
	))))
	var created map[string]any
	_ = json.Unmarshal(create.Body.Bytes(), &created)
	data := created["data"].(map[string]any)
	syncID := data["sync_id"].(string)
	localID := data["id"].(string)

	// Simulate local dataplane ingress (bridge path).
	if _, err := svc.ApplyTrafficUsage(t.Context(), map[string]uint64{localID: 1000}); err != nil {
		t.Fatal(err)
	}
	users, _ := svc.store.LoadUsers()
	var uIdx int
	for i := range users {
		if users[i].ID == localID {
			uIdx = i
			break
		}
	}
	if users[uIdx].TrafficIngressBytes != 1000 {
		t.Fatalf("ingress=%d", users[uIdx].TrafficIngressBytes)
	}
	// Syncable: used must stay 0 until hub push.
	if users[uIdx].TrafficUsedBytes != 0 {
		t.Fatalf("used should stay 0 for syncable, got %d", users[uIdx].TrafficUsedBytes)
	}

	global := uint64(1000)
	applyBody, _ := json.Marshal(map[string]any{
		"items": []any{map[string]any{"sync_id": syncID, "global_used": global}},
	})
	apply := httptest.NewRecorder()
	mux.ServeHTTP(apply, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/sync/metrics", bytes.NewReader(applyBody)))
	if apply.Code != 200 {
		t.Fatalf("apply %d %s", apply.Code, apply.Body.String())
	}

	// Further bridge updates to same store total must not change used; ingress stays local.
	if _, err := svc.ApplyTrafficUsage(t.Context(), map[string]uint64{localID: 1000}); err != nil {
		t.Fatal(err)
	}
	users, _ = svc.store.LoadUsers()
	for i := range users {
		if users[i].ID == localID {
			if users[i].TrafficUsedBytes != 1000 {
				t.Fatalf("used after push=%d", users[i].TrafficUsedBytes)
			}
			if users[i].TrafficIngressBytes != 1000 {
				t.Fatalf("ingress after push=%d", users[i].TrafficIngressBytes)
			}
		}
	}

	// New local bytes: ingress grows; used stays until next hub push.
	if _, err := svc.ApplyTrafficUsage(t.Context(), map[string]uint64{localID: 1500}); err != nil {
		t.Fatal(err)
	}
	users, _ = svc.store.LoadUsers()
	for i := range users {
		if users[i].ID == localID {
			if users[i].TrafficIngressBytes != 1500 {
				t.Fatalf("ingress=%d", users[i].TrafficIngressBytes)
			}
			if users[i].TrafficUsedBytes != 1000 {
				t.Fatalf("used must not follow ingress for syncable, got %d", users[i].TrafficUsedBytes)
			}
		}
	}

	rep := httptest.NewRecorder()
	mux.ServeHTTP(rep, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users/sync/metrics?sync_ids="+syncID, nil))
	var repEnv map[string]any
	_ = json.Unmarshal(rep.Body.Bytes(), &repEnv)
	items := repEnv["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	item := items[0].(map[string]any)
	if item["ingress_bytes"].(float64) != 1500 || item["used_bytes"].(float64) != 1000 {
		t.Fatalf("item=%v", item)
	}
}

func TestUsersSyncSoftDeleteAndMembership(t *testing.T) {
	t.Parallel()
	_, mux := newSyncTestSvc(t)

	create := httptest.NewRecorder()
	mux.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(
		`{"name":"carol","sync_mode":"full"}`,
	))))
	var created map[string]any
	_ = json.Unmarshal(create.Body.Bytes(), &created)
	data := created["data"].(map[string]any)
	id := data["id"].(string)
	syncID := data["sync_id"].(string)

	del := httptest.NewRecorder()
	mux.ServeHTTP(del, httptest.NewRequest(http.MethodDelete, "/v1/controlplane/users/"+id, nil))
	if del.Code != 200 {
		t.Fatalf("delete %d", del.Code)
	}

	list := httptest.NewRecorder()
	mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users", nil))
	var listEnv map[string]any
	_ = json.Unmarshal(list.Body.Bytes(), &listEnv)
	if arr, _ := listEnv["data"].([]any); len(arr) != 0 {
		t.Fatalf("list should hide tombstones, got %v", listEnv["data"])
	}

	list2 := httptest.NewRecorder()
	mux.ServeHTTP(list2, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users?include_deleted=1", nil))
	var list2Env map[string]any
	_ = json.Unmarshal(list2.Body.Bytes(), &list2Env)
	if arr, _ := list2Env["data"].([]any); len(arr) != 1 {
		t.Fatalf("include_deleted list want 1 got %v", list2Env["data"])
	}

	// Recreate via import with same sync_id should revive / update tombstone.
	impBody, _ := json.Marshal(map[string]any{
		"source": "client",
		"policy": map[string]any{"secrets": "identity", "apply_tombstones": true},
		"users": []any{
			map[string]any{"sync_id": syncID, "name": "carol", "sync_mode": "identity", "revision": 99, "enabled": true},
		},
	})
	imp := httptest.NewRecorder()
	mux.ServeHTTP(imp, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/import", bytes.NewReader(impBody)))
	if imp.Code != 200 {
		t.Fatalf("import revive %d %s", imp.Code, imp.Body.String())
	}

	memBody, _ := json.Marshal(map[string]any{"disable": []string{syncID}})
	mem := httptest.NewRecorder()
	mux.ServeHTTP(mem, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/sync/membership", bytes.NewReader(memBody)))
	if mem.Code != 200 {
		t.Fatalf("membership %d %s", mem.Code, mem.Body.String())
	}

	exp := httptest.NewRecorder()
	mux.ServeHTTP(exp, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users/export?sync=1", nil))
	var expEnv map[string]any
	_ = json.Unmarshal(exp.Body.Bytes(), &expEnv)
	users := expEnv["data"].(map[string]any)["users"].([]any)
	if len(users) != 0 {
		t.Fatalf("disabled sync user must not export, got %d", len(users))
	}
}

func TestUsersSyncToggleOnOffKeepsSyncID(t *testing.T) {
	t.Parallel()
	svc, mux := newSyncTestSvc(t)

	create := httptest.NewRecorder()
	mux.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(
		`{"name":"erin"}`,
	))))
	var created map[string]any
	_ = json.Unmarshal(create.Body.Bytes(), &created)
	data := created["data"].(map[string]any)
	localID := data["id"].(string)

	if _, err := svc.ApplyTrafficUsage(t.Context(), map[string]uint64{localID: 4242}); err != nil {
		t.Fatal(err)
	}

	on := httptest.NewRecorder()
	mux.ServeHTTP(on, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/"+localID+"/sync",
		bytes.NewReader([]byte(`{"enabled":true}`))))
	if on.Code != 200 {
		t.Fatalf("sync on %d %s", on.Code, on.Body.String())
	}
	var onEnv map[string]any
	_ = json.Unmarshal(on.Body.Bytes(), &onEnv)
	onData := onEnv["data"].(map[string]any)
	syncID := onData["sync_id"].(string)
	if syncID == "" {
		t.Fatal("expected sync_id")
	}
	if onData["sync_enabled"] != true {
		t.Fatalf("sync_enabled=%v", onData["sync_enabled"])
	}
	if onData["sync_mode"] != "identity" {
		t.Fatalf("mode=%v", onData["sync_mode"])
	}

	users, _ := svc.store.LoadUsers()
	for _, u := range users {
		if u.ID == localID && u.TrafficIngressBytes != 4242 {
			t.Fatalf("ingress seeded want 4242 got %d", u.TrafficIngressBytes)
		}
	}

	off := httptest.NewRecorder()
	mux.ServeHTTP(off, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/"+localID+"/sync",
		bytes.NewReader([]byte(`{"enabled":false}`))))
	if off.Code != 200 {
		t.Fatalf("sync off %d %s", off.Code, off.Body.String())
	}
	var offEnv map[string]any
	_ = json.Unmarshal(off.Body.Bytes(), &offEnv)
	offData := offEnv["data"].(map[string]any)
	if offData["sync_id"] != syncID {
		t.Fatalf("sync_id cleared: %v", offData["sync_id"])
	}
	if offData["sync_enabled"] != false {
		t.Fatal("expected sync_enabled=false")
	}

	// Still listed (not deleted).
	list := httptest.NewRecorder()
	mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users", nil))
	var listEnv map[string]any
	_ = json.Unmarshal(list.Body.Bytes(), &listEnv)
	arr := listEnv["data"].([]any)
	if len(arr) != 1 {
		t.Fatalf("list=%v", listEnv["data"])
	}

	// Metrics still show ignored user.
	rep := httptest.NewRecorder()
	mux.ServeHTTP(rep, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users/sync/metrics?sync_ids="+syncID, nil))
	var repEnv map[string]any
	_ = json.Unmarshal(rep.Body.Bytes(), &repEnv)
	items := repEnv["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("metrics items=%v", items)
	}
	item := items[0].(map[string]any)
	if item["sync_enabled"] != false {
		t.Fatalf("ignored should be visible with sync_enabled=false, got %v", item)
	}

	// Re-ON reuses same sync_id.
	on2 := httptest.NewRecorder()
	mux.ServeHTTP(on2, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/"+localID+"/sync",
		bytes.NewReader([]byte(`{"enabled":true,"mode":"identity"}`))))
	var on2Env map[string]any
	_ = json.Unmarshal(on2.Body.Bytes(), &on2Env)
	if on2Env["data"].(map[string]any)["sync_id"] != syncID {
		t.Fatalf("sync_id rotated unexpectedly: %v", on2Env["data"])
	}
}

func TestUsersSyncRenameOnNameConflict(t *testing.T) {
	t.Parallel()
	svc, mux := newSyncTestSvc(t)

	// Local-only user (e.g. server "Default") must not be adopted by name.
	create := httptest.NewRecorder()
	mux.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(
		`{"name":"Default"}`,
	))))
	var created map[string]any
	_ = json.Unmarshal(create.Body.Bytes(), &created)
	data := created["data"].(map[string]any)
	localID := data["id"].(string)

	syncID := "11111111-2222-4333-8444-555555555555"
	impBody, _ := json.Marshal(map[string]any{
		"source": "client",
		"policy": map[string]any{"secrets": "identity", "on_name_conflict": "merge_by_sync_id"},
		"users": []any{
			map[string]any{
				"sync_id": syncID, "name": "Default", "sync_mode": "identity", "revision": 2, "enabled": true,
			},
		},
	})
	imp := httptest.NewRecorder()
	mux.ServeHTTP(imp, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/import", bytes.NewReader(impBody)))
	if imp.Code != 200 {
		t.Fatalf("import %d %s", imp.Code, imp.Body.String())
	}
	var impEnv map[string]any
	_ = json.Unmarshal(imp.Body.Bytes(), &impEnv)
	res := impEnv["data"].(map[string]any)
	if int(res["created"].(float64)) != 1 {
		t.Fatalf("want created=1 got %v", res)
	}
	if int(res["updated"].(float64)) != 0 {
		t.Fatalf("want updated=0 (no adopt) got %v", res)
	}
	usersOut := res["users"].([]any)
	row := usersOut[0].(map[string]any)
	if row["action"] != "created" {
		t.Fatalf("row=%v", row)
	}
	importedID := row["local_id"].(string)
	if importedID == localID {
		t.Fatalf("imported overwrote local id %s", localID)
	}

	users, _ := svc.store.LoadUsers()
	var localOK, importedOK bool
	for _, u := range users {
		if u.ID == localID {
			localOK = true
			if u.SyncID != "" {
				t.Fatalf("local user gained sync_id=%q", u.SyncID)
			}
			if u.Name != "Default" {
				t.Fatalf("local name mutated to %q", u.Name)
			}
		}
		if u.ID == importedID {
			importedOK = true
			if u.SyncID != syncID {
				t.Fatalf("imported sync_id=%s", u.SyncID)
			}
			if u.Name != "Default 2" {
				t.Fatalf("imported name want %q got %q", "Default 2", u.Name)
			}
		}
	}
	if !localOK || !importedOK {
		t.Fatalf("localOK=%v importedOK=%v", localOK, importedOK)
	}
}

func TestUsersSyncRenameSkipsTakenSuffixes(t *testing.T) {
	t.Parallel()
	_, mux := newSyncTestSvc(t)

	for _, name := range []string{"alice", "alice 2"} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users", bytes.NewReader([]byte(
			fmt.Sprintf(`{"name":%q}`, name),
		))))
		if rr.Code != 200 {
			t.Fatalf("create %q: %d %s", name, rr.Code, rr.Body.String())
		}
	}

	syncID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	impBody, _ := json.Marshal(map[string]any{
		"source": "client",
		"policy": map[string]any{"secrets": "full", "on_name_conflict": "rename"},
		"users": []any{
			map[string]any{
				"sync_id": syncID, "name": "alice", "sync_mode": "full", "revision": 1, "enabled": true,
				"sub_token": "tok-alice-3",
			},
		},
	})
	imp := httptest.NewRecorder()
	mux.ServeHTTP(imp, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/import", bytes.NewReader(impBody)))
	if imp.Code != 200 {
		t.Fatalf("import %d %s", imp.Code, imp.Body.String())
	}
	var impEnv map[string]any
	_ = json.Unmarshal(imp.Body.Bytes(), &impEnv)
	usersOut := impEnv["data"].(map[string]any)["users"].([]any)
	id := usersOut[0].(map[string]any)["local_id"].(string)

	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users/"+id+"?secrets=1", nil))
	var getEnv map[string]any
	_ = json.Unmarshal(get.Body.Bytes(), &getEnv)
	gotName := getEnv["data"].(map[string]any)["name"].(string)
	if gotName != "alice 3" {
		t.Fatalf("name want alice 3 got %q", gotName)
	}
}

func TestUsersSyncFullSecretsCloneCreateAndUpdate(t *testing.T) {
	t.Parallel()
	svc, mux := newSyncTestSvc(t)

	syncID := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	token := "clone-token-deadbeef"
	impCreate, _ := json.Marshal(map[string]any{
		"source": "client",
		"policy": map[string]any{
			"secrets":          "full",
			"on_name_conflict": "merge_by_sync_id",
		},
		"users": []any{
			map[string]any{
				"sync_id":    syncID,
				"name":       "full-user",
				"sync_mode":  "full",
				"sync_enabled": true,
				"revision":   1,
				"enabled":    true,
				"sub_token":  token,
				"creds": map[string]any{
					"vless-tcp": map[string]any{"uuid": "11111111-1111-1111-1111-111111111111"},
				},
			},
		},
	})
	create := httptest.NewRecorder()
	mux.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/import", bytes.NewReader(impCreate)))
	if create.Code != 200 {
		t.Fatalf("import create %d %s", create.Code, create.Body.String())
	}

	users, err := svc.store.LoadUsers()
	if err != nil {
		t.Fatal(err)
	}
	var localID string
	for _, u := range users {
		if u.SyncID == syncID {
			localID = u.ID
			if u.SubToken != token {
				t.Fatalf("sub_token want %q got %q", token, u.SubToken)
			}
			if u.Creds == nil || u.Creds["vless-tcp"] == nil {
				t.Fatalf("creds missing: %+v", u.Creds)
			}
			break
		}
	}
	if localID == "" {
		t.Fatal("local user missing after full import")
	}

	exp := httptest.NewRecorder()
	mux.ServeHTTP(exp, httptest.NewRequest(http.MethodGet, "/v1/controlplane/users/export?sync=1&mode=full", nil))
	if exp.Code != 200 {
		t.Fatalf("export full %d %s", exp.Code, exp.Body.String())
	}
	var expEnv map[string]any
	_ = json.Unmarshal(exp.Body.Bytes(), &expEnv)
	exported := expEnv["data"].(map[string]any)["users"].([]any)
	if len(exported) != 1 {
		t.Fatalf("export users=%d", len(exported))
	}
	eu := exported[0].(map[string]any)
	if eu["sub_token"] != token {
		t.Fatalf("export token=%v", eu["sub_token"])
	}
	if eu["creds"] == nil {
		t.Fatal("export missing creds")
	}

	newToken := "rotated-token-cafebabe"
	impUpdate, _ := json.Marshal(map[string]any{
		"source": "client",
		"policy": map[string]any{
			"secrets":          "full",
			"on_name_conflict": "merge_by_sync_id",
		},
		"users": []any{
			map[string]any{
				"sync_id":      syncID,
				"name":         "full-user",
				"sync_mode":    "full",
				"sync_enabled": true,
				"revision":     2,
				"enabled":      true,
				"sub_token":    newToken,
				"creds": map[string]any{
					"vless-tcp": map[string]any{"uuid": "22222222-2222-2222-2222-222222222222"},
				},
			},
		},
	})
	upd := httptest.NewRecorder()
	mux.ServeHTTP(upd, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/import", bytes.NewReader(impUpdate)))
	if upd.Code != 200 {
		t.Fatalf("import update %d %s", upd.Code, upd.Body.String())
	}

	users2, _ := svc.store.LoadUsers()
	for _, u := range users2 {
		if u.SyncID != syncID {
			continue
		}
		if u.ID != localID {
			t.Fatalf("local id rotated: %s -> %s", localID, u.ID)
		}
		if u.SubToken != newToken {
			t.Fatalf("updated token want %q got %q", newToken, u.SubToken)
		}
		vless := u.Creds["vless-tcp"]
		if vless == nil || vless["uuid"] != "22222222-2222-2222-2222-222222222222" {
			t.Fatalf("updated creds=%v", u.Creds)
		}
	}

	// OFF keeps sync_id (ignore), does not wipe token.
	off := httptest.NewRecorder()
	mux.ServeHTTP(off, httptest.NewRequest(http.MethodPost, "/v1/controlplane/users/"+localID+"/sync",
		bytes.NewReader([]byte(`{"enabled":false}`))))
	if off.Code != 200 {
		t.Fatalf("sync off %d %s", off.Code, off.Body.String())
	}
	users3, _ := svc.store.LoadUsers()
	for _, u := range users3 {
		if u.ID != localID {
			continue
		}
		if u.SyncID != syncID {
			t.Fatalf("sync_id cleared on OFF: %q", u.SyncID)
		}
		if u.SyncEnabled {
			t.Fatal("expected sync_enabled=false")
		}
		if u.SubToken != newToken {
			t.Fatalf("OFF must not wipe token, got %q", u.SubToken)
		}
	}
}

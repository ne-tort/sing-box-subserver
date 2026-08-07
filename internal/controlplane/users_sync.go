//go:build with_controlplane

package controlplane

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/smoke"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
)

func newSyncUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func (s *Service) nodeID() string {
	if s.cfg.Cfg != nil {
		return strings.TrimSpace(s.cfg.Cfg.NodeID)
	}
	return ""
}

func (s *Service) handleUsersExport(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	q := r.URL.Query()
	syncOnly := q.Get("sync") == "1"
	mode := strings.TrimSpace(q.Get("mode"))
	if mode == "" {
		mode = domain.SyncModeIdentity
	}
	if mode != domain.SyncModeIdentity && mode != domain.SyncModeFull {
		failJSON(w, 400, "bad_request", "mode must be identity or full")
		return
	}
	includeDeleted := q.Get("include_deleted") == "1"
	idFilter := map[string]struct{}{}
	if raw := strings.TrimSpace(q.Get("ids")); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				idFilter[p] = struct{}{}
			}
		}
	}
	withSecrets := mode == domain.SyncModeFull
	outUsers := make([]any, 0)
	for _, u := range users {
		if smoke.IsSmokeUser(u.Name) {
			continue
		}
		if !includeDeleted && u.DeletedAt != nil {
			continue
		}
		if len(idFilter) > 0 {
			if _, ok := idFilter[u.ID]; !ok {
				if _, ok2 := idFilter[u.SyncID]; !ok2 {
					continue
				}
			}
		}
		if syncOnly {
			if !u.SyncActive() && !(includeDeleted && u.DeletedAt != nil && strings.TrimSpace(u.SyncID) != "" && u.EffectiveSyncMode() != domain.SyncModeLocal) {
				continue
			}
		}
		outUsers = append(outUsers, redactUser(u, withSecrets))
	}
	okJSON(w, 200, map[string]any{
		"format_version": 1,
		"node_id":        s.nodeID(),
		"exported_at":    time.Now().UTC(),
		"users":          outUsers,
	})
}

func (s *Service) handleUsersImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
		Policy struct {
			Secrets         string `json:"secrets"` // identity | full | keep_local
			OnNameConflict  string `json:"on_name_conflict"`
			ApplyTombstones *bool  `json:"apply_tombstones"`
		} `json:"policy"`
		Users []map[string]any `json:"users"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	secretsPolicy := strings.TrimSpace(body.Policy.Secrets)
	if secretsPolicy == "" {
		secretsPolicy = domain.SyncModeIdentity
	}
	if secretsPolicy != domain.SyncModeIdentity && secretsPolicy != domain.SyncModeFull && secretsPolicy != "keep_local" {
		failJSON(w, 400, "bad_request", "invalid policy.secrets")
		return
	}
	nameConflict := strings.TrimSpace(body.Policy.OnNameConflict)
	if nameConflict == "" {
		nameConflict = "merge_by_sync_id"
	}
	applyTombs := true
	if body.Policy.ApplyTombstones != nil {
		applyTombs = *body.Policy.ApplyTombstones
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	now := time.Now().UTC()
	created, updated, skipped, tombstoned := 0, 0, 0, 0
	needRemat := false
	results := make([]map[string]any, 0, len(body.Users))

	bySync := map[string]int{}
	byName := map[string]int{}
	for i := range users {
		if users[i].SyncID != "" {
			bySync[users[i].SyncID] = i
		}
		if users[i].DeletedAt == nil {
			byName[users[i].Name] = i
		}
	}

	for _, raw := range body.Users {
		syncID := strings.TrimSpace(fmtString(raw["sync_id"]))
		name := strings.TrimSpace(fmtString(raw["name"]))
		if name == "" && syncID == "" {
			skipped++
			continue
		}
		if smoke.IsSmokeUser(name) {
			failJSON(w, 400, "bad_request", "reserved system name")
			return
		}
		rev := uint64FromAny(raw["revision"])
		var deletedAt *time.Time
		if v, ok := raw["deleted_at"]; ok && v != nil {
			if s, ok := v.(string); ok && s != "" {
				t, err := parseTimePtr(s)
				if err != nil {
					failJSON(w, 400, "bad_request", err.Error())
					return
				}
				deletedAt = t
			}
		}

		if idx, ok := bySync[syncID]; ok && syncID != "" {
			u := &users[idx]
			if rev > 0 && rev <= u.Revision && deletedAt == nil {
				skipped++
				results = append(results, map[string]any{"sync_id": u.SyncID, "local_id": u.ID, "action": "skipped"})
				continue
			}
			if deletedAt != nil && applyTombs {
				if u.DeletedAt == nil {
					if byName[u.Name] == idx {
						delete(byName, u.Name)
					}
				}
				u.DeletedAt = deletedAt
				u.Enabled = false
				u.UpdatedAt = now
				if rev > u.Revision {
					u.Revision = rev
				} else {
					u.Revision++
				}
				tombstoned++
				needRemat = true
				results = append(results, map[string]any{"sync_id": u.SyncID, "local_id": u.ID, "action": "tombstoned"})
				continue
			}
			wantName := strings.TrimSpace(fmtString(raw["name"]))
			if wantName == "" {
				wantName = u.Name
			}
			if wantName != "" {
				if ni, taken := byName[wantName]; taken && ni != idx {
					switch nameConflict {
					case "reject":
						failJSON(w, 409, "cp_name_conflict", "name already exists: "+wantName)
						return
					case "rename", "merge_by_sync_id":
						wantName = uniqueImportName(wantName, byName)
					default:
						failJSON(w, 400, "bad_request", "invalid on_name_conflict")
						return
					}
				}
				raw["name"] = wantName
			}
			oldName := u.Name
			if err := s.applyImportProfile(u, raw, secretsPolicy, now, rev); err != nil {
				failJSON(w, 400, "bad_request", err.Error())
				return
			}
			if u.DeletedAt == nil && oldName != u.Name {
				if byName[oldName] == idx {
					delete(byName, oldName)
				}
				byName[u.Name] = idx
			}
			updated++
			needRemat = true
			results = append(results, map[string]any{"sync_id": u.SyncID, "local_id": u.ID, "action": "updated"})
			continue
		}

		// Name conflict without sync_id match: never adopt a different local user.
		// Identity is sync_id-only; colliding names get a deterministic " {n}" suffix.
		if name != "" {
			if _, ok := byName[name]; ok {
				switch nameConflict {
				case "reject":
					failJSON(w, 409, "cp_name_conflict", "name already exists: "+name)
					return
				case "rename", "merge_by_sync_id":
					name = uniqueImportName(name, byName)
				default:
					failJSON(w, 400, "bad_request", "invalid on_name_conflict")
					return
				}
				raw["name"] = name
			}
		}

		u, err := s.newUserFromImport(raw, secretsPolicy, syncID, name, now, rev, body.Source)
		if err != nil {
			failJSON(w, 400, "bad_request", err.Error())
			return
		}
		if deletedAt != nil && applyTombs {
			u.DeletedAt = deletedAt
			u.Enabled = false
		}
		users = append(users, u)
		bySync[u.SyncID] = len(users) - 1
		if u.DeletedAt == nil {
			byName[u.Name] = len(users) - 1
		}
		created++
		needRemat = true
		results = append(results, map[string]any{"sync_id": u.SyncID, "local_id": u.ID, "action": "created"})
	}

	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	result := map[string]any{
		"created":    created,
		"updated":    updated,
		"skipped":    skipped,
		"tombstoned": tombstoned,
		"source":     body.Source,
		"users":      results,
	}
	if needRemat {
		if err := s.rematerializeLocked(r.Context(), false); err != nil {
			result["rematerialize_warning"] = err.Error()
		}
	}
	okJSON(w, 200, result)
}

func uniqueImportName(base string, byName map[string]int) string {
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s %d", base, i)
		if _, ok := byName[cand]; !ok {
			return cand
		}
	}
}

func (s *Service) applyImportProfile(u *domain.User, raw map[string]any, secretsPolicy string, now time.Time, rev uint64) error {
	wasSync := u.SyncActive()
	if v := strings.TrimSpace(fmtString(raw["name"])); v != "" {
		u.Name = v
	}
	if v, ok := raw["enabled"].(bool); ok {
		u.Enabled = v
	}
	if v := strings.TrimSpace(fmtString(raw["sync_mode"])); v != "" {
		u.SyncMode = v
	}
	if v, ok := raw["sync_enabled"].(bool); ok {
		u.SyncEnabled = v
	} else if u.EffectiveSyncMode() != domain.SyncModeLocal {
		u.SyncEnabled = true
	}
	if syncID := strings.TrimSpace(fmtString(raw["sync_id"])); syncID != "" {
		u.SyncID = syncID
	}
	if _, ok := raw["expires_at"]; ok {
		if raw["expires_at"] == nil {
			u.ExpiresAt = nil
		} else if s, ok := raw["expires_at"].(string); ok {
			t, err := parseTimePtr(s)
			if err != nil {
				return err
			}
			u.ExpiresAt = t
		}
	}
	if _, ok := raw["traffic_limit_bytes"]; ok {
		if raw["traffic_limit_bytes"] == nil {
			u.TrafficLimitBytes = nil
		} else {
			v := uint64FromAny(raw["traffic_limit_bytes"])
			u.TrafficLimitBytes = &v
		}
	}
	if v, ok := raw["speed_up_bytes_per_sec"]; ok {
		u.SpeedUpBytesPerSec = int64(uint64FromAny(v))
	}
	if v, ok := raw["speed_down_bytes_per_sec"]; ok {
		u.SpeedDownBytesPerSec = int64(uint64FromAny(v))
	}
	switch secretsPolicy {
	case domain.SyncModeFull:
		if tok := strings.TrimSpace(fmtString(raw["sub_token"])); tok != "" {
			u.SubToken = tok
		}
		if creds, ok := raw["creds"].(map[string]any); ok {
			mapped := map[string]map[string]any{}
			for k, v := range creds {
				if m, ok := v.(map[string]any); ok {
					mapped[k] = m
				}
			}
			if err := validateCreds(mapped); err != nil {
				return err
			}
			mergeUserCreds(u, mapped)
		}
	case "keep_local":
		// leave secrets
	default: // identity
		// leave secrets on update
	}
	if _, err := s.ensureCreds(u); err != nil {
		return err
	}
	u.DeletedAt = nil
	u.UpdatedAt = now
	if rev > u.Revision {
		u.Revision = rev
	} else {
		u.Revision++
	}
	if u.Origin == "" {
		u.Origin = domain.OriginImport
	}
	if !wasSync && u.SyncActive() {
		u.SeedIngressFromUsed()
	}
	return nil
}

func (s *Service) newUserFromImport(raw map[string]any, secretsPolicy, syncID, name string, now time.Time, rev uint64, source string) (domain.User, error) {
	idTok, err := randomToken()
	if err != nil {
		return domain.User{}, err
	}
	tok, err := randomToken()
	if err != nil {
		return domain.User{}, err
	}
	mode := strings.TrimSpace(fmtString(raw["sync_mode"]))
	if mode == "" {
		if syncID != "" {
			mode = domain.SyncModeIdentity
		} else {
			mode = domain.SyncModeLocal
		}
	}
	if syncID == "" && mode != domain.SyncModeLocal {
		syncID, err = newSyncUUID()
		if err != nil {
			return domain.User{}, err
		}
	}
	if name == "" {
		name = "import-" + idTok[:8]
	}
	u := domain.User{
		ID:          idTok[:16],
		Name:        name,
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
		SubToken:    tok,
		Creds:       map[string]map[string]any{},
		SyncID:      syncID,
		SyncMode:    mode,
		SyncEnabled: mode != domain.SyncModeLocal,
		Origin:      domain.OriginImport,
		Revision:    1,
	}
	if rev > 0 {
		u.Revision = rev
	}
	if source != "" && strings.HasPrefix(source, "peer:") {
		u.Origin = domain.OriginSync
	}
	if v, ok := raw["enabled"].(bool); ok {
		u.Enabled = v
	}
	if v, ok := raw["sync_enabled"].(bool); ok {
		u.SyncEnabled = v
	}
	if _, ok := raw["expires_at"]; ok {
		if raw["expires_at"] == nil {
			u.ExpiresAt = nil
		} else if s, ok := raw["expires_at"].(string); ok {
			t, err := parseTimePtr(s)
			if err != nil {
				return domain.User{}, err
			}
			u.ExpiresAt = t
		}
	}
	if _, ok := raw["traffic_limit_bytes"]; ok && raw["traffic_limit_bytes"] != nil {
		v := uint64FromAny(raw["traffic_limit_bytes"])
		u.TrafficLimitBytes = &v
	}
	if v, ok := raw["speed_up_bytes_per_sec"]; ok {
		u.SpeedUpBytesPerSec = int64(uint64FromAny(v))
	}
	if v, ok := raw["speed_down_bytes_per_sec"]; ok {
		u.SpeedDownBytesPerSec = int64(uint64FromAny(v))
	}
	if secretsPolicy == domain.SyncModeFull {
		if t := strings.TrimSpace(fmtString(raw["sub_token"])); t != "" {
			u.SubToken = t
		}
		if creds, ok := raw["creds"].(map[string]any); ok {
			mapped := map[string]map[string]any{}
			for k, v := range creds {
				if m, ok := v.(map[string]any); ok {
					mapped[k] = m
				}
			}
			if err := validateCreds(mapped); err != nil {
				return domain.User{}, err
			}
			mergeUserCreds(&u, mapped)
		}
	}
	if _, err := s.ensureCreds(&u); err != nil {
		return domain.User{}, err
	}
	return u, nil
}

func (s *Service) handleUsersSyncMetricsGet(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	filter := map[string]struct{}{}
	if raw := strings.TrimSpace(r.URL.Query().Get("sync_ids")); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				filter[p] = struct{}{}
			}
		}
	}
	items := make([]any, 0)
	for _, u := range users {
		if u.DeletedAt != nil || strings.TrimSpace(u.SyncID) == "" {
			continue
		}
		if u.EffectiveSyncMode() == domain.SyncModeLocal {
			continue
		}
		if len(filter) > 0 {
			if _, ok := filter[u.SyncID]; !ok {
				continue
			}
		}
		items = append(items, map[string]any{
			"sync_id":      u.SyncID,
			"local_id":     u.ID,
			"epoch":        u.TrafficEpoch,
			"ingress_bytes": u.TrafficIngressBytes,
			"used_bytes":   u.TrafficUsedBytes,
			"sync_enabled": u.SyncEnabled,
			"sync_mode":    u.EffectiveSyncMode(),
		})
	}
	okJSON(w, 200, map[string]any{
		"node_id": s.nodeID(),
		"items":   items,
	})
}

func (s *Service) handleUsersSyncMetricsPost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []struct {
			SyncID        string  `json:"sync_id"`
			GlobalUsed    *uint64 `json:"global_used"`
			ResetIngress  bool    `json:"reset_ingress"`
			ResetGlobal   bool    `json:"reset_global"`
		} `json:"items"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	now := time.Now().UTC()
	beforeElig := s.eligibilityMapLocked()
	before := s.eligibilityFingerprintLocked()
	bySync := map[string]int{}
	for i := range users {
		if users[i].SyncID != "" {
			bySync[users[i].SyncID] = i
		}
	}
	applied := 0
	var resetIDs []string
	for _, it := range body.Items {
		sid := strings.TrimSpace(it.SyncID)
		idx, ok := bySync[sid]
		if !ok {
			continue
		}
		u := &users[idx]
		changed := false
		if it.ResetIngress {
			u.TrafficEpoch++
			u.TrafficIngressBytes = 0
			resetIDs = append(resetIDs, u.ID)
			changed = true
		}
		if it.ResetGlobal {
			u.TrafficUsedBytes = 0
			changed = true
		}
		if it.GlobalUsed != nil {
			u.TrafficUsedBytes = *it.GlobalUsed
			changed = true
		}
		if changed {
			u.UpdatedAt = now
			applied++
		}
	}
	if applied == 0 {
		okJSON(w, 200, map[string]any{"applied": 0})
		return
	}
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if s.trafficHooks != nil {
		for _, id := range resetIDs {
			s.trafficHooks.OnTrafficReset(id)
		}
	}
	s.notifyBecameIneligibleLocked(beforeElig)
	result := map[string]any{"applied": applied}
	if before != s.eligibilityFingerprintLocked() {
		if err := s.rematerializeLocked(r.Context(), false); err != nil {
			result["rematerialize_warning"] = err.Error()
		}
	}
	okJSON(w, 200, result)
}

func (s *Service) handleUsersSyncMembership(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enable  []string `json:"enable"`
		Disable []string `json:"disable"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	now := time.Now().UTC()
	en := map[string]struct{}{}
	for _, id := range body.Enable {
		en[strings.TrimSpace(id)] = struct{}{}
	}
	dis := map[string]struct{}{}
	for _, id := range body.Disable {
		dis[strings.TrimSpace(id)] = struct{}{}
	}
	n := 0
	for i := range users {
		sid := users[i].SyncID
		if sid == "" {
			continue
		}
		if _, ok := en[sid]; ok {
			users[i].SyncEnabled = true
			users[i].UpdatedAt = now
			n++
		}
		if _, ok := dis[sid]; ok {
			users[i].SyncEnabled = false
			users[i].UpdatedAt = now
			n++
		}
	}
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{"updated": n})
}

// handleUsersSyncToggle enables/disables sync for one local user without deleting it.
// ON: ensure sync_id + mode, sync_enabled=true, seed ingress. OFF: sync_enabled=false only.
func (s *Service) handleUsersSyncToggle(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var body struct {
		Enabled *bool  `json:"enabled"`
		Mode    string `json:"mode"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if body.Enabled == nil {
		failJSON(w, 400, "bad_request", "enabled required")
		return
	}
	u := &users[i]
	if u.DeletedAt != nil {
		failJSON(w, 400, "bad_request", "user is deleted")
		return
	}
	now := time.Now().UTC()
	if *body.Enabled {
		mode := strings.TrimSpace(body.Mode)
		if mode == "" {
			mode = domain.SyncModeIdentity
		}
		if mode != domain.SyncModeIdentity && mode != domain.SyncModeFull {
			failJSON(w, 400, "bad_request", "mode must be identity or full")
			return
		}
		if strings.TrimSpace(u.SyncID) == "" {
			sid, err := newSyncUUID()
			if err != nil {
				failJSON(w, 500, "internal", err.Error())
				return
			}
			u.SyncID = sid
		}
		u.SyncMode = mode
		u.SyncEnabled = true
		u.SeedIngressFromUsed()
		u.Revision++
		u.UpdatedAt = now
		if u.Origin == "" {
			u.Origin = domain.OriginLocal
		}
	} else {
		// Opt-out on this node only — keep sync_id / sync_mode for ignore detection.
		u.SyncEnabled = false
		u.Revision++
		u.UpdatedAt = now
	}
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, redactUser(*u, false))
}

func fmtString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func uint64FromAny(v any) uint64 {
	switch t := v.(type) {
	case float64:
		return uint64(t)
	case int:
		return uint64(t)
	case int64:
		return uint64(t)
	case uint64:
		return t
	case string:
		var n uint64
		_, _ = fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}

//go:build with_controlplane

package controlplane

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/materialize"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/demuxrecipes"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

// Service is the embedded controlplane.
type Service struct {
	cfg   Deps
	store *store.Store
	log   *slog.Logger
	mu    sync.Mutex

	mgmtTLS   mgmtCertCache
	acmeWatch struct {
		mu        sync.Mutex
		enteredAt time.Time
		everReady bool
		lostSince time.Time
	}
}

// New constructs the CP service (with_controlplane builds only).
func New(d Deps) *Service {
	st, err := store.Open(d.DataDir)
	if err != nil {
		if d.Logger != nil {
			d.Logger.Error("controlplane store open failed", "err", err)
		}
		return nil
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{cfg: d, store: st, log: log}
}

func (s *Service) OnLeaveOwnership() {
	if s == nil {
		return
	}
	_ = s.store.ClearActiveSets()
}

// Bootstrap ensures default TLS profile and rematerializes if we own the dataplane.
func (s *Service) Bootstrap(ctx context.Context) {
	if s == nil {
		return
	}
	p, err := s.ensureTLSProfile(false)
	if err != nil && s.log != nil {
		s.log.Warn("controlplane tls profile bootstrap failed", "err", err)
	} else if err == nil {
		if err := s.ensureSafetySelfSignedPEMs(p); err != nil && s.log != nil {
			s.log.Warn("controlplane safety self_signed pems failed", "err", err)
		}
		if p.Mode == domain.TLSModeACMEDomain || p.Mode == domain.TLSModeACMEIP {
			s.noteACMEModeEntered()
			if p.ACME != nil {
				ready, _, _ := acmeCertificateReady(s.cfg.DataDir, p.ACME.Domains)
				s.noteACMEReady(ready)
			}
		}
	}
	if s.cfg.Owner == nil || s.cfg.Owner.Owner() != configowner.ModeControlplane {
		return
	}
	st, err := s.store.LoadState()
	if err != nil || len(st.ActiveSets) == 0 {
		return
	}
	if err := s.rematerialize(ctx); err != nil && s.log != nil {
		s.log.Warn("controlplane boot rematerialize failed", "err", err)
	}
}

func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}
	tickSec := 60
	if s.cfg.Cfg != nil && s.cfg.Cfg.Controlplane.ExpiryTickSec > 0 {
		tickSec = s.cfg.Cfg.Controlplane.ExpiryTickSec
	}
	// ACME watchdog ticks more often than expiry; reuse min(tick, 30s).
	watchSec := tickSec
	if watchSec > 30 {
		watchSec = 30
	}
	t := time.NewTicker(time.Duration(tickSec) * time.Second)
	w := time.NewTicker(time.Duration(watchSec) * time.Second)
	defer t.Stop()
	defer w.Stop()
	var lastFP string
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.C:
			s.acmeWatchdog(ctx)
		case <-t.C:
			s.mu.Lock()
			_ = s.applyTrafficResetsLocked(time.Now().UTC())
			fp := s.eligibilityFingerprintLocked()
			s.mu.Unlock()
			if s.cfg.Owner == nil || s.cfg.Owner.Owner() != configowner.ModeControlplane {
				lastFP = fp
				continue
			}
			if fp != lastFP {
				lastFP = fp
				_ = s.rematerialize(ctx)
			}
		}
	}
}

func (s *Service) eligibilityFingerprintLocked() string {
	users, err := s.store.LoadUsers()
	if err != nil {
		return ""
	}
	st, err := s.store.LoadState()
	if err != nil {
		return ""
	}
	now := time.Now().UTC()
	parts := append([]string{}, st.ActiveSets...)
	sort.Strings(parts)
	for _, u := range users {
		if u.Eligible(now) {
			parts = append(parts, "u:"+u.ID)
		} else {
			parts = append(parts, "x:"+u.ID)
		}
	}
	return strings.Join(parts, ",")
}

func (s *Service) applyTrafficResetsLocked(now time.Time) bool {
	users, err := s.store.LoadUsers()
	if err != nil {
		return false
	}
	changed := false
	for i := range users {
		u := &users[i]
		if u.TrafficResetAt == nil || u.TrafficResetAt.After(now) {
			continue
		}
		u.TrafficUsedBytes = 0
		if u.TrafficResetPeriodSec != nil && *u.TrafficResetPeriodSec > 0 {
			next := now.Add(time.Duration(*u.TrafficResetPeriodSec) * time.Second)
			u.TrafficResetAt = &next
		} else {
			u.TrafficResetAt = nil
		}
		u.UpdatedAt = now
		changed = true
	}
	if changed {
		_ = s.store.SaveUsers(users)
	}
	return changed
}

func (s *Service) Register(mux *http.ServeMux, requireAuth func(func(http.ResponseWriter, *http.Request)) http.HandlerFunc) {
	mux.HandleFunc("GET /v1/controlplane/status", requireAuth(s.handleStatus))
	mux.HandleFunc("GET /v1/controlplane/users", requireAuth(s.handleUsersList))
	mux.HandleFunc("POST /v1/controlplane/users", requireAuth(s.handleUsersCreate))
	mux.HandleFunc("GET /v1/controlplane/users/{id}", requireAuth(s.handleUsersGet))
	mux.HandleFunc("PATCH /v1/controlplane/users/{id}", requireAuth(s.handleUsersPatch))
	mux.HandleFunc("DELETE /v1/controlplane/users/{id}", requireAuth(s.handleUsersDelete))
	mux.HandleFunc("POST /v1/controlplane/users/{id}/rotate-token", requireAuth(s.handleUsersRotateToken))
	mux.HandleFunc("POST /v1/controlplane/users/{id}/rotate-creds", requireAuth(s.handleUsersRotateCreds))
	mux.HandleFunc("GET /v1/controlplane/presets", requireAuth(s.handlePresetsList))
	mux.HandleFunc("GET /v1/controlplane/presets/{name}", requireAuth(s.handlePresetsGet))
	mux.HandleFunc("GET /v1/controlplane/demux-recipes", requireAuth(s.handleDemuxRecipesList))
	mux.HandleFunc("GET /v1/controlplane/demux-recipes/{name}", requireAuth(s.handleDemuxRecipesGet))
	mux.HandleFunc("GET /v1/controlplane/sets", requireAuth(s.handleSetsList))
	mux.HandleFunc("POST /v1/controlplane/sets", requireAuth(s.handleSetsCreate))
	mux.HandleFunc("GET /v1/controlplane/sets/{name}", requireAuth(s.handleSetsGet))
	mux.HandleFunc("PUT /v1/controlplane/sets/{name}", requireAuth(s.handleSetsPut))
	mux.HandleFunc("DELETE /v1/controlplane/sets/{name}", requireAuth(s.handleSetsDelete))
	mux.HandleFunc("POST /v1/controlplane/sets/{name}/activate", requireAuth(s.handleSetsActivate))
	mux.HandleFunc("POST /v1/controlplane/sets/{name}/deactivate", requireAuth(s.handleSetsDeactivate))
	mux.HandleFunc("GET /v1/controlplane/tls", requireAuth(s.handleTLSGet))
	mux.HandleFunc("PUT /v1/controlplane/tls", requireAuth(s.handleTLSPut))
	mux.HandleFunc("POST /v1/controlplane/tls/regenerate", requireAuth(s.handleTLSRegenerate))
	mux.HandleFunc("GET /v1/sub/{token}", s.handleSub)
}

func randomToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func randomPassword() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Service) ensureCreds(u *domain.User) (bool, error) {
	if u.Creds == nil {
		u.Creds = map[string]map[string]any{}
	}
	changed := false
	for _, p := range presets.All() {
		if u.Creds[p.Name] != nil {
			continue
		}
		creds := map[string]any{}
		for _, f := range p.CredFields {
			if f == "uuid" {
				tok, err := randomToken()
				if err != nil {
					return false, err
				}
				creds["uuid"] = fmt.Sprintf("%s-%s-%s-%s-%s", tok[0:8], tok[8:12], tok[12:16], tok[16:20], tok[20:32])
			} else {
				pw, err := randomPassword()
				if err != nil {
					return false, err
				}
				creds[f] = pw
			}
		}
		u.Creds[p.Name] = creds
		changed = true
	}
	return changed, nil
}

func (s *Service) publicHost(r *http.Request) string {
	if s.cfg.Cfg != nil && s.cfg.Cfg.Controlplane.PublicHost != "" {
		return s.cfg.Cfg.Controlplane.PublicHost
	}
	if r != nil && r.Host != "" {
		host, _, err := net.SplitHostPort(r.Host)
		if err == nil {
			return host
		}
		return r.Host
	}
	return "127.0.0.1"
}

func (s *Service) subscriptionURL(r *http.Request, token string) (path, url string) {
	path = "/v1/sub/" + token
	host := s.publicHost(r)
	port := ""
	if s.cfg.Cfg != nil {
		if s.cfg.Cfg.Controlplane.PublicPort > 0 {
			port = strconv.Itoa(s.cfg.Cfg.Controlplane.PublicPort)
		} else if _, p, err := net.SplitHostPort(s.cfg.Cfg.Listen); err == nil {
			port = p
		}
	}
	// CP builds always serve management/sub over HTTPS (CP TLS profile).
	scheme := "https"
	if s.cfg.Cfg != nil && s.cfg.Cfg.HasTLS() {
		scheme = "https"
	}
	if port != "" && port != "80" && port != "443" {
		url = fmt.Sprintf("%s://%s:%s%s", scheme, host, port, path)
	} else {
		url = fmt.Sprintf("%s://%s%s", scheme, host, path)
	}
	return path, url
}

func (s *Service) activeSetObjects() ([]domain.InboundSet, error) {
	st, err := s.store.LoadState()
	if err != nil {
		return nil, err
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		return nil, err
	}
	byName := map[string]domain.InboundSet{}
	for _, set := range sets {
		byName[set.Name] = set
	}
	names := append([]string{}, st.ActiveSets...)
	sort.Strings(names)
	out := make([]domain.InboundSet, 0, len(names))
	for _, n := range names {
		if set, ok := byName[n]; ok {
			out = append(out, set)
		}
	}
	return out, nil
}

func (s *Service) eligibleUsers(now time.Time) ([]domain.User, error) {
	users, err := s.store.LoadUsers()
	if err != nil {
		return nil, err
	}
	changed := false
	out := make([]domain.User, 0, len(users))
	for i := range users {
		c, err := s.ensureCreds(&users[i])
		if err != nil {
			return nil, err
		}
		if c {
			changed = true
		}
		if users[i].Eligible(now) {
			out = append(out, users[i])
		}
	}
	if changed {
		_ = s.store.SaveUsers(users)
	}
	return out, nil
}

func (s *Service) rematerialize(ctx context.Context) error {
	return s.rematerializeForce(ctx, false)
}

func (s *Service) rematerializeForce(ctx context.Context, forceReload bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rematerializeLocked(ctx, forceReload)
}

func (s *Service) rematerializeLocked(ctx context.Context, forceReload bool) error {
	if s.cfg.Owner == nil || s.cfg.Owner.Owner() != configowner.ModeControlplane {
		return nil
	}
	sets, err := s.activeSetObjects()
	if err != nil {
		return err
	}
	if len(sets) == 0 {
		return nil
	}
	users, err := s.eligibleUsers(time.Now().UTC())
	if err != nil {
		return err
	}
	host := "127.0.0.1"
	if s.cfg.Cfg != nil && s.cfg.Cfg.Controlplane.PublicHost != "" {
		host = s.cfg.Cfg.Controlplane.PublicHost
	}
	profile, err := s.ensureTLSProfile(false)
	if err != nil {
		return err
	}
	in := materialize.Input{
		ActiveSets: sets,
		Users:      users,
		PublicHost: host,
		DataDir:    s.cfg.DataDir,
		TLS:        profile,
	}
	pemChanged := false
	if profile.Mode == domain.TLSModeSelfSigned {
		if profile.SelfSigned == nil {
			return fmt.Errorf("self_signed spec missing")
		}
		cert, key, changed, err := ensureSelfSigned(s.cfg.DataDir, *profile.SelfSigned, false)
		if err != nil {
			return fmt.Errorf("tls material: %w", err)
		}
		pemChanged = changed
		in.TLSCertPath, in.TLSKeyPath = cert, key
	}
	raw, err := materialize.Build(in)
	if err != nil {
		return err
	}
	res, err := s.cfg.Supervisor.Apply(ctx, supervisor.ApplyRequest{
		Raw:    raw,
		Source: configstore.SourceControlplane,
		Force:  forceReload || pemChanged,
	})
	if err != nil {
		return err
	}
	st, err := s.store.LoadState()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	st.LastMaterializeSHA256 = res.SHA256
	st.LastMaterializeAt = &now
	return s.store.SaveState(st)
}

func (s *Service) ensureTLSProfile(forceDefault bool) (domain.TLSProfile, error) {
	host := ""
	if s.cfg.Cfg != nil {
		host = s.cfg.Cfg.Controlplane.PublicHost
	}
	p, ok, err := s.store.LoadTLSProfile()
	if err != nil {
		return domain.TLSProfile{}, err
	}
	if !ok || forceDefault {
		p = domain.DefaultSelfSigned(host)
		if err := p.Validate(); err != nil {
			return domain.TLSProfile{}, err
		}
		if err := s.store.SaveTLSProfile(p); err != nil {
			return domain.TLSProfile{}, err
		}
		if p.Mode == domain.TLSModeSelfSigned && p.SelfSigned != nil {
			_, _, _, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, false)
			if err != nil {
				return domain.TLSProfile{}, err
			}
		}
		return p, nil
	}
	if err := p.Validate(); err != nil {
		return domain.TLSProfile{}, err
	}
	if p.Mode == domain.TLSModeSelfSigned && p.SelfSigned != nil {
		if _, _, _, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, false); err != nil {
			return domain.TLSProfile{}, err
		}
	} else if err := s.ensureSafetySelfSignedPEMs(p); err != nil {
		return domain.TLSProfile{}, err
	}
	return p, nil
}

func (s *Service) acmeWatchdog(ctx context.Context) {
	p, err := s.ensureTLSProfile(false)
	if err != nil {
		return
	}
	if p.Mode != domain.TLSModeACMEDomain && p.Mode != domain.TLSModeACMEIP {
		return
	}
	domains := []string{}
	if p.ACME != nil {
		domains = p.ACME.Domains
	}
	ready, _, _ := acmeCertificateReady(s.cfg.DataDir, domains)
	s.noteACMEReady(ready)
	if ready {
		return
	}
	ok, reason := s.shouldACMEFallback()
	if !ok {
		return
	}
	s.forceSelfSignedFallback(ctx, reason)
}

func (s *Service) forceSelfSignedFallback(ctx context.Context, reason string) {
	host := ""
	if s.cfg.Cfg != nil {
		host = s.cfg.Cfg.Controlplane.PublicHost
	}
	p := domain.DefaultSelfSigned(host)
	if err := p.Validate(); err != nil {
		if s.log != nil {
			s.log.Error("acme fallback: invalid self_signed profile", "err", err, "reason", reason)
		}
		return
	}
	if err := s.store.SaveTLSProfile(p); err != nil {
		if s.log != nil {
			s.log.Error("acme fallback: save profile failed", "err", err, "reason", reason)
		}
		return
	}
	if _, _, _, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, true); err != nil {
		if s.log != nil {
			s.log.Error("acme fallback: write pem failed", "err", err, "reason", reason)
		}
		return
	}
	// Reset ACME watch state after leaving ACME mode.
	s.acmeWatch.mu.Lock()
	s.acmeWatch.enteredAt = time.Time{}
	s.acmeWatch.everReady = false
	s.acmeWatch.lostSince = time.Time{}
	s.acmeWatch.mu.Unlock()
	s.mgmtTLS.mu.Lock()
	s.mgmtTLS.cert = nil
	s.mgmtTLS.source = ""
	s.mgmtTLS.mu.Unlock()

	if s.log != nil {
		s.log.Error("ACME emergency fallback to self_signed", "reason", reason)
	}
	if err := s.rematerializeForce(ctx, true); err != nil && s.log != nil {
		s.log.Error("acme fallback rematerialize failed", "err", err)
	}
}

func okJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

func failJSON(w http.ResponseWriter, code int, errCode, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": false,
		"error": map[string]any{"code": errCode, "message": msg},
	})
}

func redactUser(u domain.User, secrets bool) map[string]any {
	m := map[string]any{
		"id":                       u.ID,
		"name":                     u.Name,
		"enabled":                  u.Enabled,
		"created_at":               u.CreatedAt,
		"updated_at":               u.UpdatedAt,
		"expires_at":               u.ExpiresAt,
		"traffic_limit_bytes":      u.TrafficLimitBytes,
		"traffic_used_bytes":       u.TrafficUsedBytes,
		"traffic_reset_at":         u.TrafficResetAt,
		"traffic_reset_period_sec": u.TrafficResetPeriodSec,
		"has_token":                u.SubToken != "",
	}
	names := make([]string, 0, len(u.Creds))
	for k := range u.Creds {
		names = append(names, k)
	}
	sort.Strings(names)
	m["cred_presets"] = names
	if secrets {
		m["sub_token"] = u.SubToken
		m["creds"] = u.Creds
	}
	return m
}

func contains(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}

func removeStr(ss []string, x string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s != x {
			out = append(out, s)
		}
	}
	return out
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func parseTimePtr(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}

func (s *Service) handleStatus(w http.ResponseWriter, _ *http.Request) {
	users, _ := s.store.LoadUsers()
	sets, _ := s.store.LoadSets()
	st, _ := s.store.LoadState()
	now := time.Now().UTC()
	elig := 0
	for _, u := range users {
		if u.Eligible(now) {
			elig++
		}
	}
	mode := "idle"
	if s.cfg.Owner != nil {
		mode = string(s.cfg.Owner.Owner())
	}
	okJSON(w, http.StatusOK, map[string]any{
		"config_mode":              mode,
		"active_sets":              st.ActiveSets,
		"users_total":              len(users),
		"users_eligible":           elig,
		"presets_count":            len(presets.All()),
		"demux_recipes_count":      len(demuxrecipes.All()),
		"sets_count":               len(sets),
		"last_materialize_sha256":  st.LastMaterializeSHA256,
		"last_materialize_at":      st.LastMaterializeAt,
	})
}

func (s *Service) handleUsersList(w http.ResponseWriter, _ *http.Request) {
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	out := make([]any, 0, len(users))
	for _, u := range users {
		out = append(out, redactUser(u, false))
	}
	okJSON(w, 200, out)
}

func (s *Service) handleUsersCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name              string  `json:"name"`
		Enabled           *bool   `json:"enabled"`
		ExpiresAt         *string `json:"expires_at"`
		TrafficLimitBytes *uint64 `json:"traffic_limit_bytes"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		failJSON(w, 400, "bad_request", "name required")
		return
	}
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	for _, u := range users {
		if u.Name == body.Name {
			failJSON(w, 409, "conflict", "name already exists")
			return
		}
	}
	id, err := randomToken()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	tok, err := randomToken()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	now := time.Now().UTC()
	u := domain.User{
		ID:        id[:16],
		Name:      body.Name,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
		SubToken:  tok,
		Creds:     map[string]map[string]any{},
	}
	if body.Enabled != nil {
		u.Enabled = *body.Enabled
	}
	if body.ExpiresAt != nil {
		t, err := parseTimePtr(*body.ExpiresAt)
		if err != nil {
			failJSON(w, 400, "bad_request", err.Error())
			return
		}
		u.ExpiresAt = t
	}
	u.TrafficLimitBytes = body.TrafficLimitBytes
	if _, err := s.ensureCreds(&u); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	users = append(users, u)
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	_ = s.rematerialize(r.Context())
	path, url := s.subscriptionURL(r, u.SubToken)
	data := redactUser(u, true)
	data["subscription_path"] = path
	data["subscription_url"] = url
	okJSON(w, 200, data)
}

func (s *Service) findUser(id string) ([]domain.User, int, error) {
	users, err := s.store.LoadUsers()
	if err != nil {
		return nil, -1, err
	}
	for i := range users {
		if users[i].ID == id {
			return users, i, nil
		}
	}
	return users, -1, store.ErrNotFound
}

func (s *Service) handleUsersGet(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	secrets := r.URL.Query().Get("secrets") == "1"
	data := redactUser(users[i], secrets)
	if secrets {
		path, url := s.subscriptionURL(r, users[i].SubToken)
		data["subscription_path"] = path
		data["subscription_url"] = url
	}
	okJSON(w, 200, data)
}

func (s *Service) handleUsersPatch(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var body map[string]any
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	u := &users[i]
	if v, ok := body["name"].(string); ok && v != "" {
		u.Name = v
	}
	if v, ok := body["enabled"].(bool); ok {
		u.Enabled = v
	}
	if _, ok := body["expires_at"]; ok {
		if body["expires_at"] == nil {
			u.ExpiresAt = nil
		} else if v, ok := body["expires_at"].(string); ok {
			t, err := parseTimePtr(v)
			if err != nil {
				failJSON(w, 400, "bad_request", err.Error())
				return
			}
			u.ExpiresAt = t
		}
	}
	if _, ok := body["traffic_limit_bytes"]; ok {
		if body["traffic_limit_bytes"] == nil {
			u.TrafficLimitBytes = nil
		} else if v, ok := body["traffic_limit_bytes"].(float64); ok {
			u64 := uint64(v)
			u.TrafficLimitBytes = &u64
		}
	}
	if v, ok := body["traffic_used_bytes"].(float64); ok {
		u.TrafficUsedBytes = uint64(v)
	}
	u.UpdatedAt = time.Now().UTC()
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if err := s.rematerialize(r.Context()); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, redactUser(*u, false))
}

func (s *Service) handleUsersDelete(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	users = append(users[:i], users[i+1:]...)
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	_ = s.rematerialize(r.Context())
	okJSON(w, 200, map[string]any{"deleted": true})
}

func (s *Service) handleUsersRotateToken(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	tok, err := randomToken()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	users[i].SubToken = tok
	users[i].UpdatedAt = time.Now().UTC()
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	path, url := s.subscriptionURL(r, tok)
	okJSON(w, 200, map[string]any{
		"sub_token":         tok,
		"subscription_path": path,
		"subscription_url":  url,
	})
}

func (s *Service) handleUsersRotateCreds(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	users[i].Creds = map[string]map[string]any{}
	if _, err := s.ensureCreds(&users[i]); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	users[i].UpdatedAt = time.Now().UTC()
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	_ = s.rematerialize(r.Context())
	okJSON(w, 200, redactUser(users[i], true))
}

func (s *Service) handlePresetsList(w http.ResponseWriter, _ *http.Request) {
	all := presets.All()
	out := make([]any, 0, len(all))
	for _, p := range all {
		out = append(out, map[string]any{
			"name": p.Name, "protocol": p.Protocol, "description": p.Description, "traits": p.Traits,
		})
	}
	okJSON(w, 200, out)
}

func (s *Service) handlePresetsGet(w http.ResponseWriter, r *http.Request) {
	p, err := presets.Get(r.PathValue("name"))
	if err != nil {
		failJSON(w, 404, "not_found", err.Error())
		return
	}
	okJSON(w, 200, p)
}

func (s *Service) handleDemuxRecipesList(w http.ResponseWriter, _ *http.Request) {
	all := demuxrecipes.All()
	out := make([]any, 0, len(all))
	for _, r := range all {
		out = append(out, map[string]any{
			"name":             r.Name,
			"description":      r.Description,
			"required_presets": r.RequiredPresets,
			"suggested_port":   r.SuggestedPort,
		})
	}
	okJSON(w, 200, out)
}

func (s *Service) handleDemuxRecipesGet(w http.ResponseWriter, r *http.Request) {
	rec, err := demuxrecipes.Get(r.PathValue("name"))
	if err != nil {
		failJSON(w, 404, "not_found", err.Error())
		return
	}
	okJSON(w, 200, rec)
}

func (s *Service) validateSet(set domain.InboundSet, others []domain.InboundSet) error {
	if set.Name == "" || set.ListenPort == 0 || len(set.Presets) == 0 {
		return fmt.Errorf("name, listen_port, presets required")
	}
	if set.Listen == "" {
		set.Listen = "::"
	}
	if !set.HasDemux() && len(set.Presets) != 1 {
		return fmt.Errorf("without demux exactly one preset required")
	}
	for _, pn := range set.Presets {
		if _, err := presets.Get(pn); err != nil {
			return fmt.Errorf("cp_unknown_preset: %w", err)
		}
	}
	if set.HasDemux() {
		if err := demuxrecipes.ValidateTemplate(set.DemuxTemplate); err != nil {
			return fmt.Errorf("cp_invalid_demux: %w", err)
		}
	}
	for _, o := range others {
		if o.Name != set.Name && o.ListenPort == set.ListenPort {
			return fmt.Errorf("cp_port_conflict: port %d in use by %s", set.ListenPort, o.Name)
		}
	}
	return nil
}

func (s *Service) handleSetsList(w http.ResponseWriter, _ *http.Request) {
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	st, _ := s.store.LoadState()
	out := make([]any, 0, len(sets))
	for _, set := range sets {
		m := map[string]any{
			"name": set.Name, "description": set.Description, "listen": set.Listen,
			"listen_port": set.ListenPort, "presets": set.Presets,
			"has_demux": set.HasDemux(), "active": contains(st.ActiveSets, set.Name),
		}
		out = append(out, m)
	}
	okJSON(w, 200, out)
}

func (s *Service) handleSetsCreate(w http.ResponseWriter, r *http.Request) {
	var set domain.InboundSet
	if err := decodeBody(r, &set); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	for _, o := range sets {
		if o.Name == set.Name {
			failJSON(w, 409, "conflict", "set name exists")
			return
		}
	}
	if set.Listen == "" {
		set.Listen = "::"
	}
	if err := s.validateSet(set, sets); err != nil {
		code := 400
		ec := "bad_request"
		if strings.Contains(err.Error(), "cp_port_conflict") {
			code, ec = 409, "cp_port_conflict"
		}
		if strings.Contains(err.Error(), "cp_unknown_preset") {
			ec = "cp_unknown_preset"
		}
		if strings.Contains(err.Error(), "cp_invalid_demux") {
			ec = "cp_invalid_demux"
		}
		failJSON(w, code, ec, err.Error())
		return
	}
	now := time.Now().UTC()
	set.CreatedAt, set.UpdatedAt = now, now
	sets = append(sets, set)
	if err := s.store.SaveSets(sets); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, set)
}

func (s *Service) handleSetsGet(w http.ResponseWriter, r *http.Request) {
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	name := r.PathValue("name")
	for _, set := range sets {
		if set.Name == name {
			okJSON(w, 200, set)
			return
		}
	}
	failJSON(w, 404, "not_found", "set not found")
}

func (s *Service) handleSetsPut(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var set domain.InboundSet
	if err := decodeBody(r, &set); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	set.Name = name
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	idx := -1
	for i := range sets {
		if sets[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		failJSON(w, 404, "not_found", "set not found")
		return
	}
	if set.Listen == "" {
		set.Listen = "::"
	}
	if err := s.validateSet(set, sets); err != nil {
		code, ec := 400, "bad_request"
		if strings.Contains(err.Error(), "cp_port_conflict") {
			code, ec = 409, "cp_port_conflict"
		}
		if strings.Contains(err.Error(), "cp_unknown_preset") {
			ec = "cp_unknown_preset"
		}
		if strings.Contains(err.Error(), "cp_invalid_demux") {
			ec = "cp_invalid_demux"
		}
		failJSON(w, code, ec, err.Error())
		return
	}
	set.CreatedAt = sets[idx].CreatedAt
	set.UpdatedAt = time.Now().UTC()
	sets[idx] = set
	if err := s.store.SaveSets(sets); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	st, _ := s.store.LoadState()
	if contains(st.ActiveSets, name) {
		_ = s.rematerialize(r.Context())
	}
	okJSON(w, 200, set)
}

func (s *Service) handleSetsDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, err := s.store.LoadState()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if contains(st.ActiveSets, name) {
		failJSON(w, 409, "conflict", "deactivate set first")
		return
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	out := make([]domain.InboundSet, 0, len(sets))
	found := false
	for _, set := range sets {
		if set.Name == name {
			found = true
			continue
		}
		out = append(out, set)
	}
	if !found {
		failJSON(w, 404, "not_found", "set not found")
		return
	}
	if err := s.store.SaveSets(out); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{"deleted": true})
}

func (s *Service) handleSetsActivate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var found *domain.InboundSet
	for i := range sets {
		if sets[i].Name == name {
			found = &sets[i]
			break
		}
	}
	if found == nil {
		failJSON(w, 404, "not_found", "set not found")
		return
	}
	if found.HasDemux() {
		// Require with_demux in binary; Start fails with clear error if missing.
	}
	if s.cfg.Owner != nil {
		if err := s.cfg.Owner.Claim(configowner.ModeControlplane); err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
	}
	st, err := s.store.LoadState()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	prev := append([]string{}, st.ActiveSets...)
	if !contains(st.ActiveSets, name) {
		st.ActiveSets = append(st.ActiveSets, name)
		if err := s.store.SaveState(st); err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
	}
	if err := s.rematerialize(r.Context()); err != nil {
		st.ActiveSets = prev
		_ = s.store.SaveState(st)
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	mode := string(configowner.ModeControlplane)
	if s.cfg.Owner != nil {
		mode = string(s.cfg.Owner.Owner())
	}
	st, _ = s.store.LoadState()
	okJSON(w, 200, map[string]any{"active_sets": st.ActiveSets, "config_mode": mode})
}

func (s *Service) handleSetsDeactivate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, err := s.store.LoadState()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if !contains(st.ActiveSets, name) {
		// Idempotent deactivate: already inactive is OK.
		mode := "idle"
		if s.cfg.Owner != nil {
			mode = string(s.cfg.Owner.Owner())
		}
		okJSON(w, 200, map[string]any{"active_sets": st.ActiveSets, "config_mode": mode, "noop": true})
		return
	}
	st.ActiveSets = removeStr(st.ActiveSets, name)
	if err := s.store.SaveState(st); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if len(st.ActiveSets) == 0 {
		if s.cfg.Owner != nil {
			_ = s.cfg.Owner.Claim(configowner.ModeIdle)
		}
	} else {
		_ = s.rematerialize(r.Context())
	}
	mode := "idle"
	if s.cfg.Owner != nil {
		mode = string(s.cfg.Owner.Owner())
	}
	okJSON(w, 200, map[string]any{"active_sets": st.ActiveSets, "config_mode": mode})
}

func (s *Service) handleTLSGet(w http.ResponseWriter, _ *http.Request) {
	p, err := s.ensureTLSProfile(false)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, s.tlsStatusPayload(p))
}

func (s *Service) handleTLSPut(w http.ResponseWriter, r *http.Request) {
	var p domain.TLSProfile
	if err := decodeBody(r, &p); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if err := p.Validate(); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if err := s.store.SaveTLSProfile(p); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	forceReload := false
	if p.Mode == domain.TLSModeSelfSigned && p.SelfSigned != nil {
		if _, _, changed, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, false); err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		} else {
			forceReload = changed
		}
	} else {
		// Mode switch away from self_signed still needs Apply when CP owns the box.
		forceReload = true
		if err := s.ensureSafetySelfSignedPEMs(p); err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
		if p.Mode == domain.TLSModeACMEDomain || p.Mode == domain.TLSModeACMEIP {
			s.noteACMEModeEntered()
		}
	}
	s.mgmtTLS.mu.Lock()
	s.mgmtTLS.cert = nil
	s.mgmtTLS.mu.Unlock()
	if err := s.rematerializeForce(r.Context(), forceReload); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, s.tlsStatusPayload(p))
}

func (s *Service) handleTLSRegenerate(w http.ResponseWriter, r *http.Request) {
	p, err := s.ensureTLSProfile(false)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if p.Mode != domain.TLSModeSelfSigned || p.SelfSigned == nil {
		failJSON(w, 400, "bad_request", "regenerate only for self_signed mode")
		return
	}
	if _, _, _, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, true); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if err := s.rematerializeForce(r.Context(), true); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, s.tlsStatusPayload(p))
}

func (s *Service) tlsStatusPayload(p domain.TLSProfile) map[string]any {
	out := map[string]any{
		"mode":        p.Mode,
		"self_signed": p.SelfSigned,
		"acme":        redactACME(p.ACME),
	}
	cert, key := tlsMaterialPaths(s.cfg.DataDir)
	_, certErr := os.Stat(cert)
	_, keyErr := os.Stat(key)
	pemPresent := certErr == nil && keyErr == nil
	status := map[string]any{
		"mode":                p.Mode,
		"acme_data_directory": acmeDataDirectory(s.cfg.DataDir),
	}
	switch p.Mode {
	case domain.TLSModeSelfSigned:
		status["self_signed_cert_present"] = pemPresent
		status["cert_path"] = cert
		status["key_path"] = key
		status["active_material"] = "self_signed_pem"
		status["ready"] = pemPresent
		if !pemPresent {
			status["ready_reason"] = "self_signed pem missing"
		}
	case domain.TLSModeACMEDomain, domain.TLSModeACMEIP:
		status["self_signed_cert_present"] = pemPresent // orphan files may remain
		status["active_material"] = "certificate_provider"
		status["certificate_provider_tag"] = domain.TLSProviderTag
		domains := []string{}
		if p.ACME != nil {
			domains = p.ACME.Domains
		}
		ready, missing, found := acmeCertificateReady(s.cfg.DataDir, domains)
		s.noteACMEReady(ready)
		status["ready"] = ready
		status["acme_certs_found"] = found
		status["acme_certs_missing"] = missing
		if !ready {
			status["ready_reason"] = "waiting for ACME obtain (certmagic)"
		}
	default:
		status["active_material"] = "unknown"
		status["ready"] = false
		status["ready_reason"] = "unknown mode"
	}
	status["mgmt_https"] = true
	status["mgmt_cert_source"] = s.mgmtCertSource()
	if _, _, src, err := s.mgmtMaterialPaths(); err == nil {
		status["mgmt_cert_source"] = src
	}
	out["material_status"] = status
	return out
}

func redactACME(a *domain.ACMESpec) any {
	if a == nil {
		return nil
	}
	cp := *a
	if len(cp.DNS01Challenge) > 0 {
		red := map[string]any{}
		for k, v := range cp.DNS01Challenge {
			ks := strings.ToLower(k)
			if strings.Contains(ks, "token") || strings.Contains(ks, "secret") || strings.Contains(ks, "password") || strings.Contains(ks, "key") {
				red[k] = "[redacted]"
			} else {
				red[k] = v
			}
		}
		cp.DNS01Challenge = red
	}
	return cp
}

func (s *Service) handleSub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		failJSON(w, 405, "method_not_allowed", "GET only")
		return
	}
	token := r.PathValue("token")
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var user *domain.User
	for i := range users {
		if subtleConstantTimeEq(users[i].SubToken, token) {
			user = &users[i]
			break
		}
	}
	if user == nil {
		failJSON(w, 404, "not_found", "unknown token")
		return
	}
	if !user.Eligible(time.Now().UTC()) {
		failJSON(w, 403, "cp_user_ineligible", "user ineligible")
		return
	}
	sets, err := s.activeSetObjects()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if len(sets) == 0 {
		failJSON(w, 409, "cp_no_active_set", "no active sets")
		return
	}
	_, _ = s.ensureCreds(user)
	filterSet := r.URL.Query().Get("set")
	filterPreset := r.URL.Query().Get("preset")
	host := s.publicHost(r)
	profile, err := s.ensureTLSProfile(false)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	body, err := materialize.RenderSubscription(*user, sets, host, profile, filterSet, filterPreset)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

func subtleConstantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
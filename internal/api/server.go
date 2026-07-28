package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/auth"
	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/heartbeat"
	"github.com/ne-tort/sing-box-subserver/internal/subscribe"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"github.com/ne-tort/sing-box-subserver/internal/version"
)

// Server is the management HTTP API.
type Server struct {
	Cfg         *agentcfg.Config
	Supervisor  *supervisor.Supervisor
	Obs         *obs.Observability
	Subscribe   *subscribe.Manager
	Heartbeat   *heartbeat.Pusher
	Auth        *auth.Store
	Owner       *configowner.Registry
	Controlplane ControlplaneHandler // optional; nil without with_controlplane
	mux         *http.ServeMux
}

// ControlplaneHandler mounts optional embedded CP routes.
type ControlplaneHandler interface {
	Register(mux *http.ServeMux, requireAuth func(func(http.ResponseWriter, *http.Request)) http.HandlerFunc)
}

// MgmtTLSProvider is optionally implemented by controlplane to terminate
// management/subscription HTTPS from the CP TLS profile.
type MgmtTLSProvider interface {
	ServingHTTPS() bool
	GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error)
}

func New(cfg *agentcfg.Config, sup *supervisor.Supervisor, o *obs.Observability) *Server {
	s := &Server{Cfg: cfg, Supervisor: sup, Obs: o, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return recoverMiddleware(s.Obs, s.mux)
}

func recoverMiddleware(o *obs.Observability, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if o != nil && o.Logger != nil {
					o.Logger.Error("http panic recovered", "err", rec, "path", r.URL.Path)
				}
				Fail(w, http.StatusInternalServerError, "panic", "internal error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /v1/health", s.requireAuthOptional(s.handleHealth))
	s.mux.HandleFunc("GET /v1/version", s.requireAuthOptional(s.handleVersion))
	s.mux.HandleFunc("GET /v1/ready", s.requireAuth(s.handleReady))
	s.mux.HandleFunc("GET /v1/status", s.requireAuth(s.handleStatus))
	s.mux.HandleFunc("GET /v1/config", s.requireAuth(s.handleGetConfig))
	s.mux.HandleFunc("PUT /v1/config", s.requireAuth(s.handlePutConfig))
	s.mux.HandleFunc("POST /v1/validate", s.requireAuth(s.handleValidate))
	s.mux.HandleFunc("GET /v1/logs", s.requireAuth(s.handleLogs))
	s.mux.HandleFunc("GET /v1/metrics", s.requireAuth(s.handleMetrics))
	s.mux.HandleFunc("POST /v1/box/stop", s.requireAuth(s.handleBoxStop))
	s.mux.HandleFunc("POST /v1/box/start", s.requireAuth(s.handleBoxStart))
	s.mux.HandleFunc("GET /v1/subscribe", s.requireAuth(s.handleSubscribeGet))
	s.mux.HandleFunc("POST /v1/subscribe", s.requireAuth(s.handleSubscribePost))
	s.mux.HandleFunc("DELETE /v1/subscribe", s.requireAuth(s.handleSubscribeDelete))
	s.mux.HandleFunc("POST /v1/subscribe/refresh", s.requireAuth(s.handleSubscribeRefresh))
	// Alias: pull == subscribe (same runtime manager).
	s.mux.HandleFunc("GET /v1/pull", s.requireAuth(s.handleSubscribeGet))
	s.mux.HandleFunc("PUT /v1/pull", s.requireAuth(s.handleSubscribePost))
	s.mux.HandleFunc("DELETE /v1/pull", s.requireAuth(s.handleSubscribeDelete))
	s.mux.HandleFunc("POST /v1/pull/refresh", s.requireAuth(s.handleSubscribeRefresh))
	s.mux.HandleFunc("GET /v1/heartbeat", s.requireAuth(s.handleHeartbeatGet))
	s.mux.HandleFunc("PUT /v1/heartbeat", s.requireAuth(s.handleHeartbeatPut))
	s.mux.HandleFunc("DELETE /v1/heartbeat", s.requireAuth(s.handleHeartbeatDelete))
	s.mux.HandleFunc("GET /v1/auth/tokens", s.requireAuth(s.handleAuthList))
	s.mux.HandleFunc("POST /v1/auth/tokens", s.requireAuth(s.handleAuthCreate))
	s.mux.HandleFunc("DELETE /v1/auth/tokens/{id}", s.requireAuth(s.handleAuthDelete))
	s.mux.HandleFunc("POST /v1/auth/rotate", s.requireAuth(s.handleAuthRotate))
	s.mux.HandleFunc("POST /v1/auth/bootstrap/disable", s.requireAuth(s.handleAuthBootstrapDisable))
}

// SetControlplane wires optional CP routes (call after New; routes are additive).
func (s *Server) SetControlplane(h ControlplaneHandler) {
	s.Controlplane = h
	if h != nil {
		h.Register(s.mux, func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
			return s.requireAuth(handlerFunc(next))
		})
	}
}

type handlerFunc func(http.ResponseWriter, *http.Request)

func (s *Server) authorize(r *http.Request) bool {
	if s.Auth != nil {
		return s.Auth.Authorize(r)
	}
	return auth.Bearer(r, s.Cfg.Token)
}

func (s *Server) requireAuth(next handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorize(r) {
			Fail(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token", nil)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAuthOptional(next handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.Cfg.HealthPublicEnabled() && !s.authorize(r) {
			Fail(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token", nil)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	OK(w, http.StatusOK, map[string]string{"status": "alive"})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	OK(w, http.StatusOK, versionPayload())
}

func versionPayload() map[string]any {
	return map[string]any{
		"agent_version":   version.AgentVersion,
		"agent_commit":    version.AgentCommit,
		"singbox_version": version.SingBoxVersion(),
		"singbox_commit":  version.SingBoxCommit,
		"build_tags":      version.BuildTags,
	}
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	st := s.Supervisor.Status()
	if st.State == supervisor.StateRunning {
		OK(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	Fail(w, http.StatusServiceUnavailable, "not_ready", "dataplane not ready", map[string]string{"state": string(st.State)})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	st := s.Supervisor.Status()
	started := s.Supervisor.ProcessStartedAt()
	data := map[string]any{
		"state":              st.State,
		"node_id":            s.Cfg.NodeID,
		"listen":             s.Cfg.Listen,
		"agent_version":      version.AgentVersion,
		"agent_commit":       version.AgentCommit,
		"singbox_version":    version.SingBoxVersion(),
		"singbox_commit":     version.SingBoxCommit,
		"build_tags":         version.BuildTags,
		"revision":           st.Revision,
		"content_sha256":     st.ContentSHA256,
		"box_started_at":     st.BoxStartedAt,
		"process_started_at": started.UTC().Format(time.RFC3339Nano),
		"uptime_sec":         int64(time.Since(started).Seconds()),
		"last_apply":         st.LastApply,
		"last_error":         st.LastError,
		"pull":               st.Pull,
		"box_up":             st.BoxUp,
	}
	if s.Subscribe != nil {
		data["subscribe"] = s.Subscribe.Status()
	}
	if s.Owner != nil {
		data["config_mode"] = string(s.Owner.Owner())
	}
	if s.Heartbeat != nil {
		data["heartbeat"] = s.Heartbeat.Status()
	}
	if s.Auth != nil {
		data["auth"] = map[string]any{
			"bootstrap_enabled": s.Auth.BootstrapEnabled(),
			"active_count":      s.Auth.CountActive(),
		}
	}
	OK(w, http.StatusOK, data)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	raw, meta, err := s.Supervisor.LastGoodConfig()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			Fail(w, http.StatusNotFound, "not_found", "no last-good config", nil)
			return
		}
		mapSupervisorErr(w, err)
		return
	}
	w.Header().Set("ETag", `"sha256:`+meta.ContentSHA256+`"`)
	w.Header().Set("X-Revision", strconv.FormatUint(meta.Revision, 10))
	if r.URL.Query().Get("meta") == "1" {
		OK(w, http.StatusOK, meta)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	raw, source, err := readConfigBody(r)
	if err != nil {
		Fail(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	req := supervisor.ApplyRequest{Raw: raw, Source: source}
	if m := r.Header.Get("If-Match"); m != "" {
		m = strings.Trim(m, `"`)
		if strings.HasPrefix(m, "sha256:") {
			req.MatchMode = supervisor.MatchSHA256
			req.MatchSHA = strings.TrimPrefix(m, "sha256:")
		} else {
			rev, err := strconv.ParseUint(m, 10, 64)
			if err != nil {
				Fail(w, http.StatusBadRequest, "bad_request", "invalid If-Match", nil)
				return
			}
			req.MatchMode = supervisor.MatchRevision
			req.MatchRev = rev
		}
	}
	res, err := s.Supervisor.Apply(r.Context(), req)
	if err != nil {
		mapSupervisorErr(w, err)
		return
	}
	if s.Owner != nil {
		_ = s.Owner.Claim(configowner.ModeDirect)
	} else if s.Subscribe != nil {
		s.Subscribe.CancelOnDirectConfig()
	}
	mode := "direct"
	if s.Owner != nil {
		mode = string(s.Owner.Owner())
	}
	OK(w, http.StatusOK, map[string]any{
		"revision":       res.Revision,
		"content_sha256": res.SHA256,
		"noop":           res.Noop,
		"state":          res.State,
		"config_mode":    mode,
	})
}

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	raw, _, err := readConfigBody(r)
	if err != nil {
		Fail(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	if err := s.Supervisor.Validate(r.Context(), raw); err != nil {
		mapSupervisorErr(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]string{"status": "valid"})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var since uint64
	if v := q.Get("since"); v != "" {
		since, _ = strconv.ParseUint(strings.TrimPrefix(v, "seq-"), 10, 64)
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	entries, next := s.Obs.Ring.Query(since, q.Get("level"), limit)
	OK(w, http.StatusOK, map[string]any{
		"next":    "seq-" + strconv.FormatUint(next, 10),
		"entries": entries,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	st := s.Supervisor.Status()
	var uptime float64
	if st.BoxStartedAt != nil {
		uptime = time.Since(*st.BoxStartedAt).Seconds()
	}
	format := r.URL.Query().Get("format")
	if format == "" || format == "prometheus" {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, s.Obs.Metrics.PrometheusText(st.BoxUp, uptime, st.Revision))
		return
	}
	snap := s.Obs.Metrics.Snapshot()
	ps := obs.ReadProcessStats()
	OK(w, http.StatusOK, map[string]any{
		"process": map[string]any{
			"cpu_percent": ps.CPUPercent,
			"rss_bytes":   ps.RSSBytes,
			"goroutines":  ps.Goroutines,
		},
		"box": map[string]any{
			"uptime_sec": uptime,
			"state":      st.State,
		},
		"apply_total":       snap.ApplyTotal,
		"apply_fail_total":  snap.ApplyFailTotal,
		"rollback_total":    snap.RollbackTotal,
		"box_restart_total": snap.BoxRestartTotal,
		"config_revision":   st.Revision,
	})
}

func (s *Server) handleBoxStop(w http.ResponseWriter, _ *http.Request) {
	if err := s.Supervisor.StopBox(); err != nil {
		mapSupervisorErr(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) handleBoxStart(w http.ResponseWriter, r *http.Request) {
	if err := s.Supervisor.StartBox(r.Context()); err != nil {
		mapSupervisorErr(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]string{"status": "started"})
}

func (s *Server) handleAuthList(w http.ResponseWriter, _ *http.Request) {
	if s.Auth == nil {
		Fail(w, http.StatusNotFound, "not_found", "auth store unavailable", nil)
		return
	}
	OK(w, http.StatusOK, map[string]any{
		"tokens":            s.Auth.List(),
		"bootstrap_enabled": s.Auth.BootstrapEnabled(),
		"active_count":      s.Auth.CountActive(),
	})
}

func (s *Server) handleAuthCreate(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		Fail(w, http.StatusNotFound, "not_found", "auth store unavailable", nil)
		return
	}
	var body struct {
		Name  string `json:"name"`
		Token string `json:"token"` // optional: panel-chosen secret
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		Fail(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	view, secret, err := s.Auth.Create(body.Name, body.Token)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{
		"id":         view.ID,
		"name":       view.Name,
		"created_at": view.CreatedAt,
		"token":      secret, // shown once
	})
}

func (s *Server) handleAuthDelete(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		Fail(w, http.StatusNotFound, "not_found", "auth store unavailable", nil)
		return
	}
	id := r.PathValue("id")
	if err := s.Auth.Delete(id); err != nil {
		mapAuthErr(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{"deleted": id, "tokens": s.Auth.List()})
}

func (s *Server) handleAuthRotate(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		Fail(w, http.StatusNotFound, "not_found", "auth store unavailable", nil)
		return
	}
	var body struct {
		Name         string `json:"name"`
		RevokeOthers bool   `json:"revoke_others"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	if body.Name == "" {
		body.Name = "panel"
	}
	view, secret, err := s.Auth.Rotate(body.Name, body.RevokeOthers)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	if s.Subscribe != nil {
		s.Subscribe.SetOutboundToken(secret)
	}
	if s.Heartbeat != nil {
		s.Heartbeat.SetOutboundToken(secret)
	}
	OK(w, http.StatusOK, map[string]any{
		"id":            view.ID,
		"name":          view.Name,
		"created_at":    view.CreatedAt,
		"token":         secret,
		"revoke_others": body.RevokeOthers,
		"tokens":        s.Auth.List(),
	})
}

func (s *Server) handleAuthBootstrapDisable(w http.ResponseWriter, _ *http.Request) {
	if s.Auth == nil {
		Fail(w, http.StatusNotFound, "not_found", "auth store unavailable", nil)
		return
	}
	if err := s.Auth.DisableBootstrap(); err != nil {
		mapAuthErr(w, err)
		return
	}
	OK(w, http.StatusOK, map[string]any{
		"bootstrap_enabled": false,
		"tokens":            s.Auth.List(),
	})
}

func mapAuthErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		Fail(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, auth.ErrLastCredential), errors.Is(err, auth.ErrNeedManaged):
		Fail(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, auth.ErrInvalidToken), errors.Is(err, auth.ErrBootstrapOff):
		Fail(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
	default:
		Fail(w, http.StatusInternalServerError, "internal", err.Error(), nil)
	}
}

func (s *Server) handleSubscribeGet(w http.ResponseWriter, _ *http.Request) {
	if s.Subscribe == nil {
		Fail(w, http.StatusNotFound, "not_found", "subscribe not available", nil)
		return
	}
	OK(w, http.StatusOK, s.Subscribe.Status())
}

func (s *Server) handleSubscribePost(w http.ResponseWriter, r *http.Request) {
	if s.Subscribe == nil {
		Fail(w, http.StatusNotFound, "not_found", "subscribe not available", nil)
		return
	}
	var body subscribe.Spec
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		Fail(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	if err := s.Subscribe.Subscribe(body); err != nil {
		Fail(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	// Immediate apply (same as refresh) so mode flips and config is live.
	// Spec is already persisted: if the controller is unreachable (common for
	// local panel / private network), still return 200 with fetch_error so
	// callers can update Authorization headers without a hard failure.
	var fetchErr string
	if err := s.Subscribe.Refresh(r.Context()); err != nil {
		fetchErr = err.Error()
	}
	// Enabling subscribe claims ownership even if the first fetch fails: the
	// schedule is the writer now (ADR 0008). fetch_error is reported separately.
	if s.Owner != nil {
		_ = s.Owner.Claim(configowner.ModeSubscribed)
	}
	st := s.Supervisor.Status()
	mode := "subscribed"
	if s.Owner != nil {
		mode = string(s.Owner.Owner())
	}
	out := map[string]any{
		"subscribe":      s.Subscribe.Status(),
		"revision":       st.Revision,
		"content_sha256": st.ContentSHA256,
		"state":          st.State,
		"config_mode":    mode,
	}
	if fetchErr != "" {
		out["fetch_error"] = fetchErr
		out["fetch_ok"] = false
	} else {
		out["fetch_ok"] = true
	}
	OK(w, http.StatusOK, out)
}

func (s *Server) handleSubscribeDelete(w http.ResponseWriter, _ *http.Request) {
	if s.Subscribe == nil {
		Fail(w, http.StatusNotFound, "not_found", "subscribe not available", nil)
		return
	}
	if err := s.Subscribe.Unsubscribe(); err != nil {
		Fail(w, http.StatusInternalServerError, "internal", err.Error(), nil)
		return
	}
	// Only displace ownership when we were the subscribe writer. DELETE must not
	// wipe controlplane/direct ownership (ADR 0008).
	if s.Owner != nil && s.Owner.Owner() == configowner.ModeSubscribed {
		_ = s.Owner.Claim(configowner.ModeIdle)
	}
	out := map[string]any{
		"subscribe": s.Subscribe.Status(),
	}
	if s.Owner != nil {
		out["config_mode"] = string(s.Owner.Owner())
	}
	OK(w, http.StatusOK, out)
}

func (s *Server) handleSubscribeRefresh(w http.ResponseWriter, r *http.Request) {
	if s.Subscribe == nil {
		Fail(w, http.StatusNotFound, "not_found", "subscribe not available", nil)
		return
	}
	if err := s.Subscribe.Refresh(r.Context()); err != nil {
		if errors.Is(err, subscribe.ErrNotSubscribed) {
			Fail(w, http.StatusConflict, "not_subscribed", "subscription is not active", nil)
			return
		}
		Fail(w, http.StatusBadGateway, "subscribe_fetch_failed", err.Error(), s.Subscribe.Status())
		return
	}
	st := s.Supervisor.Status()
	OK(w, http.StatusOK, map[string]any{
		"subscribe":      s.Subscribe.Status(),
		"revision":       st.Revision,
		"content_sha256": st.ContentSHA256,
		"state":          st.State,
	})
}

func (s *Server) handleHeartbeatGet(w http.ResponseWriter, _ *http.Request) {
	if s.Heartbeat == nil {
		Fail(w, http.StatusNotFound, "not_found", "heartbeat not available", nil)
		return
	}
	OK(w, http.StatusOK, s.Heartbeat.Status())
}

func (s *Server) handleHeartbeatPut(w http.ResponseWriter, r *http.Request) {
	if s.Heartbeat == nil {
		Fail(w, http.StatusNotFound, "not_found", "heartbeat not available", nil)
		return
	}
	var body struct {
		URL         string            `json:"url"`
		IntervalSec int               `json:"interval_sec"`
		TimeoutSec  int               `json:"timeout_sec"`
		Headers     map[string]string `json:"headers"`
		Enabled     *bool             `json:"enabled"`
		TLSInsecure bool              `json:"tls_insecure"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		Fail(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	en := true
	if body.Enabled != nil {
		en = *body.Enabled
	}
	if err := s.Heartbeat.Configure(heartbeat.Spec{
		URL:         body.URL,
		IntervalSec: body.IntervalSec,
		TimeoutSec:  body.TimeoutSec,
		Headers:     body.Headers,
		TLSInsecure: body.TLSInsecure,
	}, en); err != nil {
		Fail(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	OK(w, http.StatusOK, s.Heartbeat.Status())
}

func (s *Server) handleHeartbeatDelete(w http.ResponseWriter, _ *http.Request) {
	if s.Heartbeat == nil {
		Fail(w, http.StatusNotFound, "not_found", "heartbeat not available", nil)
		return
	}
	if err := s.Heartbeat.Disable(); err != nil {
		Fail(w, http.StatusInternalServerError, "internal", err.Error(), nil)
		return
	}
	OK(w, http.StatusOK, s.Heartbeat.Status())
}

func readConfigBody(r *http.Request) ([]byte, configstore.Source, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 {
		return nil, "", errors.New("empty body")
	}
	source := configstore.SourcePush
	var wrap struct {
		Config       json.RawMessage `json:"config"`
		Source       string          `json:"source"`
		RevisionHint string          `json:"revision_hint"`
	}
	if err := json.Unmarshal(body, &wrap); err == nil && len(wrap.Config) > 0 {
		if wrap.Source == string(configstore.SourcePull) {
			source = configstore.SourcePull
		}
		return wrap.Config, source, nil
	}
	return body, source, nil
}

// ListenAndServe starts the management server (blocking).
// Prefer controlplane TLS profile (HTTPS) when available; else agent.yaml tls.cert/key;
// else plain HTTP (labs with insecure_public_bind).
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.Cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	useTLS := false
	var getCert func(*tls.ClientHelloInfo) (*tls.Certificate, error)
	if p, ok := s.Controlplane.(MgmtTLSProvider); ok && p != nil && p.ServingHTTPS() {
		useTLS = true
		getCert = p.GetCertificate
	} else if s.Cfg.TLS.Cert != "" {
		useTLS = true
	}
	if useTLS {
		srv.TLSConfig = &tls.Config{
			MinVersion:     tls.VersionTLS12,
			GetCertificate: getCert,
		}
	}

	errCh := make(chan error, 1)
	go func() {
		var err error
		if useTLS {
			ln, lerr := net.Listen("tcp", s.Cfg.Listen)
			if lerr != nil {
				errCh <- lerr
				return
			}
			if getCert != nil {
				err = srv.ServeTLS(ln, "", "")
			} else {
				err = srv.ServeTLS(ln, s.Cfg.TLS.Cert, s.Cfg.TLS.Key)
			}
		} else {
			err = srv.ListenAndServe()
		}
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

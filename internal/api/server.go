package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/auth"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"github.com/ne-tort/sing-box-subserver/internal/version"
)

// Server is the management HTTP API.
type Server struct {
	Cfg        *agentcfg.Config
	Supervisor *supervisor.Supervisor
	Obs        *obs.Observability
	mux        *http.ServeMux
}

func New(cfg *agentcfg.Config, sup *supervisor.Supervisor, o *obs.Observability) *Server {
	s := &Server{Cfg: cfg, Supervisor: sup, Obs: o, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

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
}

type handlerFunc func(http.ResponseWriter, *http.Request)

func (s *Server) requireAuth(next handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !auth.Bearer(r, s.Cfg.Token) {
			Fail(w, http.StatusUnauthorized, "unauthorized", "invalid or missing bearer token", nil)
			return
		}
		next(w, r)
	}
}

func (s *Server) requireAuthOptional(next handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.Cfg.HealthPublicEnabled() && !auth.Bearer(r, s.Cfg.Token) {
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
	data := map[string]any{
		"state":              st.State,
		"node_id":            s.Cfg.NodeID,
		"agent_version":      version.AgentVersion,
		"agent_commit":       version.AgentCommit,
		"singbox_version":    version.SingBoxVersion(),
		"singbox_commit":     version.SingBoxCommit,
		"build_tags":         version.BuildTags,
		"revision":           st.Revision,
		"content_sha256":     st.ContentSHA256,
		"box_started_at":     st.BoxStartedAt,
		"process_started_at": s.Supervisor.ProcessStartedAt().UTC().Format(time.RFC3339Nano),
		"last_apply":         st.LastApply,
		"last_error":         st.LastError,
		"pull":               st.Pull,
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
	OK(w, http.StatusOK, map[string]any{
		"revision":        res.Revision,
		"content_sha256":  res.SHA256,
		"noop":            res.Noop,
		"state":           res.State,
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
	format := r.URL.Query().Get("format")
	if format == "" || format == "prometheus" {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, s.Obs.Metrics.PrometheusText())
		return
	}
	st := s.Supervisor.Status()
	snap := s.Obs.Metrics.Snapshot()
	OK(w, http.StatusOK, map[string]any{
		"process":           map[string]any{},
		"box":               map[string]any{"state": st.State},
		"apply_total":       snap.ApplyTotal,
		"apply_fail_total":  snap.ApplyFailTotal,
		"rollback_total":    snap.RollbackTotal,
		"box_restart_total": snap.BoxRestartTotal,
	})
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
func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.Cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		var err error
		if s.Cfg.TLS.Cert != "" {
			err = srv.ListenAndServeTLS(s.Cfg.TLS.Cert, s.Cfg.TLS.Key)
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

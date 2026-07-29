//go:build with_traffic

package portal

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/service"
)

func Register(mux *http.ServeMux, svc *service.Service, requireAuth func(func(http.ResponseWriter, *http.Request)) http.HandlerFunc) {
	if mux == nil || svc == nil {
		return
	}
	mux.HandleFunc("GET /v1/traffic/status", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, svc.StatusPayload())
	}))
	mux.HandleFunc("GET /v1/traffic/subjects", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, map[string]any{"subjects": svc.Subjects()})
	}))
	mux.HandleFunc("GET /v1/traffic/stats", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var since time.Time
		if s := q.Get("since"); s != "" {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				since = t
			}
		}
		if since.IsZero() {
			since = time.Now().UTC().Add(-24 * time.Hour)
		}
		okJSON(w, svc.StatsQuery(q.Get("subject"), domain.SeriesType(q.Get("series_type")), q.Get("key"), since))
	}))
	mux.HandleFunc("GET /v1/traffic/onlines", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, svc.Onlines())
	}))
	mux.HandleFunc("PUT /v1/traffic/limits", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Limits map[string]domain.SpeedLimit `json:"limits"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			failJSON(w, 400, "bad_request", err.Error())
			return
		}
		svc.SetLimits(body.Limits)
		okJSON(w, map[string]any{"ok": true})
	}))
}

func okJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

func failJSON(w http.ResponseWriter, code int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]any{"type": typ, "message": msg}})
}

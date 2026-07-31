package obs

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds process counters for apply / rollback / restarts.
type Metrics struct {
	ApplyOK       atomic.Uint64
	ApplyFail     atomic.Uint64
	RollbackTotal atomic.Uint64
	BoxRestarts   atomic.Uint64
}

func (m *Metrics) IncApply(ok bool) {
	if ok {
		m.ApplyOK.Add(1)
	} else {
		m.ApplyFail.Add(1)
	}
}

// Snapshot is a JSON-friendly metrics view.
type Snapshot struct {
	ApplyTotal      uint64 `json:"apply_total"`
	ApplyFailTotal  uint64 `json:"apply_fail_total"`
	RollbackTotal   uint64 `json:"rollback_total"`
	BoxRestartTotal uint64 `json:"box_restart_total"`
}

func (m *Metrics) Snapshot() Snapshot {
	ok := m.ApplyOK.Load()
	fail := m.ApplyFail.Load()
	return Snapshot{
		ApplyTotal:      ok + fail,
		ApplyFailTotal:  fail,
		RollbackTotal:   m.RollbackTotal.Load(),
		BoxRestartTotal: m.BoxRestarts.Load(),
	}
}

// ProcessStats is a cheap runtime snapshot (no OS RSS sampler).
type ProcessStats struct {
	Goroutines int    `json:"goroutines"`
	RSSBytes   uint64 `json:"rss_bytes"`
	CPUPercent float64 `json:"cpu_percent"`
}

func ReadProcessStats() ProcessStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ProcessStats{
		Goroutines: runtime.NumGoroutine(),
		RSSBytes:   ms.Sys,
		CPUPercent: 0,
	}
}

// PrometheusText renders counters plus optional live gauges.
func (m *Metrics) PrometheusText(boxUp bool, boxUptimeSec float64, revision uint64) string {
	s := m.Snapshot()
	ps := ReadProcessStats()
	var b strings.Builder
	b.WriteString("# HELP subserver_apply_total Total config applies by result\n")
	b.WriteString("# TYPE subserver_apply_total counter\n")
	b.WriteString("subserver_apply_total{result=\"ok\"} ")
	b.WriteString(strconv.FormatUint(s.ApplyTotal-s.ApplyFailTotal, 10))
	b.WriteByte('\n')
	b.WriteString("subserver_apply_total{result=\"fail\"} ")
	b.WriteString(strconv.FormatUint(s.ApplyFailTotal, 10))
	b.WriteByte('\n')
	b.WriteString("# HELP subserver_rollback_total Last-good restores after failed apply\n")
	b.WriteString("# TYPE subserver_rollback_total counter\n")
	b.WriteString("subserver_rollback_total ")
	b.WriteString(strconv.FormatUint(s.RollbackTotal, 10))
	b.WriteByte('\n')
	b.WriteString("# HELP subserver_box_restart_total Unexpected box restarts\n")
	b.WriteString("# TYPE subserver_box_restart_total counter\n")
	b.WriteString("subserver_box_restart_total ")
	b.WriteString(strconv.FormatUint(s.BoxRestartTotal, 10))
	b.WriteByte('\n')
	b.WriteString("# HELP subserver_box_up Dataplane up (1) or down (0)\n")
	b.WriteString("# TYPE subserver_box_up gauge\n")
	if boxUp {
		b.WriteString("subserver_box_up 1\n")
	} else {
		b.WriteString("subserver_box_up 0\n")
	}
	b.WriteString("# HELP subserver_box_uptime_seconds Box uptime seconds\n")
	b.WriteString("# TYPE subserver_box_uptime_seconds gauge\n")
	b.WriteString("subserver_box_uptime_seconds ")
	b.WriteString(strconv.FormatFloat(boxUptimeSec, 'f', 1, 64))
	b.WriteByte('\n')
	b.WriteString("# HELP subserver_config_revision Current config revision\n")
	b.WriteString("# TYPE subserver_config_revision gauge\n")
	b.WriteString("subserver_config_revision ")
	b.WriteString(strconv.FormatUint(revision, 10))
	b.WriteByte('\n')
	b.WriteString("# HELP subserver_goroutines Goroutine count\n")
	b.WriteString("# TYPE subserver_goroutines gauge\n")
	b.WriteString("subserver_goroutines ")
	b.WriteString(strconv.Itoa(ps.Goroutines))
	b.WriteByte('\n')
	b.WriteString("# HELP subserver_process_rss_bytes Approx process memory (MemStats.Sys)\n")
	b.WriteString("# TYPE subserver_process_rss_bytes gauge\n")
	b.WriteString("subserver_process_rss_bytes ")
	b.WriteString(strconv.FormatUint(ps.RSSBytes, 10))
	b.WriteByte('\n')
	return b.String()
}

// Entry is one ring-buffer log line.
type Entry struct {
	Seq    uint64         `json:"seq"`
	TS     time.Time      `json:"ts"`
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields,omitempty"`
}

// Ring is a fixed-size concurrent log buffer.
type Ring struct {
	mu      sync.RWMutex
	entries []Entry
	cap     int
	nextSeq uint64
}

func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = 2000
	}
	return &Ring{cap: capacity, entries: make([]Entry, 0, capacity)}
}

func (r *Ring) Append(level, msg string, fields map[string]any) Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextSeq++
	e := Entry{
		Seq:    r.nextSeq,
		TS:     time.Now().UTC(),
		Level:  level,
		Msg:    msg,
		Fields: fields,
	}
	if len(r.entries) >= r.cap {
		r.entries = r.entries[1:]
	}
	r.entries = append(r.entries, e)
	return e
}

// Query returns entries with seq > sinceSeq, filtered by level, limited.
func (r *Ring) Query(sinceSeq uint64, level string, limit int) (entries []Entry, next uint64) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	level = strings.ToLower(strings.TrimSpace(level))
	out := make([]Entry, 0, limit)
	for _, e := range r.entries {
		if e.Seq <= sinceSeq {
			continue
		}
		if level != "" && !strings.EqualFold(e.Level, level) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	next = sinceSeq
	if len(out) > 0 {
		next = out[len(out)-1].Seq
	} else if r.nextSeq > 0 {
		next = r.nextSeq
	}
	return out, next
}

type ringHandler struct {
	inner slog.Handler
	ring  *Ring
}

func (h *ringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *ringHandler) Handle(ctx context.Context, rec slog.Record) error {
	fields := make(map[string]any)
	rec.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.Any()
		return true
	})
	h.ring.Append(strings.ToLower(rec.Level.String()), rec.Message, fields)
	return h.inner.Handle(ctx, rec)
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ringHandler{inner: h.inner.WithAttrs(attrs), ring: h.ring}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	return &ringHandler{inner: h.inner.WithGroup(name), ring: h.ring}
}

// Observability bundles logger, rings, and metrics.
type Observability struct {
	Logger  *slog.Logger
	Ring    *Ring // agent (slog) logs
	BoxRing *Ring // sing-box dataplane logs
	Metrics *Metrics
}

// Setup builds slog + rings + metrics. level is debug|info|warn|error.
func Setup(level string) *Observability {
	ring := NewRing(2000)
	boxRing := NewRing(4000)
	metrics := &Metrics{}
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	inner := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	logger := slog.New(&ringHandler{inner: inner, ring: ring})
	return &Observability{Logger: logger, Ring: ring, BoxRing: boxRing, Metrics: metrics}
}

// BoxPlatformWriter adapts sing-box PlatformWriter into BoxRing.
type BoxPlatformWriter struct {
	Ring *Ring
}

func (w BoxPlatformWriter) WriteMessage(level uint8, message string) {
	if w.Ring == nil {
		return
	}
	lvl := "info"
	switch level {
	case 0, 1: // panic/fatal
		lvl = "error"
	case 2:
		lvl = "error"
	case 3:
		lvl = "warn"
	case 4:
		lvl = "info"
	case 5, 6: // debug/trace
		lvl = "debug"
	}
	w.Ring.Append(lvl, message, map[string]any{"component": "sing-box"})
}

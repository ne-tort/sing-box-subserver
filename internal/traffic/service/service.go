//go:build with_traffic

package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/store"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/tracker"
)

// Config for the traffic service.
type Config struct {
	DataDir         string
	FlushInterval   time.Duration
	RetentionDays   int
	AllowInject     bool
	Logger          *slog.Logger
}

// Service owns trackers, store, flush loop, and consumer APIs.
type Service struct {
	cfg    Config
	stats  *tracker.StatsTracker
	rate   *tracker.RateLimitTracker
	conns  *tracker.ConnTracker
	store  *store.Store
	log    *slog.Logger

	mu          sync.Mutex
	flushMu     sync.Mutex // serializes Flush vs Discard/Zero/SetSubjectUsage (no resurrect)
	lastFlush   time.Time
	lastOnline  map[string]struct{} // dataplane_user keys with activity in last flush
	cpLimits    map[string]domain.SpeedLimit // from controlplane speed_*
	manualLimits map[string]domain.SpeedLimit // from PUT /v1/traffic/limits (ops)
}

func New(cfg Config) (*Service, error) {
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 10 * time.Second
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 30
	}
	dir := cfg.DataDir
	if dir == "" {
		dir = "traffic"
	} else {
		dir = dir + "/traffic"
	}
	st, err := store.New(dir, cfg.RetentionDays)
	if err != nil {
		return nil, err
	}
	svc := &Service{
		cfg:        cfg,
		stats:      tracker.NewStatsTracker(),
		rate:       tracker.NewRateLimitTracker(),
		conns:      tracker.NewConnTracker(),
		store:      st,
		log:        cfg.Logger,
		lastOnline:   make(map[string]struct{}),
		cpLimits:     make(map[string]domain.SpeedLimit),
		manualLimits: make(map[string]domain.SpeedLimit),
	}
	return svc, nil
}

// Trackers returns connection trackers to append on the router.
func (s *Service) Trackers() []adapter.ConnectionTracker {
	if s == nil {
		return nil
	}
	// Order: stats (bytes) → rate (shaping) → conn (kick registry).
	return []adapter.ConnectionTracker{s.stats, s.rate, s.conns}
}

func (s *Service) OnBoxStarted(context.Context) {}
func (s *Service) OnBoxStopped() {
	if s == nil {
		return
	}
	_ = s.Flush()
}

// Run starts the flush + retention loop until ctx is done.
func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}
	t := time.NewTicker(s.cfg.FlushInterval)
	defer t.Stop()
	purgeEvery := time.NewTicker(24 * time.Hour)
	defer purgeEvery.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = s.Flush()
			return
		case <-t.C:
			if err := s.Flush(); err != nil && s.log != nil {
				s.log.Warn("traffic flush failed", "err", err)
			}
		case <-purgeEvery.C:
			if n, err := s.store.PurgeOlderThan(time.Now().UTC()); err != nil && s.log != nil {
				s.log.Warn("traffic retention purge failed", "err", err)
			} else if n > 0 && s.log != nil {
				s.log.Info("traffic series purged", "files", n)
			}
		}
	}
}

// Flush swaps live counters into the store.
func (s *Service) Flush() error {
	if s == nil {
		return nil
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	now := time.Now().UTC()
	inbounds, outbounds, users := s.stats.SwapDeltas()
	samples := make([]domain.Sample, 0, len(inbounds)+len(outbounds)+len(users)*2)
	online := make(map[string]struct{})
	for _, d := range inbounds {
		samples = append(samples, domain.Sample{At: now, SeriesType: domain.SeriesInbound, Key: d.Key, Up: d.Up, Down: d.Down})
	}
	for _, d := range outbounds {
		samples = append(samples, domain.Sample{At: now, SeriesType: domain.SeriesOutbound, Key: d.Key, Up: d.Up, Down: d.Down})
	}
	for _, d := range users {
		samples = append(samples, domain.Sample{At: now, SeriesType: domain.SeriesDataplaneUser, Key: d.Key, Up: d.Up, Down: d.Down})
		if d.Up > 0 || d.Down > 0 {
			online[d.Key] = struct{}{}
		}
	}
	// Aggregate subject series from current store subjects.
	for _, sub := range s.store.Subjects() {
		var up, down int64
		for _, k := range sub.DataplaneKeys {
			for _, d := range users {
				if d.Key == k {
					up += d.Up
					down += d.Down
				}
			}
		}
		if up > 0 || down > 0 {
			samples = append(samples, domain.Sample{At: now, SeriesType: domain.SeriesSubject, Key: sub.ID, Up: up, Down: down})
		}
	}
	if err := s.store.ApplySamples(samples); err != nil {
		return err
	}
	_ = s.SyncAutoDiscoverSubjects()
	s.mu.Lock()
	s.lastFlush = now
	s.lastOnline = online
	s.mu.Unlock()
	return nil
}

// RegisterManifest replaces subjects for a consumer.
func (s *Service) RegisterManifest(consumer string, subjects []domain.Subject) error {
	if s == nil {
		return nil
	}
	return s.store.ReplaceSubjects(consumer, subjects)
}

// SetLimits is an alias for SetManualLimits (HTTP PUT /v1/traffic/limits).
func (s *Service) SetLimits(limits map[string]domain.SpeedLimit) {
	s.SetManualLimits(limits)
}

// SetManualLimits replaces the ops/manual shaping layer (does not clear CP speed_*).
// Bare display names that prefix registered dataplane keys (e.g. "alice" → "alice-flow-none")
// are expanded onto those keys so CP VLESS ops overrides actually hit the wire path.
func (s *Service) SetManualLimits(limits map[string]domain.SpeedLimit) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if limits == nil {
		limits = make(map[string]domain.SpeedLimit)
	}
	s.manualLimits = expandManualLimitKeys(s.store.Subjects(), limits)
	eff := s.effectiveLimitsLocked()
	s.mu.Unlock()
	s.rate.SetLimits(eff)
}

// expandManualLimitKeys copies input limits and, when a key is a bare prefix of
// subject dataplane keys (key + "-…"), applies the same limit to those children
// and to an exact bare key if present. Exact variant keys are left as-is.
func expandManualLimitKeys(subjects []domain.Subject, in map[string]domain.SpeedLimit) map[string]domain.SpeedLimit {
	out := make(map[string]domain.SpeedLimit, len(in))
	for k, v := range in {
		out[k] = v
		if k == "" {
			continue
		}
		prefix := k + "-"
		for _, sub := range subjects {
			hasChild := false
			for _, dk := range sub.DataplaneKeys {
				if strings.HasPrefix(dk, prefix) {
					hasChild = true
					break
				}
			}
			if !hasChild {
				continue
			}
			for _, dk := range sub.DataplaneKeys {
				if dk == k || strings.HasPrefix(dk, prefix) {
					out[dk] = v
				}
			}
		}
	}
	return out
}

// SetCPLimits replaces the controlplane shaping layer from speed_* (preserves manual overrides).
func (s *Service) SetCPLimits(limits map[string]domain.SpeedLimit) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if limits == nil {
		limits = make(map[string]domain.SpeedLimit)
	}
	s.cpLimits = limits
	eff := s.effectiveLimitsLocked()
	s.mu.Unlock()
	s.rate.SetLimits(eff)
}

func (s *Service) effectiveLimitsLocked() map[string]domain.SpeedLimit {
	out := make(map[string]domain.SpeedLimit, len(s.cpLimits)+len(s.manualLimits))
	for k, v := range s.cpLimits {
		if v.UpBytesPerSec > 0 || v.DownBytesPerSec > 0 {
			out[k] = v
		}
	}
	// Manual wins on key conflict (ops override).
	for k, v := range s.manualLimits {
		if v.UpBytesPerSec <= 0 && v.DownBytesPerSec <= 0 {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

// CurrentLimits returns effective shaping map (CP ∪ manual, manual wins).
func (s *Service) CurrentLimits() map[string]domain.SpeedLimit {
	if s == nil {
		return nil
	}
	return s.rate.Limits()
}

// LimitsPayload for GET /v1/traffic/limits.
func (s *Service) LimitsPayload() map[string]any {
	if s == nil {
		return map[string]any{"enabled": false}
	}
	s.mu.Lock()
	cp := copyLimits(s.cpLimits)
	manual := copyLimits(s.manualLimits)
	eff := s.effectiveLimitsLocked()
	s.mu.Unlock()
	return map[string]any{
		"controlplane": cp,
		"manual":       manual,
		"effective":    eff,
	}
}

func copyLimits(in map[string]domain.SpeedLimit) map[string]domain.SpeedLimit {
	out := make(map[string]domain.SpeedLimit, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// PollSubjectUsage returns cumulative usage for a subject.
func (s *Service) PollSubjectUsage(subjectID string) domain.Usage {
	if s == nil {
		return domain.Usage{SubjectID: subjectID}
	}
	return s.store.SubjectUsage(subjectID)
}

// ZeroSubject resets store counters and discards live deltas for a subject.
func (s *Service) ZeroSubject(subjectID string) error {
	if s == nil {
		return nil
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	keys := s.subjectKeys(subjectID)
	s.stats.DiscardUserKeys(keys)
	return s.store.ZeroSubjectKeys(subjectID, keys)
}

// SetSubjectUsage sets absolute cumulative usage for a subject (admin PATCH sync).
// Live deltas for mapped keys are discarded so the next Flush cannot resurrect bytes.
// Absolute total is stored on the subject series (not a single dataplane key) so
// per-key live stats stay honest; see Store.SubjectUsage.
func (s *Service) SetSubjectUsage(subjectID string, total uint64) error {
	if s == nil {
		return nil
	}
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	keys := s.subjectKeys(subjectID)
	s.stats.DiscardUserKeys(keys)
	if err := s.store.ZeroSubjectKeys(subjectID, keys); err != nil {
		return err
	}
	if total == 0 {
		return nil
	}
	now := time.Now().UTC()
	return s.store.ApplySamples([]domain.Sample{
		{At: now, SeriesType: domain.SeriesSubject, Key: subjectID, Up: int64(total), Down: 0},
	})
}

func (s *Service) subjectKeys(subjectID string) []string {
	sub, ok := s.store.Subject(subjectID)
	if !ok {
		return nil
	}
	return append([]string{}, sub.DataplaneKeys...)
}

// InjectUserTraffic injects live dataplane_user bytes (tests / future WG path).
func (s *Service) InjectUserTraffic(user string, up, down int64) {
	if s == nil {
		return
	}
	s.stats.InjectUserTraffic(user, up, down)
}

// InjectInboundTraffic injects live inbound-tag bytes (metrics smoke).
func (s *Service) InjectInboundTraffic(inbound string, up, down int64) {
	if s == nil {
		return
	}
	s.stats.AddInboundTraffic(inbound, up, down)
}

// InjectAllowed reports whether POST /v1/traffic/inject is enabled.
func (s *Service) InjectAllowed() bool {
	return s != nil && s.cfg.AllowInject
}

// PollSubjectUsageByDataplaneKey finds a subject that owns the key and returns usage,
// or a synthetic usage from the raw dataplane_user counter.
func (s *Service) PollSubjectUsageByDataplaneKey(key string) domain.Usage {
	if s == nil || key == "" {
		return domain.Usage{}
	}
	for _, sub := range s.store.Subjects() {
		for _, k := range sub.DataplaneKeys {
			if k == key {
				return s.store.SubjectUsage(sub.ID)
			}
		}
	}
	c := s.store.Counter(domain.SeriesDataplaneUser, key)
	return domain.Usage{Key: key, Up: c.Up, Down: c.Down, Total: c.Up + c.Down}
}

// StatusPayload for HTTP.
func (s *Service) StatusPayload() map[string]any {
	if s == nil {
		return map[string]any{"enabled": false}
	}
	s.mu.Lock()
	last := s.lastFlush
	s.mu.Unlock()
	out := map[string]any{
		"enabled":             true,
		"flush_interval_sec":  int(s.cfg.FlushInterval.Seconds()),
		"retention_days":      s.store.RetentionDays(),
		"subjects_registered": len(s.store.Subjects()),
		"limits_effective":    len(s.CurrentLimits()),
		"conns_active":        s.ActiveConnCount(),
	}
	if !last.IsZero() {
		out["last_flush_at"] = last
	}
	return out
}

func (s *Service) Subjects() []domain.Subject {
	if s == nil {
		return nil
	}
	return s.store.Subjects()
}

func (s *Service) StatsQuery(subjectID string, seriesType domain.SeriesType, key string, since time.Time) map[string]any {
	if s == nil {
		return map[string]any{}
	}
	keyAllow := map[string]struct{}{}
	if subjectID != "" {
		keyAllow[subjectID] = struct{}{}
		for _, k := range s.subjectKeys(subjectID) {
			keyAllow[k] = struct{}{}
		}
	}
	cumulative := s.store.AllCounters()
	filtered := make([]domain.CounterTotal, 0)
	for _, c := range cumulative {
		if seriesType != "" && c.SeriesType != seriesType {
			continue
		}
		if key != "" && c.Key != key {
			continue
		}
		if subjectID != "" {
			switch c.SeriesType {
			case domain.SeriesSubject:
				if c.Key != subjectID {
					continue
				}
			case domain.SeriesDataplaneUser:
				if _, ok := keyAllow[c.Key]; !ok {
					continue
				}
			default:
				// inbound/outbound aggregates are not per-subject; omit unless key filter set
				if key == "" {
					continue
				}
			}
		}
		filtered = append(filtered, c)
	}
	series, _ := s.store.QuerySeries(since, seriesType, key)
	if subjectID != "" && key == "" {
		filteredSeries := make([]domain.Sample, 0, len(series))
		for _, sample := range series {
			switch sample.SeriesType {
			case domain.SeriesSubject:
				if sample.Key == subjectID {
					filteredSeries = append(filteredSeries, sample)
				}
			case domain.SeriesDataplaneUser:
				if _, ok := keyAllow[sample.Key]; ok {
					filteredSeries = append(filteredSeries, sample)
				}
			}
		}
		series = filteredSeries
	}
	out := map[string]any{
		"cumulative": filtered,
		"series":     series,
	}
	if subjectID != "" {
		out["subject_usage"] = s.store.SubjectUsage(subjectID)
	}
	return out
}

func (s *Service) Onlines() map[string]any {
	if s == nil {
		return map[string]any{"dataplane_users": []string{}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	users := make([]string, 0, len(s.lastOnline))
	for k := range s.lastOnline {
		users = append(users, k)
	}
	return map[string]any{"dataplane_users": users}
}

// DiscoverObservedUsers returns dataplane_user keys from cumulative counters (for auto-discover).
func (s *Service) DiscoverObservedUsers() []string {
	if s == nil {
		return nil
	}
	var out []string
	for _, c := range s.store.AllCounters() {
		if c.SeriesType == domain.SeriesDataplaneUser && c.Key != "" {
			out = append(out, c.Key)
		}
	}
	return out
}

// SetSkipInboundTags configures WG-style skip lists on both trackers.
func (s *Service) SetSkipInboundTags(tags []string) {
	if s == nil {
		return
	}
	s.stats.SetSkipInboundTags(tags)
	s.rate.SetSkipInboundTags(tags)
	s.conns.SetSkipInboundTags(tags)
}

// CloseConnByUsers kicks live sessions for dataplane keys (quota/expiry/disable).
func (s *Service) CloseConnByUsers(keys []string) int {
	if s == nil || s.conns == nil {
		return 0
	}
	return s.conns.CloseConnByUsers(keys)
}

// ActiveConnCount returns tracked session count.
func (s *Service) ActiveConnCount() int {
	if s == nil || s.conns == nil {
		return 0
	}
	return s.conns.ActiveCount()
}

//go:build with_traffic

package service

import (
	"context"
	"log/slog"
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
	Logger          *slog.Logger
}

// Service owns trackers, store, flush loop, and consumer APIs.
type Service struct {
	cfg    Config
	stats  *tracker.StatsTracker
	rate   *tracker.RateLimitTracker
	store  *store.Store
	log    *slog.Logger

	mu          sync.Mutex
	lastFlush   time.Time
	lastOnline  map[string]struct{} // dataplane_user keys with upload in last flush
	limits      map[string]domain.SpeedLimit
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
		store:      st,
		log:        cfg.Logger,
		lastOnline: make(map[string]struct{}),
		limits:     make(map[string]domain.SpeedLimit),
	}
	return svc, nil
}

// Trackers returns connection trackers to append on the router.
func (s *Service) Trackers() []adapter.ConnectionTracker {
	if s == nil {
		return nil
	}
	return []adapter.ConnectionTracker{s.stats, s.rate}
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
		if d.Up > 0 {
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

// SetLimits applies shaping map keyed by dataplane_user.
func (s *Service) SetLimits(limits map[string]domain.SpeedLimit) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.limits = limits
	if s.limits == nil {
		s.limits = make(map[string]domain.SpeedLimit)
	}
	s.mu.Unlock()
	s.rate.SetLimits(limits)
}

// PollSubjectUsage returns cumulative usage for a subject.
func (s *Service) PollSubjectUsage(subjectID string) domain.Usage {
	if s == nil {
		return domain.Usage{SubjectID: subjectID}
	}
	return s.store.SubjectUsage(subjectID)
}

// ZeroSubject resets counters for a subject.
func (s *Service) ZeroSubject(subjectID string) error {
	if s == nil {
		return nil
	}
	sub, ok := s.store.Subject(subjectID)
	if !ok {
		return s.store.ZeroSubjectKeys(subjectID, nil)
	}
	return s.store.ZeroSubjectKeys(subjectID, sub.DataplaneKeys)
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
	cumulative := s.store.AllCounters()
	filtered := make([]domain.CounterTotal, 0)
	for _, c := range cumulative {
		if seriesType != "" && c.SeriesType != seriesType {
			continue
		}
		if key != "" && c.Key != key {
			continue
		}
		if subjectID != "" && c.SeriesType == domain.SeriesSubject && c.Key != subjectID {
			continue
		}
		filtered = append(filtered, c)
	}
	series, _ := s.store.QuerySeries(since, seriesType, key)
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
}

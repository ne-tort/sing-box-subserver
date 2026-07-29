//go:build with_traffic

package service

import (
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
)

// SyncAutoDiscoverSubjects registers observed dataplane users and inbound aggregates
// for subscribe/direct modes (consumer "auto"). Does not overwrite controlplane subjects.
func (s *Service) SyncAutoDiscoverSubjects() error {
	if s == nil {
		return nil
	}
	users := s.DiscoverObservedUsers()
	subjects := make([]domain.Subject, 0, len(users)+8)
	for _, u := range users {
		subjects = append(subjects, domain.Subject{
			ID:            "dataplane_user:" + u,
			Kinds:         []domain.SubjectKind{domain.KindDataplaneUser},
			DataplaneKeys: []string{u},
			Labels:        map[string]string{"consumer": "auto", "source": "observed"},
		})
	}
	for _, c := range s.store.AllCounters() {
		if c.SeriesType != domain.SeriesInbound || c.Key == "" {
			continue
		}
		subjects = append(subjects, domain.Subject{
			ID:     "inbound:" + c.Key,
			Kinds:  []domain.SubjectKind{domain.KindInboundAggregate},
			Labels: map[string]string{"consumer": "auto", "inbound": c.Key},
		})
	}
	return s.store.ReplaceSubjects("auto", subjects)
}

// SetInboundCaps applies shaping to a synthetic key used when callers pass inbound-level limits.
// Keys should be dataplane user names; for inbound-only caps use a reserved prefix "inboundcap:".
func (s *Service) SetInboundCaps(caps map[string]domain.SpeedLimit) {
	if s == nil || len(caps) == 0 {
		return
	}
	s.mu.Lock()
	merged := make(map[string]domain.SpeedLimit, len(s.limits)+len(caps))
	for k, v := range s.limits {
		merged[k] = v
	}
	for k, v := range caps {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		merged[k] = v
	}
	s.limits = merged
	s.mu.Unlock()
	s.rate.SetLimits(merged)
}

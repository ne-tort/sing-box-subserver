//go:build with_traffic

package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
)

// Store persists cumulative counters, subjects, and JSONL time-series.
type Store struct {
	mu         sync.Mutex
	dir        string
	counters   map[string]*domain.CounterTotal // key = seriesType\x00key
	subjects   map[string]domain.Subject       // by subject_id
	retentionD int
}

func New(dir string, retentionDays int) (*Store, error) {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	if err := os.MkdirAll(filepath.Join(dir, "series"), 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		dir:        dir,
		counters:   make(map[string]*domain.CounterTotal),
		subjects:   make(map[string]domain.Subject),
		retentionD: retentionDays,
	}
	_ = s.loadCounters()
	_ = s.loadSubjects()
	return s, nil
}

func counterKey(st domain.SeriesType, key string) string {
	return string(st) + "\x00" + key
}

func (s *Store) loadCounters() error {
	raw, err := os.ReadFile(filepath.Join(s.dir, "counters.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var list []domain.CounterTotal
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	for i := range list {
		c := list[i]
		s.counters[counterKey(c.SeriesType, c.Key)] = &c
	}
	return nil
}

func (s *Store) loadSubjects() error {
	raw, err := os.ReadFile(filepath.Join(s.dir, "subjects.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var list []domain.Subject
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	for _, sub := range list {
		s.subjects[sub.ID] = sub
	}
	return nil
}

func (s *Store) saveCountersLocked() error {
	list := make([]domain.CounterTotal, 0, len(s.counters))
	for _, c := range s.counters {
		list = append(list, *c)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].SeriesType != list[j].SeriesType {
			return list[i].SeriesType < list[j].SeriesType
		}
		return list[i].Key < list[j].Key
	})
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dir, "counters.json"), raw)
}

func (s *Store) saveSubjectsLocked() error {
	list := make([]domain.Subject, 0, len(s.subjects))
	for _, sub := range s.subjects {
		list = append(list, sub)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dir, "subjects.json"), raw)
}

func atomicWrite(path string, raw []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReplaceSubjects replaces all registered subjects for a consumer namespace.
// When consumer is empty, replaces the entire subject map.
func (s *Store) ReplaceSubjects(consumer string, subjects []domain.Subject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if consumer == "" {
		s.subjects = make(map[string]domain.Subject, len(subjects))
		for _, sub := range subjects {
			s.subjects[sub.ID] = sub
		}
	} else {
		prefix := consumer + ":"
		for id := range s.subjects {
			if strings.HasPrefix(id, prefix) || (len(subjects) > 0 && subjectBelongs(s.subjects[id], consumer)) {
				// drop old ids from this consumer by label
			}
		}
		// Rebuild: keep subjects without matching consumer label, add new.
		next := make(map[string]domain.Subject)
		for id, sub := range s.subjects {
			if sub.Labels != nil && sub.Labels["consumer"] == consumer {
				continue
			}
			next[id] = sub
		}
		for _, sub := range subjects {
			if sub.Labels == nil {
				sub.Labels = map[string]string{}
			}
			sub.Labels["consumer"] = consumer
			next[sub.ID] = sub
		}
		s.subjects = next
	}
	return s.saveSubjectsLocked()
}

func subjectBelongs(sub domain.Subject, consumer string) bool {
	return sub.Labels != nil && sub.Labels["consumer"] == consumer
}

// ApplySamples adds deltas to cumulative counters and appends JSONL.
func (s *Store) ApplySamples(samples []domain.Sample) error {
	if len(samples) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byDay := map[string][]domain.Sample{}
	for _, sm := range samples {
		ck := counterKey(sm.SeriesType, sm.Key)
		ctr, ok := s.counters[ck]
		if !ok {
			ctr = &domain.CounterTotal{SeriesType: sm.SeriesType, Key: sm.Key}
			s.counters[ck] = ctr
		}
		ctr.Up += sm.Up
		ctr.Down += sm.Down
		day := sm.At.UTC().Format("2006-01-02")
		byDay[day] = append(byDay[day], sm)
	}
	if err := s.saveCountersLocked(); err != nil {
		return err
	}
	for day, list := range byDay {
		path := filepath.Join(s.dir, "series", day+".jsonl")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		w := bufio.NewWriter(f)
		for _, sm := range list {
			raw, err := json.Marshal(sm)
			if err != nil {
				_ = f.Close()
				return err
			}
			if _, err := w.Write(raw); err != nil {
				_ = f.Close()
				return err
			}
			if err := w.WriteByte('\n'); err != nil {
				_ = f.Close()
				return err
			}
		}
		if err := w.Flush(); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
	}
	return nil
}

// ZeroSubjectKeys zeroes cumulative counters for the given dataplane keys and subject id.
func (s *Store) ZeroSubjectKeys(subjectID string, keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		if ctr, ok := s.counters[counterKey(domain.SeriesDataplaneUser, k)]; ok {
			ctr.Up = 0
			ctr.Down = 0
		}
	}
	if subjectID != "" {
		if ctr, ok := s.counters[counterKey(domain.SeriesSubject, subjectID)]; ok {
			ctr.Up = 0
			ctr.Down = 0
		}
	}
	return s.saveCountersLocked()
}

func (s *Store) Subjects() []domain.Subject {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Subject, 0, len(s.subjects))
	for _, sub := range s.subjects {
		out = append(out, sub)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) Subject(id string) (domain.Subject, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subjects[id]
	return sub, ok
}

func (s *Store) Counter(st domain.SeriesType, key string) domain.CounterTotal {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.counters[counterKey(st, key)]; ok {
		return *c
	}
	return domain.CounterTotal{SeriesType: st, Key: key}
}

func (s *Store) AllCounters() []domain.CounterTotal {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.CounterTotal, 0, len(s.counters))
	for _, c := range s.counters {
		out = append(out, *c)
	}
	return out
}

// SubjectUsage sums dataplane_user counters for the subject's keys (+ subject series if present).
func (s *Store) SubjectUsage(id string) domain.Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := domain.Usage{SubjectID: id}
	sub, ok := s.subjects[id]
	if !ok {
		if c, ok := s.counters[counterKey(domain.SeriesSubject, id)]; ok {
			u.Up, u.Down = c.Up, c.Down
			u.Total = u.Up + u.Down
		}
		return u
	}
	for _, k := range sub.DataplaneKeys {
		if c, ok := s.counters[counterKey(domain.SeriesDataplaneUser, k)]; ok {
			u.Up += c.Up
			u.Down += c.Down
		}
	}
	u.Total = u.Up + u.Down
	return u
}

// QuerySeries reads JSONL samples since the given time.
func (s *Store) QuerySeries(since time.Time, seriesType domain.SeriesType, key string) ([]domain.Sample, error) {
	s.mu.Lock()
	dir := s.dir
	s.mu.Unlock()
	entries, err := os.ReadDir(filepath.Join(dir, "series"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []domain.Sample
	sinceDay := since.UTC().Format("2006-01-02")
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(name, ".jsonl")
		if day < sinceDay {
			continue
		}
		path := filepath.Join(dir, "series", name)
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			var sm domain.Sample
			if err := json.Unmarshal(sc.Bytes(), &sm); err != nil {
				continue
			}
			if sm.At.Before(since) {
				continue
			}
			if seriesType != "" && sm.SeriesType != seriesType {
				continue
			}
			if key != "" && sm.Key != key {
				continue
			}
			out = append(out, sm)
		}
		_ = f.Close()
	}
	return out, nil
}

// PurgeOlderThan deletes series files older than retention.
func (s *Store) PurgeOlderThan(now time.Time) (int, error) {
	s.mu.Lock()
	days := s.retentionD
	dir := s.dir
	s.mu.Unlock()
	cutoff := now.UTC().AddDate(0, 0, -days).Format("2006-01-02")
	entries, err := os.ReadDir(filepath.Join(dir, "series"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(name, ".jsonl")
		if day < cutoff {
			if err := os.Remove(filepath.Join(dir, "series", name)); err != nil {
				return n, fmt.Errorf("purge %s: %w", name, err)
			}
			n++
		}
	}
	return n, nil
}

func (s *Store) RetentionDays() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retentionD
}

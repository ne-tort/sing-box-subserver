//go:build with_controlplane

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func (s *Store) commitsDir() string {
	return filepath.Join(s.dir, "commits")
}

func (s *Store) LoadHeads() (domain.HeadsFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var h domain.HeadsFile
	err := readJSON(s.path("heads.json"), &h)
	if errors.Is(err, os.ErrNotExist) {
		return domain.HeadsFile{Blocks: map[string]domain.BlockHead{}}, nil
	}
	if err != nil {
		return domain.HeadsFile{}, err
	}
	if h.Blocks == nil {
		h.Blocks = map[string]domain.BlockHead{}
	}
	return h, nil
}

func (s *Store) SaveHeads(h domain.HeadsFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h.Blocks == nil {
		h.Blocks = map[string]domain.BlockHead{}
	}
	return writeJSON(s.path("heads.json"), h)
}

func (s *Store) LoadCommitMeta() (domain.CommitMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var m domain.CommitMeta
	err := readJSON(filepath.Join(s.commitsDir(), "_meta.json"), &m)
	if errors.Is(err, os.ErrNotExist) {
		return domain.CommitMeta{}, nil
	}
	return m, err
}

func (s *Store) SaveCommitMeta(m domain.CommitMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.commitsDir(), 0o700); err != nil {
		return err
	}
	if m.Recent == nil {
		m.Recent = []string{}
	}
	return writeJSON(filepath.Join(s.commitsDir(), "_meta.json"), m)
}

func (s *Store) LoadCommit(id string) (domain.Commit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") {
		return domain.Commit{}, fmt.Errorf("invalid commit id")
	}
	var c domain.Commit
	err := readJSON(filepath.Join(s.commitsDir(), id+".json"), &c)
	if errors.Is(err, os.ErrNotExist) {
		return domain.Commit{}, ErrNotFound
	}
	return c, err
}

func (s *Store) SaveCommit(c domain.Commit) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strings.TrimSpace(c.ID)
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") {
		return fmt.Errorf("invalid commit id")
	}
	if err := os.MkdirAll(s.commitsDir(), 0o700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.commitsDir(), id+".json"), c)
}

// ListRecentCommits loads commits listed in meta.recent (newest first).
func (s *Store) ListRecentCommits(limit int) ([]domain.Commit, error) {
	meta, err := s.LoadCommitMeta()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	out := make([]domain.Commit, 0, limit)
	for _, id := range meta.Recent {
		if len(out) >= limit {
			break
		}
		c, err := s.LoadCommit(id)
		if err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

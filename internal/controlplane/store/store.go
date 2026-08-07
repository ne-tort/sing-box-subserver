//go:build with_controlplane

package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/smoke"
)

// Store persists controlplane JSON files.
type Store struct {
	mu  sync.Mutex
	dir string
}

func Open(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "controlplane")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return atomicWrite(path, raw)
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

type usersFile struct {
	Users []domain.User `json:"users"`
}

type setsFile struct {
	Sets []domain.InboundSet `json:"sets"`
}

type realityAssignmentsFile struct {
	Assignments map[string]domain.RealityAssignment `json:"assignments"`
}

func (s *Store) LoadUsers() ([]domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f usersFile
	err := readJSON(s.path("users.json"), &f)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return f.Users, err
}

func (s *Store) SaveUsers(users []domain.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.path("users.json"), usersFile{Users: users})
}

func (s *Store) LoadSets() ([]domain.InboundSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f setsFile
	err := readJSON(s.path("sets.json"), &f)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return f.Sets, err
}

func (s *Store) SaveSets(sets []domain.InboundSet) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.path("sets.json"), setsFile{Sets: sets})
}

func (s *Store) LoadState() (domain.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st domain.State
	err := readJSON(s.path("state.json"), &st)
	if errors.Is(err, os.ErrNotExist) {
		return domain.State{ActiveSets: []string{}}, nil
	}
	if st.ActiveSets == nil {
		st.ActiveSets = []string{}
	}
	return st, err
}

func (s *Store) SaveState(st domain.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.ActiveSets == nil {
		st.ActiveSets = []string{}
	}
	return writeJSON(s.path("state.json"), st)
}

func (s *Store) ClearActiveSets() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var st domain.State
	err := readJSON(s.path("state.json"), &st)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	st.ActiveSets = []string{}
	return writeJSON(s.path("state.json"), st)
}

func (s *Store) LoadConfigFragments() (domain.ConfigFragments, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f domain.ConfigFragments
	err := readJSON(s.path("config_fragments.json"), &f)
	if errors.Is(err, os.ErrNotExist) {
		return domain.ConfigFragments{}, nil
	}
	return f, err
}

func (s *Store) SaveConfigFragments(f domain.ConfigFragments) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.path("config_fragments.json"), f)
}

// RulesetsDir is data_dir/controlplane/rulesets.
func (s *Store) RulesetsDir() string {
	return filepath.Join(s.dir, "rulesets")
}

// SafeRulesetFilename rejects path traversal; allows [A-Za-z0-9._-]+ ending in .srs/.json.
func SafeRulesetFilename(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("ruleset filename empty")
	}
	base := filepath.Base(name)
	if base != name || strings.Contains(name, "..") {
		return "", fmt.Errorf("ruleset filename invalid")
	}
	lower := strings.ToLower(base)
	if !strings.HasSuffix(lower, ".srs") && !strings.HasSuffix(lower, ".json") {
		return "", fmt.Errorf("ruleset filename must end with .srs or .json")
	}
	for _, r := range base {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			return "", fmt.Errorf("ruleset filename has invalid characters")
		}
	}
	return base, nil
}

// WriteRulesets replaces the rulesets directory contents with the given files (atomic-ish: clear then write).
func (s *Store) WriteRulesets(files map[string][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.dir, "rulesets")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for name, data := range files {
		safe, err := SafeRulesetFilename(name)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, safe)
		if err := atomicWrite(path, data); err != nil {
			return err
		}
		_ = os.Chmod(path, 0o600)
	}
	return nil
}

// ClearRulesets removes the rulesets directory.
func (s *Store) ClearRulesets() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(filepath.Join(s.dir, "rulesets"))
}

func (s *Store) RealityConfigPath() string {
	return s.path("reality_config.json")
}

func (s *Store) LoadRealityConfig() (domain.RealityConfig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var cfg domain.RealityConfig
	err := readJSON(s.path("reality_config.json"), &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return domain.RealityConfig{}, false, nil
	}
	if err != nil {
		return domain.RealityConfig{}, false, err
	}
	return cfg, true, nil
}

func (s *Store) SaveRealityConfig(cfg domain.RealityConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.path("reality_config.json"), cfg)
}

func (s *Store) LoadRealityAssignments() (map[string]domain.RealityAssignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f realityAssignmentsFile
	err := readJSON(s.path("reality_assignments.json"), &f)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]domain.RealityAssignment{}, nil
	}
	if err != nil {
		return nil, err
	}
	if f.Assignments == nil {
		f.Assignments = map[string]domain.RealityAssignment{}
	}
	return f.Assignments, nil
}

func (s *Store) LoadWgHub() (domain.WgHub, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var h domain.WgHub
	err := readJSON(s.path("wg_hub.json"), &h)
	if errors.Is(err, os.ErrNotExist) {
		return domain.DefaultWgHub(), nil
	}
	if err != nil {
		return domain.WgHub{}, err
	}
	h.Normalize()
	return h, nil
}

func (s *Store) SaveWgHub(h domain.WgHub) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h.Normalize()
	return writeJSON(s.path("wg_hub.json"), h)
}

func (s *Store) SaveRealityAssignments(assignments map[string]domain.RealityAssignment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if assignments == nil {
		assignments = map[string]domain.RealityAssignment{}
	}
	return writeJSON(s.path("reality_assignments.json"), realityAssignmentsFile{Assignments: assignments})
}

func (s *Store) LoadSSLProfiles() (domain.SSLProfilesFile, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var f domain.SSLProfilesFile
	err := readJSON(s.path("ssl_profiles.json"), &f)
	if errors.Is(err, os.ErrNotExist) {
		return domain.SSLProfilesFile{}, false, nil
	}
	if err != nil {
		return domain.SSLProfilesFile{}, false, err
	}
	if f.Profiles == nil {
		f.Profiles = []domain.SSLProfile{}
	}
	return f, true, nil
}

func (s *Store) SaveSSLProfiles(f domain.SSLProfilesFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.Profiles == nil {
		f.Profiles = []domain.SSLProfile{}
	}
	return writeJSON(s.path("ssl_profiles.json"), f)
}

func (s *Store) Dir() string { return s.dir }

// LoadSmokeLast returns the last smoke report, or ErrNotFound if never run.
func (s *Store) LoadSmokeLast() (*smoke.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var rep smoke.Report
	err := readJSON(s.path("smoke_last.json"), &rep)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rep, nil
}

// SaveSmokeLast atomically overwrites the last smoke report (no history / TTL).
func (s *Store) SaveSmokeLast(rep *smoke.Report) error {
	if rep == nil {
		return fmt.Errorf("nil smoke report")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.path("smoke_last.json"), rep)
}

var ErrNotFound = fmt.Errorf("not found")

package configstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	fileLastGood     = "last-good.json"
	fileLastGoodMeta = "last-good.meta.json"
	fileStaged       = "staged.json"
	fileStagedMeta   = "staged.meta.json"
	fileAgentState   = "agent-state.json"
)

// Source identifies who submitted a config.
type Source string

const (
	SourcePush Source = "push"
	SourcePull Source = "pull"
	SourceBoot Source = "boot"
)

// Meta describes a stored config blob.
type Meta struct {
	ContentSHA256 string    `json:"content_sha256"`
	Revision      uint64    `json:"revision"`
	Source        Source    `json:"source"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AgentState persists monotonic revision counter.
type AgentState struct {
	Revision uint64 `json:"revision"`
}

// Store manages staged / last-good configs under dataDir.
type Store struct {
	dir string
}

func New(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir data_dir: %w", err)
	}
	return &Store{dir: dataDir}, nil
}

func (s *Store) Dir() string { return s.dir }

func Hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Store) path(name string) string { return filepath.Join(s.dir, name) }

func atomicWrite(path string, data []byte, mode os.FileMode) error {
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
	if err := tmp.Chmod(mode); err != nil {
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
	return atomicWrite(path, raw, 0o640)
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// LoadState reads agent-state.json (missing → zero).
func (s *Store) LoadState() (AgentState, error) {
	var st AgentState
	err := readJSON(s.path(fileAgentState), &st)
	if errors.Is(err, os.ErrNotExist) {
		return AgentState{}, nil
	}
	return st, err
}

func (s *Store) saveState(st AgentState) error {
	return writeJSON(s.path(fileAgentState), st)
}

// WriteStaged writes staged config + meta (revision = current, not bumped).
func (s *Store) WriteStaged(raw []byte, source Source) (Meta, error) {
	st, err := s.LoadState()
	if err != nil {
		return Meta{}, err
	}
	meta := Meta{
		ContentSHA256: Hash(raw),
		Revision:      st.Revision,
		Source:        source,
		UpdatedAt:     time.Now().UTC(),
	}
	if err := atomicWrite(s.path(fileStaged), raw, 0o640); err != nil {
		return Meta{}, fmt.Errorf("write staged: %w", err)
	}
	if err := writeJSON(s.path(fileStagedMeta), meta); err != nil {
		return Meta{}, fmt.Errorf("write staged meta: %w", err)
	}
	return meta, nil
}

// ReadStaged returns staged bytes + meta.
func (s *Store) ReadStaged() ([]byte, Meta, error) {
	raw, err := os.ReadFile(s.path(fileStaged))
	if err != nil {
		return nil, Meta{}, err
	}
	var meta Meta
	if err := readJSON(s.path(fileStagedMeta), &meta); err != nil {
		return nil, Meta{}, err
	}
	return raw, meta, nil
}

// ReadLastGood returns last-good bytes + meta. ErrNotExist if none.
func (s *Store) ReadLastGood() ([]byte, Meta, error) {
	raw, err := os.ReadFile(s.path(fileLastGood))
	if err != nil {
		return nil, Meta{}, err
	}
	var meta Meta
	if err := readJSON(s.path(fileLastGoodMeta), &meta); err != nil {
		return nil, Meta{}, err
	}
	return raw, meta, nil
}

// HasLastGood reports whether last-good exists.
func (s *Store) HasLastGood() bool {
	_, err := os.Stat(s.path(fileLastGood))
	return err == nil
}

// PromoteStaged copies staged → last-good and bumps revision. Returns new meta.
func (s *Store) PromoteStaged() (Meta, error) {
	raw, meta, err := s.ReadStaged()
	if err != nil {
		return Meta{}, fmt.Errorf("read staged for promote: %w", err)
	}
	st, err := s.LoadState()
	if err != nil {
		return Meta{}, err
	}
	st.Revision++
	meta.Revision = st.Revision
	meta.UpdatedAt = time.Now().UTC()
	if err := atomicWrite(s.path(fileLastGood), raw, 0o640); err != nil {
		return Meta{}, fmt.Errorf("write last-good: %w", err)
	}
	if err := writeJSON(s.path(fileLastGoodMeta), meta); err != nil {
		return Meta{}, fmt.Errorf("write last-good meta: %w", err)
	}
	if err := s.saveState(st); err != nil {
		return Meta{}, fmt.Errorf("save agent state: %w", err)
	}
	return meta, nil
}

// CurrentRevision returns the persisted revision counter.
func (s *Store) CurrentRevision() (uint64, error) {
	st, err := s.LoadState()
	return st.Revision, err
}

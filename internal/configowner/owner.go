// Package configowner is the exclusive desired-config writer registry (ADR 0008).
package configowner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Mode is the normative config_mode enum.
type Mode string

const (
	ModeIdle         Mode = "idle"
	ModeSubscribed   Mode = "subscribed"
	ModeDirect       Mode = "direct"
	ModeControlplane Mode = "controlplane"
)

const stateFile = "config-owner.json"

// Hooks run when leaving a mode (after the new mode is successfully persisted).
type Hooks struct {
	// OnLeaveSubscribe is called when leaving subscribed (cancel pull schedule).
	OnLeaveSubscribe func()
	// OnLeaveControlplane is called when leaving controlplane (clear active_sets).
	OnLeaveControlplane func()
}

// Registry is the single source of truth for config_mode.
type Registry struct {
	mu      sync.RWMutex
	dir     string
	mode    Mode
	hooks   Hooks
}

type persisted struct {
	Mode Mode `json:"mode"`
}

// Open loads or creates the registry under dataDir.
func Open(dataDir string) (*Registry, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	r := &Registry{dir: dataDir, mode: ModeIdle}
	path := filepath.Join(dataDir, stateFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	var p persisted
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("config-owner.json: %w", err)
	}
	if validMode(p.Mode) {
		r.mode = p.Mode
	}
	return r, nil
}

func validMode(m Mode) bool {
	switch m {
	case ModeIdle, ModeSubscribed, ModeDirect, ModeControlplane:
		return true
	default:
		return false
	}
}

// SetHooks registers leave callbacks (typically from app wire).
func (r *Registry) SetHooks(h Hooks) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = h
}

// Owner returns the current mode.
func (r *Registry) Owner() Mode {
	if r == nil {
		return ModeIdle
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mode
}

// Claim switches ownership to mode and runs leave hooks for the previous mode.
// On persist failure the in-memory mode is rolled back and hooks are not run.
func (r *Registry) Claim(mode Mode) error {
	if r == nil {
		return fmt.Errorf("configowner: nil registry")
	}
	if !validMode(mode) {
		return fmt.Errorf("configowner: invalid mode %q", mode)
	}
	r.mu.Lock()
	prev := r.mode
	if prev == mode {
		r.mu.Unlock()
		return nil
	}
	hooks := r.hooks
	r.mode = mode
	err := r.persistLocked()
	if err != nil {
		r.mode = prev
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	if prev == ModeSubscribed && hooks.OnLeaveSubscribe != nil {
		hooks.OnLeaveSubscribe()
	}
	if prev == ModeControlplane && hooks.OnLeaveControlplane != nil {
		hooks.OnLeaveControlplane()
	}
	return nil
}

func (r *Registry) persistLocked() error {
	path := filepath.Join(r.dir, stateFile)
	raw, err := json.MarshalIndent(persisted{Mode: r.mode}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return atomicWrite(path, raw, 0o600)
}

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

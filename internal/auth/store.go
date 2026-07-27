package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const credentialsFile = "credentials.json"

var (
	ErrNotFound       = errors.New("token not found")
	ErrLastCredential = errors.New("cannot remove last valid credential")
	ErrInvalidToken   = errors.New("token too short (min 16 chars)")
	ErrBootstrapOff   = errors.New("bootstrap already disabled")
	ErrNeedManaged    = errors.New("disable bootstrap requires at least one managed token")
)

// Record is a managed API credential (panel-facing).
type Record struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Secret    string     `json:"secret"` // stored under data_dir 0600; never returned by List
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used_at,omitempty"`
}

// PublicView is safe for GET list (no secret).
type PublicView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used_at,omitempty"`
	Bootstrap bool       `json:"bootstrap,omitempty"`
}

type fileState struct {
	BootstrapEnabled *bool    `json:"bootstrap_enabled"`
	Tokens           []Record `json:"tokens"`
}

// Store holds bootstrap (YAML) + managed tokens. Hot rotation without process restart.
type Store struct {
	mu               sync.RWMutex
	dataDir          string
	bootstrap        string
	bootstrapEnabled bool
	tokens           []Record
}

// Open loads credentials from dataDir and seeds bootstrap from YAML token.
func Open(dataDir, bootstrapToken string) (*Store, error) {
	s := &Store{
		dataDir:          dataDir,
		bootstrap:        strings.TrimSpace(bootstrapToken),
		bootstrapEnabled: true,
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, err
	}
	if err := s.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *Store) path() string { return filepath.Join(s.dataDir, credentialsFile) }

func (s *Store) load() error {
	raw, err := os.ReadFile(s.path())
	if err != nil {
		return err
	}
	var st fileState
	if err := json.Unmarshal(raw, &st); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.BootstrapEnabled != nil {
		s.bootstrapEnabled = *st.BootstrapEnabled
	}
	if len(st.Tokens) > 0 {
		s.tokens = st.Tokens
	}
	return nil
}

func (s *Store) saveLocked() error {
	en := s.bootstrapEnabled
	st := fileState{BootstrapEnabled: &en, Tokens: s.tokens}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

// ExtractBearer returns the bearer token value or empty.
func ExtractBearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

func tokenEQ(got, want string) bool {
	if want == "" || got == "" {
		return false
	}
	// Hash then compare so length differences do not short-circuit.
	gh := sha256.Sum256([]byte(got))
	wh := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gh[:], wh[:]) == 1
}

// Authorize validates the request against bootstrap and managed tokens.
func (s *Store) Authorize(r *http.Request) bool {
	got := ExtractBearer(r)
	if got == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ok := false
	if s.bootstrapEnabled && tokenEQ(got, s.bootstrap) {
		ok = true
	}
	now := time.Now().UTC()
	for i := range s.tokens {
		if tokenEQ(got, s.tokens[i].Secret) {
			t := now
			s.tokens[i].LastUsed = &t
			ok = true
			break
		}
	}
	return ok
}

// List returns public metadata (no secrets).
func (s *Store) List() []PublicView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PublicView, 0, len(s.tokens)+1)
	if s.bootstrapEnabled && s.bootstrap != "" {
		out = append(out, PublicView{ID: "bootstrap", Name: "bootstrap (yaml)", Bootstrap: true})
	}
	for _, t := range s.tokens {
		out = append(out, PublicView{
			ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt, LastUsed: t.LastUsed,
		})
	}
	return out
}

// Create adds a managed token. If secret empty, generates one. Returns plaintext once.
func (s *Store) Create(name, secret string) (PublicView, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "panel"
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		var err error
		secret, err = generateToken()
		if err != nil {
			return PublicView{}, "", err
		}
	}
	if len(secret) < 16 {
		return PublicView{}, "", ErrInvalidToken
	}
	rec := Record{
		ID:        newID(),
		Name:      name,
		Secret:    secret,
		CreatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = append(s.tokens, rec)
	if err := s.saveLocked(); err != nil {
		s.tokens = s.tokens[:len(s.tokens)-1]
		return PublicView{}, "", err
	}
	return PublicView{ID: rec.ID, Name: rec.Name, CreatedAt: rec.CreatedAt}, secret, nil
}

// Delete removes a managed token by id. Cannot remove last credential if bootstrap is off.
func (s *Store) Delete(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || id == "bootstrap" {
		return fmt.Errorf("%w: use POST /v1/auth/bootstrap/disable", ErrNotFound)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.tokens {
		if s.tokens[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNotFound
	}
	remainingManaged := len(s.tokens) - 1
	if !s.bootstrapEnabled && remainingManaged < 1 {
		return ErrLastCredential
	}
	s.tokens = append(s.tokens[:idx], s.tokens[idx+1:]...)
	return s.saveLocked()
}

// Rotate creates a new token and optionally deletes previous managed tokens (not bootstrap).
func (s *Store) Rotate(name string, revokeOthers bool) (PublicView, string, error) {
	view, secret, err := s.Create(name, "")
	if err != nil {
		return PublicView{}, "", err
	}
	if revokeOthers {
		s.mu.Lock()
		keep := make([]Record, 0, 1)
		for _, t := range s.tokens {
			if t.ID == view.ID {
				keep = append(keep, t)
			}
		}
		s.tokens = keep
		_ = s.saveLocked()
		s.mu.Unlock()
	}
	return view, secret, nil
}

// DisableBootstrap turns off YAML token after at least one managed token exists.
func (s *Store) DisableBootstrap() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bootstrapEnabled {
		return ErrBootstrapOff
	}
	if len(s.tokens) < 1 {
		return ErrNeedManaged
	}
	s.bootstrapEnabled = false
	return s.saveLocked()
}

// BootstrapEnabled reports whether YAML token is still accepted.
func (s *Store) BootstrapEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bootstrapEnabled
}

// CountActive returns how many credentials can authenticate.
func (s *Store) CountActive() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := len(s.tokens)
	if s.bootstrapEnabled && s.bootstrap != "" {
		n++
	}
	return n
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

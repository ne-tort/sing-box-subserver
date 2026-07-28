package subscribe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/httputil"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

const stateFile = "subscribe-state.json"

// Spec is a runtime subscription to a remote JSON config URL (any HTTP that returns server JSON).
type Spec struct {
	URL         string            `json:"url"`
	IntervalSec int               `json:"interval_sec"`
	JitterSec   int               `json:"jitter_sec"`
	TimeoutSec  int               `json:"timeout_sec"`
	Headers     map[string]string `json:"headers,omitempty"`
	// DecryptBody: when true, body must be an aes-256-gcm envelope decryptable with agent token.
	// When false/omitted, plain JSON is accepted; encrypted envelopes are still auto-detected.
	DecryptBody bool `json:"decrypt_body,omitempty"`
	// TLSInsecure skips TLS certificate verification (local/dev panels with self-signed certs).
	TLSInsecure bool `json:"tls_insecure,omitempty"`
}

// Status is exposed via GET /v1/status and subscribe endpoints.
type Status struct {
	Enabled       bool       `json:"enabled"`
	Configured    bool       `json:"configured"` // true once REST or YAML seed wrote state (survives disable)
	URL           string     `json:"url,omitempty"`
	IntervalSec   int        `json:"interval_sec"`
	JitterSec     int        `json:"jitter_sec"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     *string    `json:"last_error"`
	Mode          string     `json:"mode"` // idle | subscribed
	LastNoop      bool       `json:"last_noop,omitempty"` // last tick skipped apply (same SHA)
}

type persisted struct {
	// Present marks that runtime owns this section; YAML must not re-seed on restart.
	Present bool `json:"present"`
	Enabled bool `json:"enabled"`
	Spec    Spec `json:"spec"`
}

// Manager owns scheduled + forced fetch of remote server JSON.
type Manager struct {
	dataDir string
	token   string // agent token for default Authorization + optional decrypt
	sup     *supervisor.Supervisor

	mu         sync.Mutex
	configured bool
	enabled    bool
	spec       Spec
	client     *http.Client
	lastNoop   bool

	fetchMu sync.Mutex
	trigger chan struct{}
}

func New(dataDir, agentToken string, sup *supervisor.Supervisor) *Manager {
	return &Manager{
		dataDir: dataDir,
		token:   agentToken,
		sup:     sup,
		trigger: make(chan struct{}, 1),
		client:  httputil.NewClient(15, false),
	}
}

// SetOutboundToken updates the default Bearer used when Spec.Headers has no Authorization.
func (m *Manager) SetOutboundToken(token string) {
	m.mu.Lock()
	m.token = strings.TrimSpace(token)
	m.mu.Unlock()
}

// BootstrapFromYAML seeds subscription once from agent YAML pull when no runtime state exists.
// If subscribe-state.json is present (even enabled=false), YAML is ignored — REST owns the setting.
func (m *Manager) BootstrapFromYAML(cfg *agentcfg.Config) error {
	if st, err := m.load(); err == nil && st.Present {
		m.mu.Lock()
		m.configured = true
		m.spec = normalizeSpec(st.Spec)
		m.enabled = st.Enabled && strings.TrimSpace(st.Spec.URL) != ""
		if m.enabled {
			m.client = httputil.NewClient(m.spec.TimeoutSec, m.spec.TLSInsecure)
		}
		m.mu.Unlock()
		m.syncPullStatus()
		if m.enabled {
			m.kick()
		}
		return nil
	}
	// Legacy: file without Present but with URL — treat as present.
	if st, err := m.load(); err == nil && strings.TrimSpace(st.Spec.URL) != "" {
		m.mu.Lock()
		m.configured = true
		m.spec = normalizeSpec(st.Spec)
		m.enabled = st.Enabled
		if m.enabled {
			m.client = httputil.NewClient(m.spec.TimeoutSec, m.spec.TLSInsecure)
		}
		m.mu.Unlock()
		_ = m.save() // upgrade to Present=true
		m.syncPullStatus()
		if m.enabled {
			m.kick()
		}
		return nil
	}
	if cfg != nil && cfg.Pull.Enabled && strings.TrimSpace(cfg.Pull.URL) != "" {
		return m.Subscribe(Spec{
			URL:         cfg.Pull.URL,
			IntervalSec: cfg.Pull.IntervalSec,
			JitterSec:   cfg.Pull.JitterSec,
			TimeoutSec:  cfg.Pull.TimeoutSec,
			Headers:     cfg.Pull.Headers,
			TLSInsecure: cfg.Pull.TLSInsecure,
		})
	}
	m.syncPullStatus()
	return nil
}

// Run blocks until ctx cancelled.
func (m *Manager) Run(ctx context.Context) {
	for {
		m.mu.Lock()
		en := m.enabled
		m.mu.Unlock()
		if !en {
			select {
			case <-ctx.Done():
				return
			case <-m.trigger:
				continue
			}
		}
		if err := m.Tick(ctx); err != nil {
			m.sup.ReportPullError(err)
		} else {
			m.sup.ReportPullSuccess()
		}
		for {
			m.mu.Lock()
			en = m.enabled
			wait := m.jitteredLocked()
			m.mu.Unlock()
			if !en {
				break
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-m.trigger:
				timer.Stop()
				goto next
			case <-timer.C:
				if err := m.Tick(ctx); err != nil {
					m.sup.ReportPullError(err)
				} else {
					m.sup.ReportPullSuccess()
				}
			}
		}
	next:
	}
}

func (m *Manager) jitteredLocked() time.Duration {
	sec := m.spec.IntervalSec
	if sec <= 0 {
		sec = 60
	}
	base := time.Duration(sec) * time.Second
	j := m.spec.JitterSec
	if j < 0 {
		j = 0
	}
	if j == 0 {
		return base
	}
	delta := time.Duration(rand.Int63n(int64(2*j*int(time.Second))+1)) - time.Duration(j)*time.Second
	d := base + delta
	if d < time.Second {
		return time.Second
	}
	return d
}

func normalizeSpec(spec Spec) Spec {
	spec.URL = strings.TrimSpace(spec.URL)
	if spec.IntervalSec <= 0 {
		spec.IntervalSec = 60
	}
	if spec.JitterSec < 0 {
		spec.JitterSec = 0
	}
	if spec.TimeoutSec <= 0 {
		spec.TimeoutSec = 15
	}
	return spec
}

// Subscribe enables scheduled fetch and triggers an immediate refresh.
func (m *Manager) Subscribe(spec Spec) error {
	spec = normalizeSpec(spec)
	if spec.URL == "" {
		return fmt.Errorf("url is required")
	}
	m.mu.Lock()
	m.spec = spec
	m.enabled = true
	m.configured = true
	m.client = httputil.NewClient(spec.TimeoutSec, spec.TLSInsecure)
	m.mu.Unlock()
	if err := m.save(); err != nil {
		return err
	}
	m.syncPullStatus()
	m.kick()
	return nil
}

// Unsubscribe stops scheduled fetch (idle). Does not clear last-good box config.
// Persists Present=true so YAML pull cannot re-enable on restart.
func (m *Manager) Unsubscribe() error {
	m.mu.Lock()
	m.enabled = false
	m.configured = true
	m.mu.Unlock()
	if err := m.save(); err != nil {
		return err
	}
	m.syncPullStatus()
	m.kick()
	return nil
}

// CancelOnDirectConfig is called after successful PUT /v1/config.
func (m *Manager) CancelOnDirectConfig() {
	_ = m.Unsubscribe()
}

var ErrNotSubscribed = errors.New("not subscribed")

func (m *Manager) Refresh(ctx context.Context) error {
	m.mu.Lock()
	en := m.enabled
	m.mu.Unlock()
	if !en {
		return ErrNotSubscribed
	}
	if err := m.Tick(ctx); err != nil {
		m.sup.ReportPullError(err)
		return err
	}
	m.sup.ReportPullSuccess()
	return nil
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	ps := m.sup.Status().Pull
	mode := "idle"
	if m.enabled {
		mode = "subscribed"
	}
	return Status{
		Enabled:       m.enabled,
		Configured:    m.configured,
		URL:           m.spec.URL,
		IntervalSec:   m.spec.IntervalSec,
		JitterSec:     m.spec.JitterSec,
		LastSuccessAt: ps.LastSuccessAt,
		LastError:     ps.LastError,
		Mode:          mode,
		LastNoop:      m.lastNoop,
	}
}

func (m *Manager) kick() {
	select {
	case m.trigger <- struct{}{}:
	default:
	}
}

func (m *Manager) syncPullStatus() {
	m.mu.Lock()
	en := m.enabled
	iv := m.spec.IntervalSec
	m.mu.Unlock()
	m.sup.SetPullStatus(supervisor.PullStatus{
		Enabled:     en,
		IntervalSec: iv,
	})
}

// Tick performs one conditional GET + Apply (with local SHA dedupe before restart).
func (m *Manager) Tick(ctx context.Context) error {
	m.fetchMu.Lock()
	defer m.fetchMu.Unlock()

	m.mu.Lock()
	if !m.enabled {
		m.mu.Unlock()
		return ErrNotSubscribed
	}
	spec := m.spec
	client := m.client
	token := m.token
	m.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return err
	}
	hasAuth := false
	for k, v := range spec.Headers {
		req.Header.Set(k, v)
		if strings.EqualFold(k, "Authorization") {
			hasAuth = true
		}
	}
	if !hasAuth && token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	st := m.sup.Status()
	if st.ContentSHA256 != "" {
		req.Header.Set("If-None-Match", `"sha256:`+st.ContentSHA256+`"`)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotModified {
		m.mu.Lock()
		m.lastNoop = true
		m.mu.Unlock()
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("subscribe status %d: %s", resp.StatusCode, truncate(body, 200))
	}

	plain, err := MaybeDecrypt(body, token, spec.DecryptBody)
	if err != nil {
		return err
	}

	// Local content compare: avoid Apply/restart when body unchanged (even if origin ignored ETag).
	sha := configstore.Hash(plain)
	if st.ContentSHA256 != "" && sha == st.ContentSHA256 {
		m.mu.Lock()
		m.lastNoop = true
		m.mu.Unlock()
		return nil
	}

	res, err := m.sup.Apply(ctx, supervisor.ApplyRequest{
		Raw:    plain,
		Source: configstore.SourceSubscribe,
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.lastNoop = res.Noop
	m.mu.Unlock()
	return nil
}

func (m *Manager) statePath() string {
	return filepath.Join(m.dataDir, stateFile)
}

func (m *Manager) save() error {
	m.mu.Lock()
	p := persisted{Present: true, Enabled: m.enabled, Spec: m.spec}
	m.mu.Unlock()
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := m.statePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, m.statePath())
}

func (m *Manager) load() (persisted, error) {
	var p persisted
	raw, err := os.ReadFile(m.statePath())
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(raw, &p)
	return p, err
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

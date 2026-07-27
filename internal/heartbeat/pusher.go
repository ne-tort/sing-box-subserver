package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"github.com/ne-tort/sing-box-subserver/internal/version"
)

const stateFile = "heartbeat-state.json"

// Spec configures optional status push to any HTTP endpoint (not s-ui specific).
type Spec struct {
	URL         string            `json:"url"`
	IntervalSec int               `json:"interval_sec"`
	TimeoutSec  int               `json:"timeout_sec"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// Status is exposed via REST / status.
type Status struct {
	Enabled       bool       `json:"enabled"`
	Configured    bool       `json:"configured"`
	URL           string     `json:"url,omitempty"`
	IntervalSec   int        `json:"interval_sec"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     *string    `json:"last_error"`
}

type persisted struct {
	Present bool `json:"present"`
	Enabled bool `json:"enabled"`
	Spec    Spec `json:"spec"`
}

// InboundsCounter optionally counts inbounds in last-good config (injected from app).
type InboundsCounter func() int

// Pusher POSTs status snapshots on a schedule; runtime Spec is owned by data_dir after seed/REST.
type Pusher struct {
	nodeID   string
	listen   string
	token    string
	dataDir  string
	sup      *supervisor.Supervisor
	inbounds InboundsCounter
	subStat  func() any // optional subscribe status snapshot

	mu         sync.Mutex
	configured bool
	enabled    bool
	spec       Spec
	client     *http.Client
	lastOK     *time.Time
	lastErr    *string

	trigger chan struct{}
}

func New(dataDir, nodeID, listen, agentToken string, sup *supervisor.Supervisor) *Pusher {
	return &Pusher{
		dataDir: dataDir,
		nodeID:  nodeID,
		listen:  listen,
		token:   agentToken,
		sup:     sup,
		trigger: make(chan struct{}, 1),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *Pusher) SetInboundsCounter(fn InboundsCounter) { p.inbounds = fn }
func (p *Pusher) SetSubscribeStatus(fn func() any)      { p.subStat = fn }

// BootstrapFromYAML seeds once from YAML when no runtime state exists.
func (p *Pusher) BootstrapFromYAML(cfg *agentcfg.Config) error {
	if st, err := p.load(); err == nil && st.Present {
		return p.applyPersisted(st)
	}
	if st, err := p.load(); err == nil && strings.TrimSpace(st.Spec.URL) != "" {
		st.Present = true
		if err := p.applyPersisted(st); err != nil {
			return err
		}
		return p.save()
	}
	if cfg != nil && cfg.Heartbeat.Enabled && strings.TrimSpace(cfg.Heartbeat.URL) != "" {
		return p.Configure(Spec{
			URL:         cfg.Heartbeat.URL,
			IntervalSec: cfg.Heartbeat.IntervalSec,
			TimeoutSec:  cfg.Heartbeat.TimeoutSec,
			Headers:     cfg.Heartbeat.Headers,
		}, true)
	}
	return nil
}

func (p *Pusher) applyPersisted(st persisted) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configured = true
	p.spec = normalize(st.Spec)
	p.enabled = st.Enabled && strings.TrimSpace(p.spec.URL) != ""
	if p.enabled {
		p.client = &http.Client{Timeout: time.Duration(p.spec.TimeoutSec) * time.Second}
	}
	return nil
}

func normalize(spec Spec) Spec {
	spec.URL = strings.TrimSpace(spec.URL)
	if spec.IntervalSec <= 0 {
		spec.IntervalSec = 30
	}
	if spec.TimeoutSec <= 0 {
		spec.TimeoutSec = 10
	}
	return spec
}

// Configure enables heartbeat and persists (REST / YAML seed).
func (p *Pusher) Configure(spec Spec, enabled bool) error {
	spec = normalize(spec)
	if enabled && spec.URL == "" {
		return fmt.Errorf("url is required when enabling heartbeat")
	}
	p.mu.Lock()
	p.spec = spec
	p.enabled = enabled && spec.URL != ""
	p.configured = true
	p.client = &http.Client{Timeout: time.Duration(spec.TimeoutSec) * time.Second}
	p.mu.Unlock()
	if err := p.save(); err != nil {
		return err
	}
	p.kick()
	return nil
}

// Disable stops heartbeat; persists Present so YAML cannot re-enable on restart.
func (p *Pusher) Disable() error {
	p.mu.Lock()
	p.enabled = false
	p.configured = true
	p.mu.Unlock()
	if err := p.save(); err != nil {
		return err
	}
	p.kick()
	return nil
}

func (p *Pusher) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Status{
		Enabled:       p.enabled,
		Configured:    p.configured,
		URL:           p.spec.URL,
		IntervalSec:   p.spec.IntervalSec,
		LastSuccessAt: p.lastOK,
		LastError:     p.lastErr,
	}
}

func (p *Pusher) kick() {
	select {
	case p.trigger <- struct{}{}:
	default:
	}
}

// Run blocks until ctx cancelled.
func (p *Pusher) Run(ctx context.Context) {
	for {
		p.mu.Lock()
		en := p.enabled
		interval := time.Duration(p.spec.IntervalSec) * time.Second
		p.mu.Unlock()
		if interval <= 0 {
			interval = 30 * time.Second
		}
		if !en {
			select {
			case <-ctx.Done():
				return
			case <-p.trigger:
				continue
			}
		}
		_ = p.tick(ctx)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-p.trigger:
			timer.Stop()
			continue
		case <-timer.C:
		}
	}
}

func (p *Pusher) tick(ctx context.Context) error {
	p.mu.Lock()
	if !p.enabled {
		p.mu.Unlock()
		return errors.New("heartbeat disabled")
	}
	spec := p.spec
	client := p.client
	token := p.token
	nodeID := p.nodeID
	listen := p.listen
	p.mu.Unlock()

	st := p.sup.Status()
	payload := map[string]any{
		"node_id":            nodeID,
		"state":              st.State,
		"listen":             listen,
		"agent_version":      version.AgentVersion,
		"agent_commit":       version.AgentCommit,
		"singbox_version":    version.SingBoxVersion(),
		"singbox_commit":     version.SingBoxCommit,
		"build_tags":         version.BuildTags,
		"revision":           st.Revision,
		"content_sha256":     st.ContentSHA256,
		"box_started_at":     st.BoxStartedAt,
		"process_started_at": p.sup.ProcessStartedAt().UTC().Format(time.RFC3339Nano),
		"uptime_sec":         int64(time.Since(p.sup.ProcessStartedAt()).Seconds()),
		"box_up":             st.BoxUp,
		"last_error":         st.LastError,
		"pull":               st.Pull,
		"config_mode":        configMode(st),
	}
	if p.subStat != nil {
		payload["subscribe"] = p.subStat()
	}
	if p.inbounds != nil {
		payload["inbounds_count"] = p.inbounds()
	}
	// online_users: not available without Clash API (ADR 0006); omit rather than invent.

	body, err := json.Marshal(payload)
	if err != nil {
		return p.recordErr(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, spec.URL, bytes.NewReader(body))
	if err != nil {
		return p.recordErr(err)
	}
	req.Header.Set("Content-Type", "application/json")
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
	resp, err := client.Do(req)
	if err != nil {
		return p.recordErr(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return p.recordErr(fmt.Errorf("heartbeat status %d", resp.StatusCode))
	}
	now := time.Now().UTC()
	p.mu.Lock()
	p.lastOK = &now
	p.lastErr = nil
	p.mu.Unlock()
	return nil
}

func configMode(st supervisor.StatusSnapshot) string {
	if st.Pull.Enabled {
		return "subscribed"
	}
	if st.ContentSHA256 != "" {
		return "direct_or_boot"
	}
	return "idle"
}

func (p *Pusher) recordErr(err error) error {
	msg := err.Error()
	p.mu.Lock()
	p.lastErr = &msg
	p.mu.Unlock()
	return err
}

func (p *Pusher) statePath() string {
	return filepath.Join(p.dataDir, stateFile)
}

func (p *Pusher) save() error {
	p.mu.Lock()
	rawPers := persisted{Present: true, Enabled: p.enabled, Spec: p.spec}
	p.mu.Unlock()
	raw, err := json.MarshalIndent(rawPers, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := p.statePath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, p.statePath())
}

func (p *Pusher) load() (persisted, error) {
	var out persisted
	raw, err := os.ReadFile(p.statePath())
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

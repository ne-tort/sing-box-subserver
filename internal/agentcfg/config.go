package agentcfg

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is agent settings (not sing-box dataplane JSON).
type Config struct {
	NodeID             string          `yaml:"node_id" json:"node_id"`
	Token              string          `yaml:"token" json:"token"`
	Listen             string          `yaml:"listen" json:"listen"`
	DataDir            string          `yaml:"data_dir" json:"data_dir"`
	HealthPublic       *bool           `yaml:"health_public" json:"health_public"`
	InsecurePublicBind bool            `yaml:"insecure_public_bind" json:"insecure_public_bind"`
	ProbeMS            int             `yaml:"probe_ms" json:"probe_ms"`
	Pull               PullConfig      `yaml:"pull" json:"pull"`
	Heartbeat          HeartbeatConfig `yaml:"heartbeat" json:"heartbeat"`
	Controlplane       ControlplaneConfig `yaml:"controlplane" json:"controlplane"`
	TLS                TLSConfig          `yaml:"tls" json:"tls"`
	Log                LogConfig          `yaml:"log" json:"log"`
}

// ControlplaneConfig seeds optional embedded CP (with_controlplane).
type ControlplaneConfig struct {
	PublicHost    string `yaml:"public_host" json:"public_host"`
	PublicPort    int    `yaml:"public_port" json:"public_port"`
	ExpiryTickSec int    `yaml:"expiry_tick_sec" json:"expiry_tick_sec"`
}

type PullConfig struct {
	Enabled     bool              `yaml:"enabled" json:"enabled"`
	URL         string            `yaml:"url" json:"url"`
	IntervalSec int               `yaml:"interval_sec" json:"interval_sec"`
	JitterSec   int               `yaml:"jitter_sec" json:"jitter_sec"`
	TimeoutSec  int               `yaml:"timeout_sec" json:"timeout_sec"`
	Headers     map[string]string `yaml:"headers" json:"headers"`
	TLSInsecure bool              `yaml:"tls_insecure" json:"tls_insecure"`
}

// HeartbeatConfig POSTs status snapshots to the panel (optional).
type HeartbeatConfig struct {
	Enabled     bool              `yaml:"enabled" json:"enabled"`
	URL         string            `yaml:"url" json:"url"`
	IntervalSec int               `yaml:"interval_sec" json:"interval_sec"`
	TimeoutSec  int               `yaml:"timeout_sec" json:"timeout_sec"`
	Headers     map[string]string `yaml:"headers" json:"headers"`
	TLSInsecure bool              `yaml:"tls_insecure" json:"tls_insecure"`
}

type TLSConfig struct {
	Cert string `yaml:"cert" json:"cert"`
	Key  string `yaml:"key" json:"key"`
}

type LogConfig struct {
	Level string `yaml:"level" json:"level"`
}

// HealthPublicEnabled returns whether /v1/health (and version) skip auth.
func (c *Config) HealthPublicEnabled() bool {
	if c.HealthPublic == nil {
		return true
	}
	return *c.HealthPublic
}

// PullInterval returns the configured pull interval or a default.
func (c *Config) PullInterval() time.Duration {
	sec := c.Pull.IntervalSec
	if sec <= 0 {
		sec = 60
	}
	return time.Duration(sec) * time.Second
}

func (c *Config) PullTimeout() time.Duration {
	sec := c.Pull.TimeoutSec
	if sec <= 0 {
		sec = 15
	}
	return time.Duration(sec) * time.Second
}

func (c *Config) PullJitter() time.Duration {
	sec := c.Pull.JitterSec
	if sec < 0 {
		sec = 0
	}
	return time.Duration(sec) * time.Second
}

// Load reads and validates agent YAML from path.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent config: %w", err)
	}
	return Parse(raw)
}

// Parse unmarshals and validates agent YAML bytes.
func Parse(raw []byte) (*Config, error) {
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse agent config: %w", err)
	}
	applyDefaults(&c)
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func applyDefaults(c *Config) {
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8080"
	}
	if c.DataDir == "" {
		c.DataDir = "/var/lib/subserver"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	if c.ProbeMS == 0 {
		c.ProbeMS = 300
	}
	if c.Pull.URL != "" && !c.Pull.Enabled {
		// URL present without enabled: treat as enabled for convenience.
		c.Pull.Enabled = true
	}
	if c.Pull.IntervalSec == 0 {
		c.Pull.IntervalSec = 60
	}
	if c.Pull.TimeoutSec == 0 {
		c.Pull.TimeoutSec = 15
	}
	if c.Pull.JitterSec == 0 && c.Pull.Enabled {
		c.Pull.JitterSec = 10
	}
	if c.Heartbeat.URL != "" && !c.Heartbeat.Enabled {
		c.Heartbeat.Enabled = true
	}
	if c.Heartbeat.IntervalSec == 0 {
		c.Heartbeat.IntervalSec = 30
	}
	if c.Heartbeat.TimeoutSec == 0 {
		c.Heartbeat.TimeoutSec = 10
	}
	if c.Controlplane.ExpiryTickSec == 0 {
		c.Controlplane.ExpiryTickSec = 60
	}
}

// ProbeDuration returns post-start settle time. Negative ProbeMS disables probe.
func (c *Config) ProbeDuration() time.Duration {
	if c.ProbeMS < 0 {
		return 0
	}
	return time.Duration(c.ProbeMS) * time.Millisecond
}

func (c *Config) HeartbeatInterval() time.Duration {
	sec := c.Heartbeat.IntervalSec
	if sec <= 0 {
		sec = 30
	}
	return time.Duration(sec) * time.Second
}

func (c *Config) HeartbeatTimeout() time.Duration {
	sec := c.Heartbeat.TimeoutSec
	if sec <= 0 {
		sec = 10
	}
	return time.Duration(sec) * time.Second
}

// HasTLS reports whether TLS cert/key are configured.
func (c *Config) HasTLS() bool {
	return c.TLS.Cert != "" && c.TLS.Key != ""
}

// IsLoopbackListen reports whether listen host is loopback / unspecified empty host treated as all-interfaces.
func (c *Config) IsLoopbackListen() bool {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		// bare port or invalid — treat as non-loopback to force explicit policy
		return false
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

// CheckBindPolicy enforces NFR-5: public bind requires TLS or insecure_public_bind.
func (c *Config) CheckBindPolicy() error {
	if c.IsLoopbackListen() {
		return nil
	}
	if c.HasTLS() {
		return nil
	}
	if c.InsecurePublicBind {
		return nil
	}
	return fmt.Errorf("agent config: listen %q is not loopback; set tls.cert/key or insecure_public_bind=true", c.Listen)
}

// Validate checks required fields.
func (c *Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.NodeID) == "" {
		missing = append(missing, "node_id")
	}
	if strings.TrimSpace(c.Token) == "" {
		missing = append(missing, "token")
	}
	if strings.TrimSpace(c.Listen) == "" {
		missing = append(missing, "listen")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		missing = append(missing, "data_dir")
	}
	if len(missing) > 0 {
		return fmt.Errorf("agent config: missing required fields: %s", strings.Join(missing, ", "))
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("agent config: invalid log.level %q", c.Log.Level)
	}
	if c.Pull.Enabled && strings.TrimSpace(c.Pull.URL) == "" {
		return fmt.Errorf("agent config: pull.enabled requires pull.url")
	}
	if c.Heartbeat.Enabled && strings.TrimSpace(c.Heartbeat.URL) == "" {
		return fmt.Errorf("agent config: heartbeat.enabled requires heartbeat.url")
	}
	if (c.TLS.Cert == "") != (c.TLS.Key == "") {
		return fmt.Errorf("agent config: tls.cert and tls.key must both be set")
	}
	if err := c.CheckBindPolicy(); err != nil {
		return err
	}
	return nil
}

//go:build with_controlplane

// Package smoke runs inbound connectivity checks via an ephemeral client box.
// Live dataplane config is never modified (see docs/controlplane/adr/0007-inbound-smoke.md).
package smoke

import (
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/materialize"
)

// SmokeUserName is the reserved system account used for subscription outbounds.
const SmokeUserName = "__cp_smoke__"

// DefaultURLs mirrors the short client UrlTestBank connectivity list.
// Trailing IP-literal URL keeps probes useful when container DNS is broken.
var DefaultURLs = []string{
	"http://connectivitycheck.gstatic.com/generate_204",
	"http://www.gstatic.com/generate_204",
	"http://cp.cloudflare.com",
	"http://www.msftconnecttest.com/connecttest.txt",
	"http://1.1.1.1/",
}

const (
	defaultTimeout   = 2500 * time.Millisecond
	maxTimeout       = 10 * time.Second
	defaultParallel  = 4
	boxSettle        = 200 * time.Millisecond
)

// Request configures one smoke run.
type Request struct {
	Sets            []string      `json:"sets,omitempty"`
	Presets         []string      `json:"presets,omitempty"`
	Timeout         time.Duration `json:"-"`
	TimeoutMs       int           `json:"timeout_ms,omitempty"`
	URLs            []string      `json:"urls,omitempty"`
	IncludeVariants bool          `json:"include_variants,omitempty"`
}

// EffectiveTimeout returns a capped positive timeout.
func (r Request) EffectiveTimeout() time.Duration {
	d := r.Timeout
	if d <= 0 && r.TimeoutMs > 0 {
		d = time.Duration(r.TimeoutMs) * time.Millisecond
	}
	if d <= 0 {
		d = defaultTimeout
	}
	if d > maxTimeout {
		d = maxTimeout
	}
	return d
}

// EffectiveURLs returns probe URLs (override or defaults).
func (r Request) EffectiveURLs() []string {
	out := make([]string, 0, len(r.URLs))
	for _, u := range r.URLs {
		if u != "" {
			out = append(out, u)
		}
	}
	if len(out) == 0 {
		return append([]string{}, DefaultURLs...)
	}
	return out
}

// Result is one outbound → inbound probe outcome.
type Result struct {
	Set         string `json:"set"`
	Preset      string `json:"preset"`
	InboundTag  string `json:"inbound_tag"`
	OutboundTag string `json:"outbound_tag"`
	Variant     string `json:"variant,omitempty"`
	Profile     string `json:"profile,omitempty"`
	OK          bool   `json:"ok"`
	LatencyMs   *int   `json:"latency_ms,omitempty"`
	URL         string `json:"url,omitempty"`
	Error       string `json:"error,omitempty"`
	Skipped     bool   `json:"skipped,omitempty"`
	SkipReason  string `json:"skip_reason,omitempty"`
}

// Report is the API payload for a completed run.
type Report struct {
	DurationMs int64    `json:"duration_ms"`
	FinishedAt string   `json:"finished_at,omitempty"` // RFC3339 UTC; set when persisted / returned from last
	Results    []Result `json:"results"`
}

// BindingSmoke is a short per-binding summary derived from the last report.
type BindingSmoke struct {
	OK         bool   `json:"ok"`
	LatencyMs  *int   `json:"latency_ms,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// IndexBySetPreset picks one Result per set+preset (first match wins).
func (r *Report) IndexBySetPreset() map[string]Result {
	out := make(map[string]Result)
	if r == nil {
		return out
	}
	for _, res := range r.Results {
		key := res.Set + "\x00" + res.Preset
		if _, ok := out[key]; ok {
			continue
		}
		out[key] = res
	}
	return out
}

// SmokeFor returns a binding summary for set+preset, or nil if absent.
func (r *Report) SmokeFor(setName, preset string) *BindingSmoke {
	if r == nil {
		return nil
	}
	for _, res := range r.Results {
		if res.Set != setName || res.Preset != preset {
			continue
		}
		return &BindingSmoke{
			OK:         res.OK,
			LatencyMs:  res.LatencyMs,
			Skipped:    res.Skipped,
			FinishedAt: r.FinishedAt,
		}
	}
	return nil
}

// Input is everything needed to build subscription outbounds and dial live inbounds.
type Input struct {
	User               domain.User
	Sets               []domain.InboundSet
	PublicHost         string
	TLS                domain.TLSProfile
	CertManager        domain.CertManager
	RealityAssignments map[string]domain.RealityAssignment
	Hub                *domain.WgHub
	TLSCertPath        string
	SlotTLS            map[string]materialize.SlotTLSMaterial
}

// IsSmokeUser reports whether name/id is the reserved smoke account.
func IsSmokeUser(name string) bool {
	return name == SmokeUserName
}

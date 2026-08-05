// Package freedns registers deterministic / dynamic hostnames for a public IPv4
// via sslip.io, nip.io and dyn.addr.tools. Provider failures are soft-skipped.
package freedns

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ProviderSSLip     = "sslip"
	ProviderNIP       = "nip"
	ProviderAddrTools = "addrtools"

	StatusOK      = "ok"
	StatusSkipped = "skipped"
	StatusError   = "error"

	addrToolsURL     = "https://dyn.addr.tools"
	addrHeartbeatMin = 24 * time.Hour
	addrForceAfter   = 60 * 24 * time.Hour // refresh before 90d expiry
	dnsVerifyTries   = 6
	dnsVerifyWait    = 2 * time.Second
)

// ProviderStatus is one free-DNS provider outcome.
type ProviderStatus struct {
	Host       string     `json:"host,omitempty"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	LastUpdate *time.Time `json:"last_update,omitempty"`
}

// State is persisted at controlplane/free_dns.json.
type State struct {
	IPv4       string                    `json:"ipv4"`
	AddrSecret string                    `json:"addr_secret,omitempty"`
	AddrHost   string                    `json:"addr_host,omitempty"`
	Providers  map[string]ProviderStatus `json:"providers"`
	UpdatedAt  time.Time                 `json:"updated_at"`
}

// Hosts returns successfully registered hostnames (stable sorted).
func (st State) Hosts() []string {
	out := make([]string, 0, 3)
	for _, id := range []string{ProviderSSLip, ProviderNIP, ProviderAddrTools} {
		p, ok := st.Providers[id]
		if !ok || p.Status != StatusOK || strings.TrimSpace(p.Host) == "" {
			continue
		}
		out = append(out, strings.ToLower(strings.TrimSpace(p.Host)))
	}
	sort.Strings(out)
	return out
}

// SourceOf returns provider id for a hostname, or "manual".
func (st State) SourceOf(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	for id, p := range st.Providers {
		if p.Status == StatusOK && strings.ToLower(strings.TrimSpace(p.Host)) == h {
			return id
		}
	}
	return "manual"
}

// Path returns free_dns.json under dataDir/controlplane.
func Path(dataDir string) string {
	return filepath.Join(dataDir, "controlplane", "free_dns.json")
}

// LoadState reads free_dns.json; missing file → empty state.
func LoadState(dataDir string) (State, error) {
	raw, err := os.ReadFile(Path(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return State{Providers: map[string]ProviderStatus{}}, nil
		}
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, err
	}
	if st.Providers == nil {
		st.Providers = map[string]ProviderStatus{}
	}
	return st, nil
}

// SaveState writes free_dns.json atomically.
func SaveState(dataDir string, st State) error {
	dir := filepath.Join(dataDir, "controlplane")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	st.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path := Path(dataDir)
	tmp, err := os.CreateTemp(dir, ".free_dns.json.tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Chmod(0o600)
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// DashedIPv4Host builds {a}-{b}-{c}-{d}.{zone} for a dotted IPv4.
func DashedIPv4Host(ip net.IP, zone string) (string, error) {
	v4 := ip.To4()
	if v4 == nil {
		return "", fmt.Errorf("not an ipv4 address")
	}
	zone = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(zone)), ".")
	return fmt.Sprintf("%d-%d-%d-%d.%s", v4[0], v4[1], v4[2], v4[3], zone), nil
}

// AddrToolsHost returns sha224(secret).dyn.addr.tools (first 56 hex chars of SHA-224).
func AddrToolsHost(secret string) string {
	sum := sha256.Sum224([]byte(secret))
	return hex.EncodeToString(sum[:]) + ".dyn.addr.tools"
}

// ResolvePublicIPv4 returns an IPv4 from publicHost (literal or single A lookup).
func ResolvePublicIPv4(ctx context.Context, publicHost string) (net.IP, error) {
	h := strings.TrimSpace(publicHost)
	if h == "" {
		return nil, fmt.Errorf("empty public_host")
	}
	if ip := net.ParseIP(h); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4, nil
		}
		return nil, fmt.Errorf("public_host is not ipv4")
	}
	resolver := net.DefaultResolver
	addrs, err := resolver.LookupIP(ctx, "ip4", h)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("no A records for %s", h)
	}
	v4 := addrs[0].To4()
	if v4 == nil {
		return nil, fmt.Errorf("resolved non-ipv4 for %s", h)
	}
	return v4, nil
}

type lookupFunc func(ctx context.Context, host string) ([]net.IP, error)
type httpDoFunc func(req *http.Request) (*http.Response, error)

// Options configures Ensure / Refresh.
type Options struct {
	DataDir string
	IPv4    net.IP
	// Optional overrides for tests.
	Lookup lookupFunc
	HTTPDo httpDoFunc
	Now    func() time.Time
	// RandSecret generates addr.tools secret when missing.
	RandSecret func() (string, error)
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now().UTC()
}

func (o Options) lookup(ctx context.Context, host string) ([]net.IP, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if o.Lookup != nil {
		return o.Lookup(ctx, host)
	}
	return net.DefaultResolver.LookupIP(ctx, "ip4", host)
}

func (o Options) httpDo(req *http.Request) (*http.Response, error) {
	if o.HTTPDo != nil {
		return o.HTTPDo(req)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	return client.Do(req)
}

func (o Options) randSecret() (string, error) {
	if o.RandSecret != nil {
		return o.RandSecret()
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "hiddify-" + hex.EncodeToString(b), nil
}

func verifyA(ctx context.Context, o Options, host string, want net.IP) error {
	want4 := want.To4()
	if want4 == nil {
		return fmt.Errorf("want ipv4")
	}
	var last error
	for i := 0; i < dnsVerifyTries; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(dnsVerifyWait):
			}
		}
		addrs, err := o.lookup(ctx, host)
		if err != nil {
			last = err
			continue
		}
		for _, a := range addrs {
			if a.To4() != nil && a.To4().Equal(want4) {
				return nil
			}
		}
		last = fmt.Errorf("A records do not include %s", want4.String())
	}
	return last
}

func setProv(st *State, id string, host, status, errMsg string, updated *time.Time) {
	if st.Providers == nil {
		st.Providers = map[string]ProviderStatus{}
	}
	st.Providers[id] = ProviderStatus{
		Host:       host,
		Status:     status,
		Error:      errMsg,
		LastUpdate: updated,
	}
}

// Ensure registers sslip/nip/addr for ipv4. Soft-skips failed providers.
func Ensure(ctx context.Context, o Options) (State, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if o.DataDir == "" {
		return State{}, fmt.Errorf("data_dir required")
	}
	ip := o.IPv4.To4()
	if ip == nil {
		return State{}, fmt.Errorf("ipv4 required")
	}
	st, err := LoadState(o.DataDir)
	if err != nil {
		return State{}, err
	}
	st.IPv4 = ip.String()

	// sslip
	if host, err := DashedIPv4Host(ip, "sslip.io"); err != nil {
		setProv(&st, ProviderSSLip, "", StatusSkipped, err.Error(), nil)
	} else if err := verifyA(ctx, o, host, ip); err != nil {
		setProv(&st, ProviderSSLip, host, StatusSkipped, err.Error(), nil)
	} else {
		setProv(&st, ProviderSSLip, host, StatusOK, "", nil)
	}

	// nip
	if host, err := DashedIPv4Host(ip, "nip.io"); err != nil {
		setProv(&st, ProviderNIP, "", StatusSkipped, err.Error(), nil)
	} else if err := verifyA(ctx, o, host, ip); err != nil {
		setProv(&st, ProviderNIP, host, StatusSkipped, err.Error(), nil)
	} else {
		setProv(&st, ProviderNIP, host, StatusOK, "", nil)
	}

	// addr.tools
	if err := ensureAddrTools(ctx, o, &st, ip); err != nil {
		// ensureAddrTools already set provider status; keep going
		_ = err
	}

	if err := SaveState(o.DataDir, st); err != nil {
		return st, err
	}
	return st, nil
}

func ensureAddrTools(ctx context.Context, o Options, st *State, ip net.IP) error {
	secret := strings.TrimSpace(st.AddrSecret)
	if secret == "" {
		s, err := o.randSecret()
		if err != nil {
			setProv(st, ProviderAddrTools, "", StatusSkipped, err.Error(), nil)
			return err
		}
		secret = s
		st.AddrSecret = secret
	}
	host := AddrToolsHost(secret)
	st.AddrHost = host
	if err := updateAddrTools(ctx, o, secret, ip.String()); err != nil {
		setProv(st, ProviderAddrTools, host, StatusSkipped, err.Error(), nil)
		return err
	}
	now := o.now()
	if err := verifyA(ctx, o, host, ip); err != nil {
		setProv(st, ProviderAddrTools, host, StatusSkipped, err.Error(), &now)
		return err
	}
	setProv(st, ProviderAddrTools, host, StatusOK, "", &now)
	return nil
}

func updateAddrTools(ctx context.Context, o Options, secret, ip string) error {
	form := url.Values{}
	form.Set("secret", secret)
	form.Set("ip", ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, addrToolsURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := o.httpDo(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("dyn.addr.tools http %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	if strings.TrimSpace(string(body)) != "OK" {
		return fmt.Errorf("dyn.addr.tools unexpected body %q", strings.TrimSpace(string(body)))
	}
	return nil
}

// RefreshAddrTools updates dyn.addr.tools if due. No-op when not configured / not due.
func RefreshAddrTools(ctx context.Context, o Options) (State, bool, error) {
	st, err := LoadState(o.DataDir)
	if err != nil {
		return st, false, err
	}
	p, ok := st.Providers[ProviderAddrTools]
	if !ok || strings.TrimSpace(st.AddrSecret) == "" {
		return st, false, nil
	}
	ip := o.IPv4.To4()
	if ip == nil {
		if parsed := net.ParseIP(st.IPv4); parsed != nil {
			ip = parsed.To4()
		}
	}
	if ip == nil {
		return st, false, nil
	}
	now := o.now()
	due := true
	if p.LastUpdate != nil {
		age := now.Sub(p.LastUpdate.UTC())
		if age < addrHeartbeatMin {
			due = false
		}
		// Always due if approaching 90d expiry window.
		if age >= addrForceAfter {
			due = true
		}
	}
	if !due && p.Status == StatusOK {
		return st, false, nil
	}
	st.IPv4 = ip.String()
	if err := ensureAddrTools(ctx, o, &st, ip); err != nil {
		_ = SaveState(o.DataDir, st)
		return st, true, err
	}
	if err := SaveState(o.DataDir, st); err != nil {
		return st, true, err
	}
	return st, true, nil
}

// Payload returns a JSON-serializable summary for API responses.
func (st State) Payload() map[string]any {
	provs := map[string]any{}
	for id, p := range st.Providers {
		m := map[string]any{
			"host":   p.Host,
			"status": p.Status,
		}
		if p.Error != "" {
			m["error"] = p.Error
		}
		if p.LastUpdate != nil {
			m["last_update"] = p.LastUpdate.UTC().Format(time.RFC3339)
		}
		provs[id] = m
	}
	return map[string]any{
		"ipv4":      st.IPv4,
		"addr_host": st.AddrHost,
		"hosts":     st.Hosts(),
		"providers": provs,
		"updated_at": func() any {
			if st.UpdatedAt.IsZero() {
				return nil
			}
			return st.UpdatedAt.UTC().Format(time.RFC3339)
		}(),
	}
}

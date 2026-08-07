//go:build with_controlplane

package controlplane

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/demuxgroups"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/freedns"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/materialize"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/paramvalidate"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	cpi18n "github.com/ne-tort/sing-box-subserver/internal/controlplane/presets/i18n"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/smoke"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ssh"
)

// Service is the embedded controlplane.
type Service struct {
	cfg                   Deps
	store                 *store.Store
	log                   *slog.Logger
	mu  sync.Mutex

	mgmtTLS   mgmtCertCache
	acmeWatch struct {
		mu        sync.Mutex
		enteredAt time.Time
		everReady bool
		lostSince time.Time
	}

	trafficHooks TrafficHooks

	smokeMu      sync.Mutex
	smokeRunning bool
}

// SetTrafficHooks wires optional traffic module bridge.
func (s *Service) SetTrafficHooks(h TrafficHooks) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trafficHooks = h
}

// New constructs the CP service (with_controlplane builds only).
func New(d Deps) *Service {
	st, err := store.Open(d.DataDir)
	if err != nil {
		if d.Logger != nil {
			d.Logger.Error("controlplane store open failed", "err", err)
		}
		return nil
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{cfg: d, store: st, log: log}
}

func (s *Service) OnLeaveOwnership() {
	if s == nil {
		return
	}
	_ = s.store.ClearActiveSets()
}

// Bootstrap ensures Default SSL profile exists, registers free-DNS + managed ACME SSL profiles, and rematerializes if we own the dataplane.
func (s *Service) Bootstrap(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.ensureSSLProfiles(); err != nil && s.log != nil {
		s.log.Warn("controlplane ssl profiles bootstrap failed", "err", err)
	}
	s.bootstrapFreeDNSSSL(ctx)
	s.noteSSLACMEReadiness()
	if s.cfg.Owner == nil {
		return
	}
	s.reconcileOwnershipOnBoot(ctx)
}

func (s *Service) Run(ctx context.Context) {
	if s == nil {
		return
	}
	tickSec := 60
	if s.cfg.Cfg != nil && s.cfg.Cfg.Controlplane.ExpiryTickSec > 0 {
		tickSec = s.cfg.Cfg.Controlplane.ExpiryTickSec
	}
	// ACME watchdog ticks more often than expiry; reuse min(tick, 30s).
	watchSec := tickSec
	if watchSec > 30 {
		watchSec = 30
	}
	t := time.NewTicker(time.Duration(tickSec) * time.Second)
	w := time.NewTicker(time.Duration(watchSec) * time.Second)
	defer t.Stop()
	defer w.Stop()
	var lastFP string
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.C:
			s.acmeWatchdog(ctx)
			s.freeDNSHeartbeat(ctx)
		case <-t.C:
			s.mu.Lock()
			beforeElig := s.eligibilityMapLocked()
			_ = s.applyTrafficResetsLocked(time.Now().UTC())
			s.notifyBecameIneligibleLocked(beforeElig)
			fp := s.eligibilityFingerprintLocked()
			s.mu.Unlock()
			if s.cfg.Owner == nil || s.cfg.Owner.Owner() != configowner.ModeControlplane {
				lastFP = fp
				continue
			}
			if fp != lastFP {
				lastFP = fp
				_ = s.rematerialize(ctx)
			}
		}
	}
}

func (s *Service) eligibilityFingerprintLocked() string {
	users, err := s.store.LoadUsers()
	if err != nil {
		return ""
	}
	st, err := s.store.LoadState()
	if err != nil {
		return ""
	}
	now := time.Now().UTC()
	parts := append([]string{}, st.ActiveSets...)
	sort.Strings(parts)
	for _, u := range users {
		if u.Eligible(now) {
			parts = append(parts, "u:"+u.ID)
		} else {
			parts = append(parts, "x:"+u.ID)
		}
	}
	return strings.Join(parts, ",")
}

func (s *Service) eligibilityMapLocked() map[string]bool {
	users, err := s.store.LoadUsers()
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	out := make(map[string]bool, len(users))
	for _, u := range users {
		out[u.ID] = u.Eligible(now)
	}
	return out
}

// notifyBecameIneligibleLocked kicks sessions for users that transitioned eligible→ineligible.
func (s *Service) notifyBecameIneligibleLocked(before map[string]bool) {
	if s.trafficHooks == nil || before == nil {
		return
	}
	after := s.eligibilityMapLocked()
	var ids []string
	for id, was := range before {
		if was && !after[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	s.trafficHooks.OnBecameIneligible(ids)
}

func (s *Service) applyTrafficResetsLocked(now time.Time) bool {
	users, err := s.store.LoadUsers()
	if err != nil {
		return false
	}
	changed := false
	var resetIDs []string
	for i := range users {
		u := &users[i]
		if u.TrafficResetAt == nil || u.TrafficResetAt.After(now) {
			continue
		}
		u.TrafficUsedBytes = 0
		u.TrafficIngressBytes = 0
		u.TrafficEpoch++
		resetIDs = append(resetIDs, u.ID)
		if u.TrafficResetPeriodSec != nil && *u.TrafficResetPeriodSec > 0 {
			next := now.Add(time.Duration(*u.TrafficResetPeriodSec) * time.Second)
			u.TrafficResetAt = &next
		} else {
			u.TrafficResetAt = nil
		}
		u.UpdatedAt = now
		changed = true
	}
	if !changed {
		return false
	}
	if err := s.store.SaveUsers(users); err != nil {
		return false
	}
	if s.trafficHooks != nil {
		for _, id := range resetIDs {
			s.trafficHooks.OnTrafficReset(id)
		}
	}
	return true
}

// ListUserIDs returns all local user ids (for traffic bridge polling).
func (s *Service) ListUserIDs() []string {
	if s == nil {
		return nil
	}
	users, err := s.store.LoadUsers()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(users))
	for _, u := range users {
		if u.DeletedAt != nil {
			continue
		}
		out = append(out, u.ID)
	}
	return out
}

// ApplyTrafficUsage writes dataplane totals from the traffic module into users.json.
// Syncable users update ingress only (hub owns traffic_used_bytes / global quota).
// Local users mirror store → used + ingress.
func (s *Service) ApplyTrafficUsage(ctx context.Context, updates map[string]uint64) (bool, error) {
	if s == nil || len(updates) == 0 {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	users, err := s.store.LoadUsers()
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	beforeElig := s.eligibilityMapLocked()
	before := s.eligibilityFingerprintLocked()
	changed := false
	for i := range users {
		v, ok := updates[users[i].ID]
		if !ok {
			continue
		}
		u := &users[i]
		if u.SyncActive() {
			if v < u.TrafficIngressBytes {
				u.TrafficEpoch++
			}
			if u.TrafficIngressBytes != v {
				u.TrafficIngressBytes = v
				u.UpdatedAt = now
				changed = true
			}
			continue
		}
		if v < u.TrafficUsedBytes || v < u.TrafficIngressBytes {
			u.TrafficEpoch++
		}
		if u.TrafficUsedBytes != v || u.TrafficIngressBytes != v {
			u.TrafficUsedBytes = v
			u.TrafficIngressBytes = v
			u.UpdatedAt = now
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	if err := s.store.SaveUsers(users); err != nil {
		return false, err
	}
	s.notifyBecameIneligibleLocked(beforeElig)
	after := s.eligibilityFingerprintLocked()
	if before == after {
		return false, nil
	}
	if err := s.rematerializeLocked(ctx, false); err != nil {
		return true, err
	}
	return true, nil
}

func (s *Service) Register(mux *http.ServeMux, requireAuth func(func(http.ResponseWriter, *http.Request)) http.HandlerFunc) {
	mux.HandleFunc("GET /v1/controlplane/heads", requireAuth(s.handleHeadsGet))
	mux.HandleFunc("POST /v1/controlplane/commits", requireAuth(s.handleCommitsPost))
	mux.HandleFunc("GET /v1/controlplane/commits", requireAuth(s.handleCommitsList))
	mux.HandleFunc("GET /v1/controlplane/commits/{id}", requireAuth(s.handleCommitsGet))
	mux.HandleFunc("GET /v1/controlplane/status", requireAuth(s.handleStatus))
	mux.HandleFunc("GET /v1/controlplane/status/details", requireAuth(s.handleStatusDetails))
	mux.HandleFunc("GET /v1/controlplane/users/export", requireAuth(s.handleUsersExport))
	mux.HandleFunc("POST /v1/controlplane/users/import", requireAuth(s.handleUsersImport))
	mux.HandleFunc("GET /v1/controlplane/users/sync/metrics", requireAuth(s.handleUsersSyncMetricsGet))
	mux.HandleFunc("POST /v1/controlplane/users/sync/metrics", requireAuth(s.handleUsersSyncMetricsPost))
	mux.HandleFunc("POST /v1/controlplane/users/sync/membership", requireAuth(s.handleUsersSyncMembership))
	mux.HandleFunc("GET /v1/controlplane/users", requireAuth(s.handleUsersList))
	mux.HandleFunc("POST /v1/controlplane/users", requireAuth(s.handleUsersCreate))
	mux.HandleFunc("GET /v1/controlplane/users/{id}", requireAuth(s.handleUsersGet))
	mux.HandleFunc("PATCH /v1/controlplane/users/{id}", requireAuth(s.handleUsersPatch))
	mux.HandleFunc("DELETE /v1/controlplane/users/{id}", requireAuth(s.handleUsersDelete))
	mux.HandleFunc("POST /v1/controlplane/users/{id}/sync", requireAuth(s.handleUsersSyncToggle))
	mux.HandleFunc("POST /v1/controlplane/users/{id}/rotate-token", requireAuth(s.handleUsersRotateToken))
	mux.HandleFunc("POST /v1/controlplane/users/{id}/rotate-creds", requireAuth(s.handleUsersRotateCreds))
	mux.HandleFunc("PUT /v1/controlplane/users/{id}/creds", requireAuth(s.handleUsersPutCreds))
	mux.HandleFunc("GET /v1/controlplane/presets", requireAuth(s.handlePresetsList))
	mux.HandleFunc("GET /v1/controlplane/presets/{name}", requireAuth(s.handlePresetsGet))
	mux.HandleFunc("GET /v1/controlplane/protocols", requireAuth(s.handleProtocolsList))
	mux.HandleFunc("GET /v1/controlplane/protocols/{tag}", requireAuth(s.handleProtocolsGet))
	mux.HandleFunc("GET /v1/controlplane/demux-groups", requireAuth(s.handleDemuxGroupsList))
	mux.HandleFunc("GET /v1/controlplane/demux-groups/{tag}", requireAuth(s.handleDemuxGroupsGet))
	mux.HandleFunc("GET /v1/controlplane/demux-groups/{tag}/substitutions", requireAuth(s.handleDemuxGroupsSubstitutions))
	mux.HandleFunc("POST /v1/controlplane/sets/from-demux-group", requireAuth(s.handleSetsFromDemuxGroup))
	mux.HandleFunc("POST /v1/controlplane/sets/from-presets", requireAuth(s.handleSetsFromPresets))
	mux.HandleFunc("GET /v1/controlplane/client/bootstrap", requireAuth(s.handleClientBootstrap))
	mux.HandleFunc("GET /v1/controlplane/ports/availability", requireAuth(s.handlePortsAvailability))
	mux.HandleFunc("GET /v1/controlplane/sets", requireAuth(s.handleSetsList))
	mux.HandleFunc("POST /v1/controlplane/sets", requireAuth(s.handleSetsCreate))
	mux.HandleFunc("GET /v1/controlplane/sets/{name}", requireAuth(s.handleSetsGet))
	mux.HandleFunc("GET /v1/controlplane/subscription-tags", requireAuth(s.handleSubscriptionTags))
	mux.HandleFunc("GET /v1/controlplane/sets/{name}/subscription-tags", requireAuth(s.handleSetSubscriptionTags))
	mux.HandleFunc("PUT /v1/controlplane/sets/{name}", requireAuth(s.handleSetsPut))
	mux.HandleFunc("DELETE /v1/controlplane/sets/{name}", requireAuth(s.handleSetsDelete))
	mux.HandleFunc("POST /v1/controlplane/sets/{name}/activate", requireAuth(s.handleSetsActivate))
	mux.HandleFunc("POST /v1/controlplane/sets/{name}/deactivate", requireAuth(s.handleSetsDeactivate))
	mux.HandleFunc("POST /v1/controlplane/smoke", requireAuth(s.handleSmoke))
	mux.HandleFunc("GET /v1/controlplane/smoke/last", requireAuth(s.handleSmokeLast))
	mux.HandleFunc("GET /v1/controlplane/ssl", requireAuth(s.handleSSLList))
	mux.HandleFunc("POST /v1/controlplane/ssl", requireAuth(s.handleSSLCreate))
	mux.HandleFunc("GET /v1/controlplane/ssl/{id}", requireAuth(s.handleSSLGet))
	mux.HandleFunc("PUT /v1/controlplane/ssl/{id}", requireAuth(s.handleSSLPut))
	mux.HandleFunc("DELETE /v1/controlplane/ssl/{id}", requireAuth(s.handleSSLDelete))
	mux.HandleFunc("POST /v1/controlplane/ssl/{id}/regenerate", requireAuth(s.handleSSLRegenerate))
	mux.HandleFunc("GET /v1/controlplane/config", requireAuth(s.handleConfigGet))
	mux.HandleFunc("GET /v1/controlplane/config/dns", requireAuth(s.handleConfigDNSGet))
	mux.HandleFunc("PUT /v1/controlplane/config/dns", requireAuth(s.handleConfigDNSPut))
	mux.HandleFunc("DELETE /v1/controlplane/config/dns", requireAuth(s.handleConfigDNSDelete))
	mux.HandleFunc("GET /v1/controlplane/config/route", requireAuth(s.handleConfigRouteGet))
	mux.HandleFunc("PUT /v1/controlplane/config/route", requireAuth(s.handleConfigRoutePut))
	mux.HandleFunc("DELETE /v1/controlplane/config/route", requireAuth(s.handleConfigRouteDelete))
	mux.HandleFunc("GET /v1/controlplane/config/outbounds", requireAuth(s.handleConfigOutboundsGet))
	mux.HandleFunc("PUT /v1/controlplane/config/outbounds", requireAuth(s.handleConfigOutboundsPut))
	mux.HandleFunc("DELETE /v1/controlplane/config/outbounds", requireAuth(s.handleConfigOutboundsDelete))
	mux.HandleFunc("GET /v1/controlplane/reality", requireAuth(s.handleRealityGet))
	mux.HandleFunc("PUT /v1/controlplane/reality", requireAuth(s.handleRealityPut))
	mux.HandleFunc("GET /v1/controlplane/wg", requireAuth(s.handleWgGet))
	mux.HandleFunc("PUT /v1/controlplane/wg", requireAuth(s.handleWgPut))
	mux.HandleFunc("POST /v1/controlplane/wg/regenerate-obfuscation", requireAuth(s.handleWgRegenerateObfuscation))
	mux.HandleFunc("GET /v1/sub/{token}", s.handleSub)
}

func randomToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func randomPassword() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func randomBase64Key(n int) (string, error) {
	if n <= 0 {
		return "", fmt.Errorf("invalid key length %d", n)
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// randomCurve25519Private returns a clamped X25519 private key (base64.RawURLEncoding),
// compatible with lx DERP wire.ParseKey.
func randomCurve25519Private() (string, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return "", err
	}
	k[0] &= 248
	k[31] &= 127
	k[31] |= 64
	return base64.RawURLEncoding.EncodeToString(k[:]), nil
}

func curve25519PublicFromPrivate(privB64 string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(privB64)
	if err != nil {
		raw, err = base64.StdEncoding.DecodeString(privB64)
	}
	if err != nil {
		return "", fmt.Errorf("curve25519 private key: %w", err)
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("curve25519 private key length %d, want 32", len(raw))
	}
	pub, err := curve25519.X25519(raw, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(pub), nil
}

func randomSSHEd25519PrivatePEM() (string, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(block)), nil
}

func sshPublicFromPrivatePEM(privPEM string) (string, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privPEM))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

func (s *Service) ensureCreds(u *domain.User) (bool, error) {
	if u.Creds == nil {
		u.Creds = map[string]map[string]any{}
	}
	changed := false
	for _, p := range presets.All() {
		if presetIsEndpoint(p) {
			continue // WG creds via ensureWgUserCreds when hub enabled
		}
		creds := presets.CredsFor(u.Creds, p.Name)
		if creds == nil {
			creds = map[string]any{}
		}
		presetChanged := false
		for _, f := range p.CredFields {
			if !credFieldEmpty(creds[f]) {
				continue
			}
			gen := ""
			if p.CredGenerators != nil {
				gen = p.CredGenerators[f]
			}
			if f == "public_key" {
				// Filled after private_key via ensureSSHPublicFromPrivate / ensureCurve25519Public.
				continue
			}
			val, err := generateCredField(f, gen)
			if err != nil {
				return false, err
			}
			creds[f] = val
			presetChanged = true
		}
		wantPub := false
		wantSSHPub := false
		for _, f := range p.CredFields {
			if f == "public_key" {
				wantPub = true
				if p.CredGenerators != nil && p.CredGenerators["private_key"] == "ssh_ed25519" {
					wantSSHPub = true
				}
			}
		}
		if wantSSHPub {
			sshChanged, err := ensureSSHPublicFromPrivate(creds)
			if err != nil {
				return false, err
			}
			if sshChanged {
				presetChanged = true
			}
		} else {
			pubChanged, err := ensureCurve25519Public(creds, wantPub)
			if err != nil {
				return false, err
			}
			if pubChanged {
				presetChanged = true
			}
		}
		for _, key := range presets.CredKeysForEnsure(p) {
			if presetChanged || u.Creds[key] == nil {
				u.Creds[key] = creds
				changed = true
			}
		}
	}
	return changed, nil
}

// ensurePeerSecrets fills set-level secrets declared by preset PeerSecretFields.
func (s *Service) ensurePeerSecrets(set *domain.InboundSet) (bool, error) {
	if set == nil {
		return false, nil
	}
	if set.PeerSecrets == nil {
		set.PeerSecrets = map[string]string{}
	}
	changed := false
	for _, b := range set.EffectiveBindings() {
		p, err := presets.Get(b.Preset)
		if err != nil || len(p.PeerSecretFields) == 0 {
			continue
		}
		canonical := p.Name
		for field, gen := range p.PeerSecretFields {
			key := canonical + "/" + field
			if strings.TrimSpace(set.PeerSecrets[key]) != "" {
				continue
			}
			if field == "public_key" {
				// Derived from private_key below.
				continue
			}
			val, err := generateCredField(field, gen)
			if err != nil {
				return false, err
			}
			s, ok := val.(string)
			if !ok || strings.TrimSpace(s) == "" {
				return false, fmt.Errorf("peer secret %q for %s: empty", field, canonical)
			}
			set.PeerSecrets[key] = s
			changed = true
		}
		if _, wantPub := p.PeerSecretFields["public_key"]; wantPub {
			privKey := canonical + "/private_key"
			pubKey := canonical + "/public_key"
			if strings.TrimSpace(set.PeerSecrets[pubKey]) == "" {
				priv := strings.TrimSpace(set.PeerSecrets[privKey])
				if priv == "" {
					return false, fmt.Errorf("peer secret public_key for %s requires private_key", canonical)
				}
				pub, err := curve25519PublicFromPrivate(priv)
				if err != nil {
					return false, fmt.Errorf("peer secret public_key for %s: %w", canonical, err)
				}
				set.PeerSecrets[pubKey] = pub
				changed = true
			}
		}
	}
	if changed && len(set.PeerSecrets) == 0 {
		set.PeerSecrets = nil
	}
	return changed, nil
}

func (s *Service) ensurePeerSecretsAll(sets []domain.InboundSet) ([]domain.InboundSet, bool, error) {
	changedAny := false
	out := make([]domain.InboundSet, len(sets))
	for i := range sets {
		set := sets[i]
		changed, err := s.ensurePeerSecrets(&set)
		if err != nil {
			return nil, false, err
		}
		if changed {
			changedAny = true
		}
		out[i] = set
	}
	return out, changedAny, nil
}

func (s *Service) publicHost(r *http.Request) string {
	if s.cfg.Cfg != nil && s.cfg.Cfg.Controlplane.PublicHost != "" {
		return s.cfg.Cfg.Controlplane.PublicHost
	}
	if r != nil && r.Host != "" {
		host, _, err := net.SplitHostPort(r.Host)
		if err == nil {
			return host
		}
		return r.Host
	}
	return "127.0.0.1"
}

func (s *Service) publicPort(r *http.Request) string {
	if s.cfg.Cfg != nil {
		if s.cfg.Cfg.Controlplane.PublicPort > 0 {
			return strconv.Itoa(s.cfg.Cfg.Controlplane.PublicPort)
		}
		if _, p, err := net.SplitHostPort(s.cfg.Cfg.Listen); err == nil && p != "" {
			return p
		}
	}
	// Request may be Host:port (management URL); PublicHost strips it — recover port here.
	if r != nil && r.Host != "" {
		if _, p, err := net.SplitHostPort(r.Host); err == nil && p != "" {
			return p
		}
	}
	return ""
}

func (s *Service) subscriptionURL(r *http.Request, token string) (path, url string) {
	path = "/v1/sub/" + token
	host := s.publicHost(r)
	port := s.publicPort(r)
	// CP builds always serve management/sub over HTTPS (CP TLS profile).
	scheme := "https"
	if port != "" && port != "80" && port != "443" {
		url = fmt.Sprintf("%s://%s:%s%s", scheme, host, port, path)
	} else {
		url = fmt.Sprintf("%s://%s%s", scheme, host, path)
	}
	return path, url
}

// scrubStaleActiveSets drops active_sets names that no longer exist in sets.json.
func (s *Service) scrubStaleActiveSets() {
	st, err := s.store.LoadState()
	if err != nil {
		return
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		return
	}
	byName := map[string]struct{}{}
	for _, set := range sets {
		byName[set.Name] = struct{}{}
	}
	cleaned := make([]string, 0, len(st.ActiveSets))
	changed := false
	for _, n := range st.ActiveSets {
		if _, ok := byName[n]; ok {
			cleaned = append(cleaned, n)
			continue
		}
		changed = true
		if s.log != nil {
			s.log.Warn("controlplane scrubbing unknown active set", "name", n)
		}
	}
	if !changed {
		return
	}
	st.ActiveSets = cleaned
	_ = s.store.SaveState(st)
}

func (s *Service) activeSetObjects() ([]domain.InboundSet, error) {
	s.scrubStaleActiveSets()
	st, err := s.store.LoadState()
	if err != nil {
		return nil, err
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		return nil, err
	}
	// First occurrence wins — duplicate names are a store corruption that create
	// paths must reject (cp_name_conflict). Prefer stable GET/list semantics.
	byName := map[string]domain.InboundSet{}
	for _, set := range sets {
		if _, exists := byName[set.Name]; exists {
			continue
		}
		byName[set.Name] = set
	}
	names := append([]string{}, st.ActiveSets...)
	sort.Strings(names)
	out := make([]domain.InboundSet, 0, len(names))
	for _, n := range names {
		if set, ok := byName[n]; ok {
			out = append(out, set)
		}
	}
	return out, nil
}

func (s *Service) eligibleUsers(now time.Time) ([]domain.User, error) {
	users, err := s.store.LoadUsers()
	if err != nil {
		return nil, err
	}
	changed := false
	out := make([]domain.User, 0, len(users))
	for i := range users {
		c, err := s.ensureCreds(&users[i])
		if err != nil {
			return nil, err
		}
		if c {
			changed = true
		}
		if users[i].Eligible(now) {
			out = append(out, users[i])
		}
	}
	if changed {
		_ = s.store.SaveUsers(users)
	}
	return out, nil
}

func (s *Service) rematerialize(ctx context.Context) error {
	return s.rematerializeForce(ctx, false)
}

func (s *Service) rematerializeForce(ctx context.Context, forceReload bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rematerializeLocked(ctx, forceReload)
}

// publishTrafficPolicyLocked pushes users/sets into the traffic module (subjects + shaping).
// Uses all known sets so variant dataplane keys stay registered even when inactive.
func (s *Service) publishTrafficPolicyLocked() {
	if s.trafficHooks == nil {
		return
	}
	users, err := s.store.LoadUsers()
	if err != nil {
		return
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		return
	}
	s.trafficHooks.OnMaterialize(users, sets)
}

func (s *Service) rematerializeLocked(ctx context.Context, forceReload bool) error {
	if s.cfg.Owner == nil || s.cfg.Owner.Owner() != configowner.ModeControlplane {
		s.publishTrafficPolicyLocked()
		return nil
	}
	sets, err := s.activeSetObjects()
	if err != nil {
		return err
	}
	hub, err := s.store.LoadWgHub()
	if err != nil {
		return err
	}
	if len(sets) == 0 && !hub.Enabled {
		_ = s.ensureSSLProfiles()
		sslEarly, _ := s.loadSSLProfiles()
		hasACME := false
		for _, sp := range sslEarly {
			if sp.IsACME() {
				hasACME = true
				break
			}
		}
		if !hasACME {
			s.publishTrafficPolicyLocked()
			return nil
		}
		// Fall through with empty sets — Build emits certificate_providers only.
	}
	users, err := s.eligibleUsers(time.Now().UTC())
	if err != nil {
		return err
	}
	if hub.Enabled {
		hubChanged, err := s.ensureWgHubSecrets(&hub, false)
		if err != nil {
			return err
		}
		users, credsChanged, err := s.ensureWgUserCreds(users)
		if err != nil {
			return err
		}
		if hubChanged {
			if err := s.store.SaveWgHub(hub); err != nil {
				return err
			}
		}
		if credsChanged {
			all, err := s.store.LoadUsers()
			if err != nil {
				return err
			}
			byID := map[string]domain.User{}
			for _, u := range users {
				byID[u.ID] = u
			}
			for i := range all {
				if u, ok := byID[all[i].ID]; ok {
					all[i].Creds = u.Creds
				}
			}
			if err := s.store.SaveUsers(all); err != nil {
				return err
			}
		}
	}
	sets, peerChanged, err := s.ensurePeerSecretsAll(sets)
	if err != nil {
		return err
	}
	if peerChanged {
		all, err := s.store.LoadSets()
		if err != nil {
			return err
		}
		byName := map[string]domain.InboundSet{}
		for _, st := range sets {
			byName[st.Name] = st
		}
		for i := range all {
			if st, ok := byName[all[i].Name]; ok {
				all[i].PeerSecrets = st.PeerSecrets
			}
		}
		if err := s.store.SaveSets(all); err != nil {
			return err
		}
	}
	host := "127.0.0.1"
	if s.cfg.Cfg != nil && s.cfg.Cfg.Controlplane.PublicHost != "" {
		host = s.cfg.Cfg.Controlplane.PublicHost
	}
	if err := s.ensureSSLProfiles(); err != nil {
		return err
	}
	sslList, err := s.loadSSLProfiles()
	if err != nil {
		return err
	}
	sslForMat := s.sslProfilesWithResolvedACMEEmail(sslList)
	defHost := host
	defCert, defKey := sslCertPaths(s.cfg.DataDir, defaultSSLProfileID)
	for _, sp := range sslList {
		if sp.ID == defaultSSLProfileID {
			if sn := sp.ServerName(); sn != "" {
				defHost = sn
			}
			defCert, defKey = sslCertPaths(s.cfg.DataDir, sp.ID)
			break
		}
	}
	profile := domain.DefaultSelfSigned(defHost)
	fragments, err := s.store.LoadConfigFragments()
	if err != nil {
		return err
	}
	realityAssignments := map[string]domain.RealityAssignment{}
	if hasRealityPreset(sets) {
		realityCfg, err := s.loadRealityConfig()
		if err != nil {
			return err
		}
		realityAssignments, _, err = s.ensureRealityAssignments(sets, realityCfg.Profiles)
		if err != nil {
			return err
		}
	}
	in := materialize.Input{
		ActiveSets:         sets,
		Users:              users,
		PublicHost:         host,
		DataDir:            s.cfg.DataDir,
		TLS:                profile,
		TLSCertPath:        defCert,
		TLSKeyPath:         defKey,
		SSLProfiles:        sslForMat,
		DNS:                fragments.EffectiveDNS(),
		Route:              fragments.EffectiveRoute(),
		Outbounds:          fragments.EffectiveOutbounds(),
		RealityAssignments: realityAssignments,
	}
	echByID := map[string]materialize.ECHMaterial{}
	for _, sp := range sslList {
		if !sp.ECHEnabled {
			continue
		}
		keyPath, cfgPath := sslECHPaths(s.cfg.DataDir, sp.ID)
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		echByID[sp.ID] = materialize.ECHMaterial{KeyPath: keyPath, ConfigPEM: string(raw)}
	}
	if len(echByID) > 0 {
		in.ECHByID = echByID
	}
	pemChanged := false
	slotTLS := map[string]materialize.SlotTLSMaterial{}
	for _, set := range sets {
		if !set.HasDemux() {
			continue
		}
		for _, b := range set.EffectiveBindings() {
			sni := strings.ToLower(strings.TrimSpace(b.Params["demux_sni"]))
			if sni == "" || net.ParseIP(sni) != nil || strings.HasSuffix(sni, ".local") {
				continue
			}
			p, err := presets.Get(b.Preset)
			if err != nil || domain.BindingUsesReality(p, b.Params) {
				continue
			}
			sslID := strings.TrimSpace(b.Params[domain.BindingParamSSLProfile])
			if sslID == "" {
				sslID = defaultSSLProfileID
			}
			if sp, ok, _ := s.findSSLProfile(sslID); ok && sp.IsACME() {
				continue
			}
			cert, key, changed, err := ensureSlotSelfSigned(s.cfg.DataDir, sni)
			if err != nil {
				return err
			}
			if changed {
				pemChanged = true
			}
			slotTLS[sni] = materialize.SlotTLSMaterial{CertPath: cert, KeyPath: key}
		}
	}
	if len(slotTLS) > 0 {
		in.SlotTLS = slotTLS
	}
	if hub.Enabled {
		h := hub
		in.WgHub = &h
	}
	raw, err := materialize.Build(in)
	if err != nil {
		s.recordMaterializeResult(false, err, false, "", materializeErrorCode(err))
		return err
	}
	res, err := s.cfg.Supervisor.Apply(ctx, supervisor.ApplyRequest{
		Raw:    raw,
		Source: configstore.SourceControlplane,
		Force:  forceReload || pemChanged,
	})
	if err != nil {
		s.recordMaterializeResult(false, err, false, "", "cp_apply_failed")
		return err
	}
	if hub.Enabled {
		_ = applyWgForwardRules(hub)
	} else {
		_ = applyWgForwardRules(domain.WgHub{Enabled: false})
	}
	s.recordMaterializeResult(true, nil, res.Noop, res.SHA256, "")
	s.publishTrafficPolicyLocked()
	return nil
}

func (s *Service) acmeWatchdog(ctx context.Context) {
	s.noteSSLACMEReadiness()
	ok, reason := s.shouldACMEFallback()
	if !ok {
		return
	}
	if s.log != nil {
		s.log.Warn("ssl ACME not ready", "reason", reason)
	}
	s.mgmtTLS.mu.Lock()
	s.mgmtTLS.cert = nil
	s.mgmtTLS.source = ""
	s.mgmtTLS.mu.Unlock()
	_ = ctx
}

func (s *Service) noteSSLACMEReadiness() {
	_ = s.ensureSSLProfiles()
	list, err := s.loadSSLProfiles()
	if err != nil {
		return
	}
	hasACME := false
	allReady := true
	for _, p := range list {
		if !p.IsACME() {
			continue
		}
		hasACME = true
		st := s.computeSSLStatus(p)
		if st.State != domain.SSLStateReady {
			allReady = false
		}
	}
	if !hasACME {
		return
	}
	s.noteACMEModeEntered()
	s.noteACMEReady(allReady)
}

func (s *Service) ensureFreeDNS(ctx context.Context) {
	if s == nil || s.cfg.DataDir == "" {
		return
	}
	ip, err := s.resolveBootstrapIPv4(ctx)
	if err != nil {
		if s.log != nil {
			s.log.Debug("free-dns: skip ensure", "err", err)
		}
		return
	}
	st, err := freedns.Ensure(ctx, freedns.Options{DataDir: s.cfg.DataDir, IPv4: ip})
	if err != nil && s.log != nil {
		s.log.Warn("free-dns: ensure failed", "err", err)
		return
	}
	if s.log != nil && len(st.Hosts()) > 0 {
		s.log.Info("free-dns: hosts ready", "hosts", st.Hosts())
	}
}

func (s *Service) freeDNSHeartbeat(ctx context.Context) {
	ip, err := s.resolveBootstrapIPv4(ctx)
	if err != nil {
		return
	}
	// First ensure providers when state is empty (bootstrap race / IP change).
	st, err := freedns.LoadState(s.cfg.DataDir)
	if err != nil || len(st.Hosts()) == 0 || st.IPv4 != ip.String() {
		st, err = freedns.Ensure(ctx, freedns.Options{DataDir: s.cfg.DataDir, IPv4: ip})
		if err != nil && s.log != nil {
			s.log.Warn("free-dns: ensure failed", "err", err)
		}
	}
	st, did, err := freedns.RefreshAddrTools(ctx, freedns.Options{DataDir: s.cfg.DataDir, IPv4: ip})
	if err != nil && s.log != nil {
		s.log.Warn("free-dns: addr.tools heartbeat failed", "err", err)
	} else if did && s.log != nil {
		s.log.Debug("free-dns: addr.tools heartbeat", "host", st.AddrHost)
	}
	// Keep managed ACME profiles aligned (reuse stable ids; soft-skip errors).
	s.ensureFreeDNSSSLProfiles(ctx)
}

func (s *Service) clientBootstrapSSL() map[string]any {
	out := map[string]any{
		"profiles": []any{},
		"ready":    true,
	}
	_ = s.ensureSSLProfiles()
	list, err := s.loadSSLProfiles()
	if err != nil {
		return out
	}
	profiles := make([]any, 0, len(list))
	allReady := true
	hasACME := false
	for _, p := range list {
		st := s.computeSSLStatus(p)
		profiles = append(profiles, map[string]any{
			"id":     p.ID,
			"name":   p.Name,
			"type":   p.Type,
			"domain": p.Domain,
			"ip":     p.IP,
			"state":  st.State,
		})
		if p.IsACME() {
			hasACME = true
			if st.State != domain.SSLStateReady {
				allReady = false
			}
		}
	}
	out["profiles"] = profiles
	if hasACME {
		out["ready"] = allReady
	}
	return out
}

func okJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

func failJSON(w http.ResponseWriter, code int, errCode, msg string) {
	failJSONData(w, code, errCode, msg, nil)
}

func failJSONData(w http.ResponseWriter, code int, errCode, msg string, extra map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	body := map[string]any{
		"ok":    false,
		"error": map[string]any{"code": errCode, "message": msg},
	}
	for k, v := range extra {
		if k == "ok" || k == "error" {
			continue
		}
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}

// validateSetHTTP maps validateSet errors to stable HTTP status + error.code for clients.
func validateSetHTTP(err error) (code int, errCode string) {
	code, errCode = 400, "cp_invalid_set"
	if err == nil {
		return code, errCode
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "cp_port_exhausted"):
		return 409, "cp_port_exhausted"
	case strings.Contains(msg, "cp_port_conflict"):
		return 409, "cp_port_conflict"
	case strings.Contains(msg, "cp_unknown_preset"):
		return code, "cp_unknown_preset"
	case strings.Contains(msg, "cp_invalid_demux"):
		return code, "cp_invalid_demux"
	case strings.Contains(msg, "cp_invalid_bindings"):
		return code, "cp_invalid_bindings"
	case strings.Contains(msg, "cp_invalid_preset"):
		return code, "cp_invalid_preset"
	default:
		return code, errCode
	}
}

// setPublicView is the list/get shape for inbound sets (active flag for clients).
// Peer secrets are omitted unless includeSecrets is true (?secrets=1).
func (s *Service) setPublicView(set domain.InboundSet, active bool) map[string]any {
	return s.setPublicViewOpts(set, active, false)
}

func (s *Service) setPublicViewOpts(set domain.InboundSet, active, includeSecrets bool) map[string]any {
	last, _ := s.store.LoadSmokeLast()
	return s.setPublicViewOptsSmoke(set, active, includeSecrets, last)
}

func (s *Service) setPublicViewOptsSmoke(set domain.InboundSet, active, includeSecrets bool, last *smoke.Report) map[string]any {
	bindings := set.EffectiveBindings()
	bindingViews := make([]map[string]any, 0, len(bindings))
	var setSmoke *smoke.BindingSmoke
	for _, b := range bindings {
		view := map[string]any{
			"preset": b.Preset,
		}
		if len(b.SubscriptionTags) > 0 {
			view["subscription_tags"] = b.SubscriptionTags
		}
		if len(b.EnabledUserVariants) > 0 {
			view["enabled_user_variants"] = b.EnabledUserVariants
		}
		if len(b.EnabledClientProfiles) > 0 {
			view["enabled_client_profiles"] = b.EnabledClientProfiles
		}
		if b.CredentialInstancePolicy != "" {
			view["credential_instance_policy"] = b.CredentialInstancePolicy
		}
		if len(b.Params) > 0 {
			view["params"] = b.Params
		}
		if sm := last.SmokeFor(set.Name, b.Preset); sm != nil {
			view["smoke"] = sm
			if setSmoke == nil {
				setSmoke = sm
			} else if !sm.Skipped && (setSmoke.Skipped || (!sm.OK && setSmoke.OK)) {
				setSmoke = sm
			}
		}
		bindingViews = append(bindingViews, view)
	}
	m := map[string]any{
		"name":        set.Name,
		"description": set.Description,
		"listen":      set.Listen,
		"listen_port": set.ListenPort,
		"presets":     uniqueSetPresets(bindings),
		"bindings":    bindingViews,
		"has_demux":   set.HasDemux(),
		"active":      active,
		"created_at":  set.CreatedAt,
		"updated_at":  set.UpdatedAt,
	}
	if setSmoke != nil {
		m["smoke"] = setSmoke
	}
	if len(set.DemuxTemplate) > 0 {
		m["demux_template"] = set.DemuxTemplate
	}
	if len(set.MemberPorts) > 0 {
		m["member_ports"] = set.MemberPorts
	}
	if len(set.SlotSNIs) > 0 {
		m["slot_snis"] = set.SlotSNIs
	}
	if set.DemuxGroup != "" {
		m["demux_group"] = set.DemuxGroup
	}
	if includeSecrets && len(set.PeerSecrets) > 0 {
		m["peer_secrets"] = set.PeerSecrets
	} else if len(set.PeerSecrets) > 0 {
		m["has_peer_secrets"] = true
	}
	return m
}

func redactUser(u domain.User, secrets bool) map[string]any {
	m := map[string]any{
		"id":                       u.ID,
		"name":                     u.Name,
		"enabled":                  u.Enabled,
		"created_at":               u.CreatedAt,
		"updated_at":               u.UpdatedAt,
		"expires_at":               u.ExpiresAt,
		"traffic_limit_bytes":      u.TrafficLimitBytes,
		"traffic_used_bytes":       u.TrafficUsedBytes,
		"traffic_ingress_bytes":    u.TrafficIngressBytes,
		"traffic_epoch":            u.TrafficEpoch,
		"traffic_reset_at":         u.TrafficResetAt,
		"traffic_reset_period_sec": u.TrafficResetPeriodSec,
		"speed_up_bytes_per_sec":   u.SpeedUpBytesPerSec,
		"speed_down_bytes_per_sec": u.SpeedDownBytesPerSec,
		"has_token":                u.SubToken != "",
		"sync_id":                  u.SyncID,
		"sync_mode":                u.EffectiveSyncMode(),
		"sync_enabled":             u.SyncEnabled,
		"revision":                 u.Revision,
		"origin":                   u.Origin,
	}
	if u.DeletedAt != nil {
		m["deleted_at"] = u.DeletedAt
	}
	names := make([]string, 0, len(u.Creds))
	for k := range u.Creds {
		names = append(names, k)
	}
	sort.Strings(names)
	m["cred_presets"] = names
	if secrets {
		m["sub_token"] = u.SubToken
		m["creds"] = u.Creds
	}
	return m
}

func contains(ss []string, x string) bool {
	for _, s := range ss {
		if s == x {
			return true
		}
	}
	return false
}

func removeStr(ss []string, x string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s != x {
			out = append(out, s)
		}
	}
	return out
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func parseTimePtr(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, err
		}
	}
	t = t.UTC()
	return &t, nil
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	okJSONETag(w, r, s.buildStatusPayload(false))
}

func (s *Service) handleStatusDetails(w http.ResponseWriter, r *http.Request) {
	okJSONETag(w, r, s.buildStatusPayload(true))
}

func (s *Service) buildStatusPayload(details bool) map[string]any {
	users, _ := s.store.LoadUsers()
	sets, _ := s.store.LoadSets()
	st, _ := s.store.LoadState()
	now := time.Now().UTC()
	elig := 0
	for _, u := range users {
		if u.Eligible(now) {
			elig++
		}
	}
	mode := "idle"
	if s.cfg.Owner != nil {
		mode = string(s.cfg.Owner.Owner())
	}
	out := map[string]any{
		"config_mode":             mode,
		"active_sets":             st.ActiveSets,
		"users_total":             len(users),
		"users_eligible":          elig,
		"presets_count":           len(presets.All()),
		"demux_groups_count":      demuxgroups.Count(),
		"sets_count":              len(sets),
		"demux_in_binary":         demuxInBinary,
		"last_materialize_sha256": st.LastMaterializeSHA256,
		"last_materialize_at":     st.LastMaterializeAt,
	}
	if st.Materialize != nil {
		out["materialize_status"] = st.Materialize
	}
	if len(st.OwnerTransitions) > 0 {
		n := len(st.OwnerTransitions)
		if n > 5 {
			n = 5
		}
		out["owner_transitions_recent"] = st.OwnerTransitions[len(st.OwnerTransitions)-n:]
	}
	_ = s.ensureSSLProfiles()
	if list, err := s.loadSSLProfiles(); err == nil {
		sslOut := make([]any, 0, len(list))
		for _, p := range list {
			st := s.computeSSLStatus(p)
			sslOut = append(sslOut, map[string]any{
				"id": p.ID, "name": p.Name, "type": p.Type, "state": st.State,
			})
		}
		out["ssl_profiles"] = sslOut
		if def, ok, _ := s.findSSLProfile(defaultSSLProfileID); ok {
			st := s.computeSSLStatus(def)
			out["tls_material_status"] = map[string]any{
				"ssl_profile_id": def.ID,
				"ssl_state":      st.State,
				"cert_path":      st.CertPath,
				"key_path":       st.KeyPath,
				"ready":          st.State == domain.SSLStateReady,
				"mgmt_https":     true,
				"mgmt_cert_source": s.mgmtCertSource(),
			}
		}
	}
	if fd, err := freedns.LoadState(s.cfg.DataDir); err == nil {
		out["free_dns"] = fd.Payload()
	}
	if hub, err := s.store.LoadWgHub(); err == nil {
		out["wg_hub"] = map[string]any{"enabled": hub.Enabled, "listen_port": hub.ListenPort, "profile": hub.Profile}
	}
	if rc, err := s.loadRealityConfig(); err == nil {
		out["reality"] = s.realityStatusPayload(rc)
	}
	out["ownership_health"] = s.ownershipHealth(st)
	out["ready"] = s.computeClientReady(st, out)
	if proj := s.commitStatusProjection(); proj != nil {
		out["commit"] = proj
	}
	if details {
		out["owner_transitions"] = st.OwnerTransitions
		out["active_set_details"] = s.buildActiveSetDetails(st.ActiveSets, sets)
		if s.cfg.Supervisor != nil {
			snap := s.cfg.Supervisor.Status()
			out["supervisor"] = map[string]any{
				"state":          snap.State,
				"revision":       snap.Revision,
				"content_sha256": snap.ContentSHA256,
				"last_apply":     snap.LastApply,
				"box_up":         snap.BoxUp,
			}
		}
	}
	return out
}

// computeClientReady is the single wizard poll signal after activate/install.
func (s *Service) computeClientReady(st domain.State, payload map[string]any) map[string]any {
	reasons := make([]string, 0)
	ok := true
	oh, _ := payload["ownership_health"].(map[string]any)
	if oh != nil {
		if status, _ := oh["status"].(string); status != "" && status != "ok" {
			ok = false
			if issues, _ := oh["issues"].([]string); len(issues) > 0 {
				reasons = append(reasons, issues...)
			} else {
				reasons = append(reasons, "ownership_degraded")
			}
		}
	}
	live := s.cpDataplaneLive(st)
	wgEnabled := false
	if hub, okHub := payload["wg_hub"].(map[string]any); okHub {
		wgEnabled, _ = hub["enabled"].(bool)
	} else if hub, err := s.store.LoadWgHub(); err == nil {
		wgEnabled = hub.Enabled
	}
	if !live {
		ok = false
		reasons = append(reasons, "no_active_sets")
	}
	if st.Materialize != nil && st.Materialize.LastError != "" {
		ok = false
		reasons = append(reasons, "materialize_error")
	}
	if ms, okTLS := payload["tls_material_status"].(map[string]any); okTLS {
		if ready, _ := ms["ready"].(bool); !ready {
			ok = false
			reasons = append(reasons, "tls_not_ready")
		}
	}
	if ids := s.activeSSLACMEProfilesNotReady(st); len(ids) > 0 {
		ok = false
		reasons = append(reasons, "ssl_acme_not_ready")
		_ = ids
	}
	boxUp := false
	supState := ""
	boxState := "stopped"
	if s.cfg.Supervisor != nil {
		snap := s.cfg.Supervisor.Status()
		boxUp = snap.BoxUp
		supState = string(snap.State)
		switch {
		case snap.BoxUp && snap.State == supervisor.StateRunning:
			boxState = "running"
		case snap.LastError != nil && *snap.LastError != "":
			boxState = "failed"
		case snap.State == supervisor.StateDegraded:
			boxState = "failed"
		case snap.State == supervisor.StateStarting || snap.State == supervisor.StateApplying:
			boxState = "starting"
		default:
			boxState = "stopped"
		}
		if !snap.BoxUp || snap.State != supervisor.StateRunning {
			ok = false
			reasons = append(reasons, "dataplane_not_running")
		}
	}
	return map[string]any{
		"ok":               ok,
		"box_up":           boxUp,
		"box_state":        boxState,
		"supervisor_state": supState,
		"active_sets":      len(st.ActiveSets) > 0,
		"wg_hub":           wgEnabled,
		"reasons":          reasons,
		"context":          readyContext(live, wgEnabled, ok, reasons),
		"poll":             "GET /v1/controlplane/status → ready.ok == true",
	}
}

// readyContext separates install-wizard readiness from idle health.
func readyContext(live, wgEnabled, ok bool, reasons []string) string {
	if !live && !wgEnabled {
		for _, r := range reasons {
			if r == "no_active_sets" {
				return "idle" // no install yet — not a failed health check
			}
		}
	}
	if ok {
		return "install_ready"
	}
	return "degraded"
}

// activeSSLACMEProfilesNotReady returns IDs of ACME SSL profiles referenced by
// active sets whose leaf is not yet ready.
func (s *Service) activeSSLACMEProfilesNotReady(st domain.State) []string {
	if s == nil || s.store == nil || len(st.ActiveSets) == 0 {
		return nil
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		return nil
	}
	_ = s.ensureSSLProfiles()
	sslByID := map[string]domain.SSLProfile{}
	if list, err := s.loadSSLProfiles(); err == nil {
		for _, p := range list {
			sslByID[p.ID] = p
		}
	}
	active := map[string]struct{}{}
	for _, n := range st.ActiveSets {
		active[n] = struct{}{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, set := range sets {
		if _, ok := active[set.Name]; !ok {
			continue
		}
		for _, b := range set.EffectiveBindings() {
			if b.Params == nil {
				continue
			}
			id := strings.TrimSpace(b.Params[domain.BindingParamSSLProfile])
			if id == "" {
				id = defaultSSLProfileID
			}
			p, ok := sslByID[id]
			if !ok || !p.IsACME() {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			st := s.computeSSLStatus(p)
			if st.State != domain.SSLStateReady {
				out = append(out, id)
			}
		}
	}
	return out
}

func (s *Service) buildActiveSetDetails(active []string, sets []domain.InboundSet) []any {
	if len(active) == 0 {
		return nil
	}
	byName := map[string]domain.InboundSet{}
	for _, set := range sets {
		if _, exists := byName[set.Name]; exists {
			continue
		}
		byName[set.Name] = set
	}
	out := make([]any, 0, len(active))
	for _, name := range active {
		set, ok := byName[name]
		if !ok {
			out = append(out, map[string]any{"name": name, "missing": true})
			continue
		}
		bindings := set.EffectiveBindings()
		bindingDetails := make([]any, 0, len(bindings))
		for _, b := range bindings {
			p, err := presets.Get(b.Preset)
			if err != nil {
				continue
			}
			variantNames := []string{}
			for _, vv := range domain.UserVariantsForProtocol(p.Protocol, b, p.DefaultUserVariants) {
				variantNames = append(variantNames, vv.Name)
			}
			bindingDetails = append(bindingDetails, map[string]any{
				"inbound_tag":             fmt.Sprintf("cp-in-%s-%s", set.Name, b.Preset),
				"preset":                  b.Preset,
				"protocol":                p.Protocol,
				"enabled_user_variants":   variantNames,
				"enabled_client_profiles": b.EnabledClientProfiles,
				"subscription_tags":       b.SubscriptionTags,
			})
		}
		out = append(out, map[string]any{
			"name":        set.Name,
			"listen":      set.Listen,
			"listen_port": set.ListenPort,
			"has_demux":   set.HasDemux(),
			"bindings":    bindingDetails,
		})
	}
	return out
}

func (s *Service) rollbackFirstActivate(wasOwner bool, prev []string, trigger string) {
	if s.cfg.Owner == nil || wasOwner || len(prev) > 0 {
		return
	}
	_ = s.claimOwnership(configowner.ModeIdle, "activate_rollback", trigger)
}

func (s *Service) handleUsersList(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	includeDeleted := r.URL.Query().Get("include_deleted") == "1"
	out := make([]any, 0, len(users))
	for _, u := range users {
		if smoke.IsSmokeUser(u.Name) {
			continue
		}
		if !includeDeleted && u.DeletedAt != nil {
			continue
		}
		out = append(out, redactUser(u, false))
	}
	okJSONETag(w, r, out)
}

func (s *Service) handleUsersCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name                  string                    `json:"name"`
		Enabled               *bool                     `json:"enabled"`
		ExpiresAt             *string                   `json:"expires_at"`
		TrafficLimitBytes     *uint64                   `json:"traffic_limit_bytes"`
		TrafficResetAt        *string                   `json:"traffic_reset_at"`
		TrafficResetPeriodSec *uint64                   `json:"traffic_reset_period_sec"`
		SpeedUpBytesPerSec    int64                     `json:"speed_up_bytes_per_sec"`
		SpeedDownBytesPerSec  int64                     `json:"speed_down_bytes_per_sec"`
		Creds                 map[string]map[string]any `json:"creds"`
		SyncID                string                    `json:"sync_id"`
		SyncMode              string                    `json:"sync_mode"`
		SyncEnabled           *bool                     `json:"sync_enabled"`
		Origin                string                    `json:"origin"`
		Revision              *uint64                   `json:"revision"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		failJSON(w, 400, "bad_request", "name required")
		return
	}
	if smoke.IsSmokeUser(strings.TrimSpace(body.Name)) {
		failJSON(w, 400, "bad_request", "reserved system name")
		return
	}
	if err := validateCreds(body.Creds); err != nil {
		failJSON(w, 400, "cp_invalid_creds", strings.TrimPrefix(err.Error(), "cp_invalid_creds: "))
		return
	}
	syncID := strings.TrimSpace(body.SyncID)
	syncMode := strings.TrimSpace(body.SyncMode)
	if syncMode == "" {
		if syncID != "" {
			syncMode = domain.SyncModeIdentity
		} else {
			syncMode = domain.SyncModeLocal
		}
	}
	if syncMode != domain.SyncModeLocal && syncMode != domain.SyncModeIdentity && syncMode != domain.SyncModeFull {
		failJSON(w, 400, "bad_request", "invalid sync_mode")
		return
	}
	if syncMode != domain.SyncModeLocal && syncID == "" {
		var err error
		syncID, err = newSyncUUID()
		if err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
	}
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	for _, u := range users {
		if u.DeletedAt == nil && u.Name == body.Name {
			failJSON(w, 409, "cp_name_conflict", "name already exists")
			return
		}
		if syncID != "" && u.SyncID == syncID && u.DeletedAt == nil {
			failJSON(w, 409, "cp_sync_id_conflict", "sync_id already exists")
			return
		}
	}
	id, err := randomToken()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	tok, err := randomToken()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	now := time.Now().UTC()
	u := domain.User{
		ID:        id[:16],
		Name:      body.Name,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
		SubToken:  tok,
		Creds:     map[string]map[string]any{},
		SyncID:    syncID,
		SyncMode:  syncMode,
		Origin:    domain.OriginLocal,
		Revision:  1,
	}
	if body.Origin != "" {
		u.Origin = body.Origin
	}
	if body.Revision != nil {
		u.Revision = *body.Revision
	}
	if syncMode != domain.SyncModeLocal {
		u.SyncEnabled = true
	}
	if body.SyncEnabled != nil {
		u.SyncEnabled = *body.SyncEnabled
	}
	if body.Enabled != nil {
		u.Enabled = *body.Enabled
	}
	if body.ExpiresAt != nil {
		t, err := parseTimePtr(*body.ExpiresAt)
		if err != nil {
			failJSON(w, 400, "bad_request", err.Error())
			return
		}
		u.ExpiresAt = t
	}
	u.TrafficLimitBytes = body.TrafficLimitBytes
	u.TrafficResetPeriodSec = body.TrafficResetPeriodSec
	u.SpeedUpBytesPerSec = body.SpeedUpBytesPerSec
	u.SpeedDownBytesPerSec = body.SpeedDownBytesPerSec
	if body.TrafficResetAt != nil {
		t, err := parseTimePtr(*body.TrafficResetAt)
		if err != nil {
			failJSON(w, 400, "bad_request", err.Error())
			return
		}
		u.TrafficResetAt = t
	}
	mergeUserCreds(&u, body.Creds)
	if _, err := s.ensureCreds(&u); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	users = append(users, u)
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	path, url := s.subscriptionURL(r, u.SubToken)
	data := redactUser(u, true)
	data["subscription_path"] = path
	data["subscription_url"] = url
	// User is already persisted — rematerialize failure must not hide the create.
	if err := s.rematerialize(r.Context()); err != nil {
		data["rematerialize_warning"] = err.Error()
	}
	okJSON(w, 200, data)
}

func (s *Service) findUser(id string) ([]domain.User, int, error) {
	users, err := s.store.LoadUsers()
	if err != nil {
		return nil, -1, err
	}
	for i := range users {
		if users[i].ID == id {
			return users, i, nil
		}
	}
	return users, -1, store.ErrNotFound
}

func (s *Service) handleUsersGet(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	secrets := r.URL.Query().Get("secrets") == "1"
	data := redactUser(users[i], secrets)
	if secrets {
		path, url := s.subscriptionURL(r, users[i].SubToken)
		data["subscription_path"] = path
		data["subscription_url"] = url
	}
	okJSON(w, 200, data)
}

func (s *Service) handleUsersPatch(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var body map[string]any
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	u := &users[i]
	wasEligible := u.Eligible(time.Now().UTC())
	wasSync := u.SyncActive()
	if v, ok := body["name"].(string); ok && v != "" {
		if v != u.Name {
			for j := range users {
				if j != i && users[j].Name == v && users[j].DeletedAt == nil {
					failJSON(w, 409, "cp_name_conflict", "name already exists")
					return
				}
			}
		}
		u.Name = v
	}
	if v, ok := body["enabled"].(bool); ok {
		u.Enabled = v
	}
	if v, ok := body["sync_id"].(string); ok {
		v = strings.TrimSpace(v)
		if v != "" && v != u.SyncID {
			for j := range users {
				if j != i && users[j].SyncID == v && users[j].DeletedAt == nil {
					failJSON(w, 409, "cp_sync_id_conflict", "sync_id already exists")
					return
				}
			}
		}
		u.SyncID = v
	}
	if v, ok := body["sync_mode"].(string); ok {
		v = strings.TrimSpace(v)
		if v != domain.SyncModeLocal && v != domain.SyncModeIdentity && v != domain.SyncModeFull {
			failJSON(w, 400, "bad_request", "invalid sync_mode")
			return
		}
		u.SyncMode = v
		if v != domain.SyncModeLocal && u.SyncID == "" {
			sid, err := newSyncUUID()
			if err != nil {
				failJSON(w, 500, "internal", err.Error())
				return
			}
			u.SyncID = sid
		}
		if v != domain.SyncModeLocal && !u.SyncEnabled {
			// Enabling sync mode without explicit sync_enabled keeps prior flag;
			// first transition from local defaults to enabled.
			if body["sync_enabled"] == nil {
				u.SyncEnabled = true
			}
		}
	}
	if v, ok := body["sync_enabled"].(bool); ok {
		u.SyncEnabled = v
	}
	revisionSet := false
	if v, ok := body["revision"].(float64); ok {
		u.Revision = uint64(v)
		revisionSet = true
	}
	if _, ok := body["expires_at"]; ok {
		if body["expires_at"] == nil {
			u.ExpiresAt = nil
		} else if v, ok := body["expires_at"].(string); ok {
			t, err := parseTimePtr(v)
			if err != nil {
				failJSON(w, 400, "bad_request", err.Error())
				return
			}
			u.ExpiresAt = t
		}
	}
	if _, ok := body["traffic_limit_bytes"]; ok {
		if body["traffic_limit_bytes"] == nil {
			u.TrafficLimitBytes = nil
		} else if v, ok := body["traffic_limit_bytes"].(float64); ok {
			u64 := uint64(v)
			u.TrafficLimitBytes = &u64
		}
	}
	usedPatched := false
	if v, ok := body["traffic_used_bytes"].(float64); ok {
		newUsed := uint64(v)
		if newUsed < u.TrafficUsedBytes && !u.SyncActive() {
			u.TrafficEpoch++
			u.TrafficIngressBytes = newUsed
		}
		u.TrafficUsedBytes = newUsed
		usedPatched = true
	}
	if v, ok := body["traffic_ingress_bytes"].(float64); ok {
		newIng := uint64(v)
		if newIng < u.TrafficIngressBytes {
			u.TrafficEpoch++
		}
		u.TrafficIngressBytes = newIng
	}
	if v, ok := body["speed_up_bytes_per_sec"].(float64); ok {
		u.SpeedUpBytesPerSec = int64(v)
	}
	if v, ok := body["speed_down_bytes_per_sec"].(float64); ok {
		u.SpeedDownBytesPerSec = int64(v)
	}
	if _, ok := body["traffic_reset_at"]; ok {
		if body["traffic_reset_at"] == nil {
			u.TrafficResetAt = nil
		} else if v, ok := body["traffic_reset_at"].(string); ok {
			t, err := parseTimePtr(v)
			if err != nil {
				failJSON(w, 400, "bad_request", err.Error())
				return
			}
			u.TrafficResetAt = t
		}
	}
	if _, ok := body["traffic_reset_period_sec"]; ok {
		if body["traffic_reset_period_sec"] == nil {
			u.TrafficResetPeriodSec = nil
		} else if v, ok := body["traffic_reset_period_sec"].(float64); ok {
			u64 := uint64(v)
			u.TrafficResetPeriodSec = &u64
		}
	}
	u.UpdatedAt = time.Now().UTC()
	if !revisionSet {
		u.Revision++
	}
	// Local → syncable: seed ingress from prior used so hub can count it once.
	if !wasSync && u.SyncActive() {
		u.SeedIngressFromUsed()
	}
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	// Only sync traffic store for local users; syncable used_bytes is hub-owned.
	if usedPatched && s.trafficHooks != nil && !u.SyncActive() {
		s.trafficHooks.OnTrafficUsedPatched(u.ID, u.TrafficUsedBytes)
	}
	if wasEligible && !u.Eligible(time.Now().UTC()) && s.trafficHooks != nil {
		s.trafficHooks.OnBecameIneligible([]string{u.ID})
	}
	data := redactUser(*u, false)
	if err := s.rematerialize(r.Context()); err != nil {
		data["rematerialize_warning"] = err.Error()
	}
	okJSON(w, 200, data)
}

func (s *Service) handleUsersDelete(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	deletedID := users[i].ID
	hard := r.URL.Query().Get("hard") == "1"
	if hard {
		users = append(users[:i], users[i+1:]...)
	} else {
		now := time.Now().UTC()
		users[i].DeletedAt = &now
		users[i].Enabled = false
		users[i].UpdatedAt = now
		users[i].Revision++
	}
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if hub, err := s.store.LoadWgHub(); err == nil && hub.ExitUserID == deletedID {
		hub.ExitUserID = ""
		_ = s.store.SaveWgHub(hub)
	}
	if s.trafficHooks != nil {
		s.trafficHooks.OnBecameIneligible([]string{deletedID})
	}
	// User already deleted from store — rematerialize failure must not hide success.
	if err := s.rematerialize(r.Context()); err != nil {
		okJSON(w, 200, map[string]any{"deleted": true, "hard": hard, "rematerialize_warning": err.Error()})
		return
	}
	okJSON(w, 200, map[string]any{"deleted": true, "hard": hard})
}

func (s *Service) handleUsersRotateToken(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	tok, err := randomToken()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	users[i].SubToken = tok
	users[i].UpdatedAt = time.Now().UTC()
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	path, url := s.subscriptionURL(r, tok)
	okJSON(w, 200, map[string]any{
		"sub_token":         tok,
		"subscription_path": path,
		"subscription_url":  url,
	})
}

func (s *Service) handleUsersRotateCreds(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var body struct {
		Preset string `json:"preset"`
		Field  string `json:"field"`
	}
	// Empty body = rotate all creds (legacy).
	if r.ContentLength != 0 {
		if err := decodeBody(r, &body); err != nil {
			failJSON(w, 400, "bad_request", err.Error())
			return
		}
	}
	preset := strings.TrimSpace(body.Preset)
	field := strings.TrimSpace(body.Field)
	if users[i].Creds == nil {
		users[i].Creds = map[string]map[string]any{}
	}
	switch {
	case preset == "" && field == "":
		users[i].Creds = map[string]map[string]any{}
	case preset != "":
		keys := rotateCredKeysForPreset(preset)
		if len(keys) == 0 {
			failJSON(w, 400, "bad_request", "unknown preset")
			return
		}
		if field == "" {
			for _, k := range keys {
				delete(users[i].Creds, k)
			}
		} else {
			for _, k := range keys {
				m := users[i].Creds[k]
				if m == nil {
					continue
				}
				delete(m, field)
				users[i].Creds[k] = m
			}
		}
	default:
		failJSON(w, 400, "bad_request", "field requires preset")
		return
	}
	if _, err := s.ensureCreds(&users[i]); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	users[i].UpdatedAt = time.Now().UTC()
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if err := s.rematerialize(r.Context()); err != nil {
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	okJSON(w, 200, redactUser(users[i], true))
}

// rotateCredKeysForPreset resolves catalog keys (canonical + aliases) for one preset name/tag.
func rotateCredKeysForPreset(preset string) []string {
	for _, p := range presets.All() {
		if p.Name == preset {
			return presets.CredKeysForEnsure(p)
		}
		for _, a := range p.Aliases {
			if a == preset {
				return presets.CredKeysForEnsure(p)
			}
		}
	}
	// Fallback: treat as raw creds map key.
	return []string{preset}
}

func (s *Service) handleUsersPutCreds(w http.ResponseWriter, r *http.Request) {
	users, i, err := s.findUser(r.PathValue("id"))
	if err != nil {
		if err == store.ErrNotFound {
			failJSON(w, 404, "not_found", "user not found")
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var body struct {
		Creds map[string]map[string]any `json:"creds"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if body.Creds == nil {
		failJSON(w, 400, "bad_request", "creds required")
		return
	}
	if err := validateCreds(body.Creds); err != nil {
		failJSON(w, 400, "cp_invalid_creds", strings.TrimPrefix(err.Error(), "cp_invalid_creds: "))
		return
	}
	mergeUserCreds(&users[i], body.Creds)
	if _, err := s.ensureCreds(&users[i]); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	users[i].UpdatedAt = time.Now().UTC()
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if err := s.rematerialize(r.Context()); err != nil {
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	okJSON(w, 200, redactUser(users[i], true))
}

func requestLang(r *http.Request) string {
	if q := strings.TrimSpace(r.URL.Query().Get("lang")); q != "" {
		return domain.NormalizeLang(q)
	}
	if h := strings.TrimSpace(r.Header.Get("X-Lang")); h != "" {
		return domain.NormalizeLang(h)
	}
	if h := strings.TrimSpace(r.Header.Get("Accept-Language")); h != "" {
		// RFC 7231: take first language-range (ignore q-weights for now).
		first := strings.Split(h, ",")[0]
		first = strings.TrimSpace(strings.Split(first, ";")[0])
		if first != "" && first != "*" {
			return domain.NormalizeLang(first)
		}
	}
	return "ru"
}

func (s *Service) handlePresetsList(w http.ResponseWriter, r *http.Request) {
	lang := requestLang(r)
	protocolFilter := strings.TrimSpace(r.URL.Query().Get("protocol"))
	all := presets.All()
	out := make([]any, 0, len(all))
	for _, p := range all {
		if protocolFilter != "" && p.Protocol != protocolFilter {
			continue
		}
		inv, err := presets.GetInvariant(p.Name)
		if err != nil {
			continue
		}
		pp := inv.ToProtocolPreset(lang)
		title := cpi18n.Preset(pp.Name, "title", lang)
		if d := cpi18n.Preset(pp.Name, "description", lang); d != "" {
			pp.Description = d
		}
		if title == "" {
			title = pp.ShortName
		}
		item := map[string]any{
			"name":                      pp.Name,
			"tag":                       pp.Name,
			"protocol":                  pp.Protocol,
			"title":                     title,
			"description":               pp.Description,
			"short_name":                pp.ShortName,
			"traits":                    pp.Traits,
			"status":                    pp.Status,
			"aliases":                   pp.Aliases,
			"scores":                    pp.Scores,
			"demux_hints":               pp.DemuxHints,
			"cred_fields":               pp.CredFields,
			"cred_generators":           pp.CredGenerators,
			"peer_secret_fields":        pp.PeerSecretFields,
			"param_fields":              pp.ParamFields,
			"optional_param_fields":     pp.OptionalParamFields,
			"custom_preset":             pp.CustomPreset,
			"default_user_variants":     pp.DefaultUserVariants,
			"default_client_profiles":   pp.DefaultClientProfiles,
			"available_user_variants":   domain.UserVariantCatalog(pp.Protocol),
			"available_client_profiles": domain.ClientProfileCatalog(pp.Protocol),
			"params_schema":             buildParamsSchemaLang(pp, false, lang),
			"optional_params":           presetOptionalParamsLang(pp, lang),
		}
		if nets := networksFromTraits(pp.Traits); len(nets) > 0 {
			item["networks"] = nets
		}
		out = append(out, item)
	}
	okJSONETag(w, r, out)
}

func (s *Service) handlePresetsGet(w http.ResponseWriter, r *http.Request) {
	lang := requestLang(r)
	inv, err := presets.GetInvariant(r.PathValue("name"))
	if err != nil {
		failJSON(w, 404, "not_found", err.Error())
		return
	}
	if len(inv.InboundTemplate) == 0 {
		failJSON(w, 404, "not_found", "preset has no templates")
		return
	}
	pp := inv.ToProtocolPreset(lang)
	title := cpi18n.Preset(pp.Name, "title", lang)
	if d := cpi18n.Preset(pp.Name, "description", lang); d != "" {
		pp.Description = d
	}
	if title == "" {
		title = pp.ShortName
	}
	proto, _ := presets.GetProtocol(inv.Protocol)
	_, pDesc := domain.ResolveI18n(proto.I18n, lang)
	if ld := cpi18n.Protocol(proto.Tag, "description", lang); ld != "" {
		pDesc = ld
	}
	okJSONETag(w, r, map[string]any{
		"name":                      pp.Name,
		"tag":                       pp.Name,
		"protocol":                  pp.Protocol,
		"title":                     title,
		"description":               pp.Description,
		"short_name":                pp.ShortName,
		"traits":                    pp.Traits,
		"status":                    pp.Status,
		"aliases":                   pp.Aliases,
		"scores":                    pp.Scores,
		"demux_hints":               pp.DemuxHints,
		"requirements":              pp.Requirements,
		"cred_fields":               pp.CredFields,
		"cred_generators":           pp.CredGenerators,
		"peer_secret_fields":        pp.PeerSecretFields,
		"param_fields":              pp.ParamFields,
		"optional_param_fields":     pp.OptionalParamFields,
		"custom_preset":             pp.CustomPreset,
		"default_user_variants":     pp.DefaultUserVariants,
		"default_client_profiles":   pp.DefaultClientProfiles,
		"available_user_variants":   domain.UserVariantCatalog(pp.Protocol),
		"available_client_profiles": domain.ClientProfileCatalog(pp.Protocol),
		"client_notes":              inv.ClientNotes,
		"params_schema":             buildParamsSchemaLang(pp, true, lang),
		"optional_params":           presetOptionalParamsDetailLang(pp, lang),
		"inbound_template":          pp.InboundTemplate,
		"outbound_template":         pp.OutboundTemplate,
		"protocol_meta": map[string]any{
			"tag":         proto.Tag,
			"short_name":  proto.ShortName,
			"status":      proto.Status,
			"description": pDesc,
		},
	})
}

func (s *Service) handleProtocolsList(w http.ResponseWriter, r *http.Request) {
	lang := requestLang(r)
	out := make([]any, 0)
	for _, p := range presets.Protocols() {
		title, desc := domain.ResolveI18n(p.I18n, lang)
		if t := cpi18n.Protocol(p.Tag, "title", lang); t != "" {
			title = t
		}
		if d := cpi18n.Protocol(p.Tag, "description", lang); d != "" {
			desc = d
		}
		out = append(out, map[string]any{
			"tag":            p.Tag,
			"short_name":     p.ShortName,
			"status":         p.Status,
			"title":          title,
			"description":    desc,
			"invariant_tags": p.InvariantTags,
			"singbox_type":   p.SingBoxType,
		})
	}
	okJSONETag(w, r, out)
}

func (s *Service) handleProtocolsGet(w http.ResponseWriter, r *http.Request) {
	lang := requestLang(r)
	p, err := presets.GetProtocol(r.PathValue("tag"))
	if err != nil {
		failJSON(w, 404, "not_found", err.Error())
		return
	}
	title, desc := domain.ResolveI18n(p.I18n, lang)
	if t := cpi18n.Protocol(p.Tag, "title", lang); t != "" {
		title = t
	}
	if d := cpi18n.Protocol(p.Tag, "description", lang); d != "" {
		desc = d
	}
	invOut := make([]any, 0, len(p.InvariantTags))
	for _, tag := range p.InvariantTags {
		inv, err := presets.GetInvariant(tag)
		if err != nil {
			continue
		}
		pp := inv.ToProtocolPreset(lang)
		if d := cpi18n.Preset(pp.Name, "description", lang); d != "" {
			pp.Description = d
		}
		item := map[string]any{
			"tag":         pp.Name,
			"short_name":  pp.ShortName,
			"status":      pp.Status,
			"description": pp.Description,
			"traits":      pp.Traits,
			"scores":      pp.Scores,
			"demux_hints": pp.DemuxHints,
			"aliases":     pp.Aliases,
		}
		invOut = append(invOut, item)
	}
	okJSONETag(w, r, map[string]any{
		"tag":            p.Tag,
		"short_name":     p.ShortName,
		"status":         p.Status,
		"title":          title,
		"description":    desc,
		"singbox_type":   p.SingBoxType,
		"invariant_tags": p.InvariantTags,
		"invariants":     invOut,
	})
}

func (s *Service) validateSet(set domain.InboundSet, others []domain.InboundSet) error {
	bindings := set.EffectiveBindings()
	if set.Name == "" || set.ListenPort == 0 || len(bindings) == 0 {
		return fmt.Errorf("name, listen_port, presets required")
	}
	if set.Listen == "" {
		set.Listen = "::"
	}
	presetsList := uniqueSetPresets(bindings)
	if !set.HasDemux() && len(presetsList) != 1 {
		return fmt.Errorf("without demux exactly one preset required")
	}
	for _, pn := range presetsList {
		p, err := presets.Get(pn)
		if err != nil {
			return fmt.Errorf("cp_unknown_preset: %w", err)
		}
		if presetIsEndpoint(p) {
			return fmt.Errorf("cp_invalid_preset: wireguard endpoint presets are managed via PUT /v1/controlplane/wg (singleton hub)")
		}
	}
	seenBindingPreset := map[string]struct{}{}
	for _, b := range bindings {
		canonical, ok := presets.CanonicalTag(b.Preset)
		if !ok {
			canonical = b.Preset
		}
		if _, exists := seenBindingPreset[canonical]; exists {
			return fmt.Errorf("cp_invalid_bindings: duplicate preset binding %q (canonical %q)", b.Preset, canonical)
		}
		seenBindingPreset[canonical] = struct{}{}
		p, err := presets.Get(b.Preset)
		if err != nil {
			return fmt.Errorf("cp_unknown_preset: %w", err)
		}
		if err := s.validateBindingParams(p, b); err != nil {
			return err
		}
	}
	if err := validateDemuxUniqueSNITLS(set); err != nil {
		return err
	}
	myNets := portNetworks(set)
	for _, o := range others {
		if o.Name == set.Name || o.ListenPort != set.ListenPort {
			continue
		}
		otherNets := portNetworks(o)
		for _, n := range myNets {
			for _, on := range otherNets {
				if n == on {
					return fmt.Errorf("cp_port_conflict: port %d %s already used by set %q", set.ListenPort, n, o.Name)
				}
			}
		}
	}
	return nil
}

// validateDemuxUniqueSNITLS rejects duplicate demux_sni / params.sni among
// SNI-matched PEM-TLS bindings in a demux set (ClientHello match collision).
func validateDemuxUniqueSNITLS(set domain.InboundSet) error {
	if !set.HasDemux() {
		return nil
	}
	seen := map[string]string{}
	for _, b := range set.EffectiveBindings() {
		p, err := presets.Get(b.Preset)
		if err != nil {
			continue
		}
		if domain.BindingUsesReality(p, b.Params) {
			continue
		}
		if !domain.BindingNeedsPEMTLS(p, b.Params) {
			continue
		}
		sni := ""
		if b.Params != nil {
			sni = strings.TrimSpace(b.Params["demux_sni"])
		}
		if sni == "" {
			continue
		}
		sni = strings.ToLower(sni)
		if other, ok := seen[sni]; ok {
			return fmt.Errorf("cp_invalid_bindings: duplicate demux SNI %q for presets %q and %q", sni, other, b.Preset)
		}
		seen[sni] = b.Preset
	}
	return nil
}

func (s *Service) validateBindingParams(p domain.ProtocolPreset, b domain.SetBinding) error {
	if b.Params == nil {
		b.Params = map[string]string{}
	}
	operator := make(map[string]string, len(b.Params))
	for k, v := range b.Params {
		operator[k] = v
	}
	applyParamMetaDefaults(p, b.Params)
	if err := paramvalidate.Validate(p, b.Params); err != nil {
		return err
	}
	for k, v := range operator {
		v = strings.TrimSpace(v)
		if v == "" || isNeutralParamValue(v) {
			continue
		}
		if _, ok := p.ParamMeta[k]; !ok {
			continue
		}
		if !paramvalidate.Visible(p, k, b.Params) {
			return fmt.Errorf("cp_param_not_applicable: %s not applicable for current params", k)
		}
	}
	if err := validateVlessFlowConflicts(b.Params); err != nil {
		return err
	}
	legacyKeys := []string{
		"sni", "self_signed_sni",
		"tls_alpn", "tls_min_version", "tls_max_version",
		"tls_cipher_suites", "tls_curve_preferences", "ech",
	}
	for _, k := range legacyKeys {
		if strings.TrimSpace(b.Params[k]) != "" {
			return fmt.Errorf("cp_invalid_bindings: legacy params.%s rejected; use params.ssl_profile", k)
		}
	}
	sslID := strings.TrimSpace(b.Params[domain.BindingParamSSLProfile])
	if sslID != "" {
		if domain.BindingUsesReality(p, b.Params) {
			return fmt.Errorf("cp_invalid_bindings: params.ssl_profile not allowed for Reality preset %q", p.Name)
		}
		if !domain.BindingNeedsPEMTLS(p, b.Params) && !presetHasTrait(p, "tls_custom") {
			return fmt.Errorf("cp_invalid_bindings: params.ssl_profile only for TLS presets, got %q", p.Name)
		}
		if _, ok, err := s.findSSLProfile(sslID); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("cp_invalid_bindings: params.ssl_profile %q not found", sslID)
		}
	}
	return nil
}

func isNeutralParamValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "none", "0", "false":
		return true
	default:
		return false
	}
}

func validateVlessFlowConflicts(params map[string]string) error {
	flow := strings.ToLower(strings.TrimSpace(params["flow"]))
	if flow == "" || flow == "none" {
		return nil
	}
	vision := strings.Contains(flow, "vision") || flow == "xtls-rprx-vision"
	if !vision {
		return nil
	}
	tr := strings.ToLower(strings.TrimSpace(params["transport"]))
	if tr != "" && tr != "tcp" {
		return fmt.Errorf("cp_param_conflict: flow %q requires transport=tcp (got %s)", flow, tr)
	}
	tlsMode := strings.ToLower(strings.TrimSpace(params["tls_mode"]))
	if tlsMode == "none" || tlsMode == "" {
		return fmt.Errorf("cp_param_conflict: flow %q requires tls_mode tls|reality", flow)
	}
	mux := strings.ToLower(strings.TrimSpace(params["multiplex"]))
	if mux != "" && mux != "none" && mux != "false" && mux != "0" {
		return fmt.Errorf("cp_param_conflict: flow %q incompatible with multiplex=%s", flow, mux)
	}
	return nil
}

func applyParamMetaDefaults(p domain.ProtocolPreset, params map[string]string) {
	for k, meta := range p.ParamMeta {
		if strings.TrimSpace(params[k]) != "" {
			continue
		}
		if d := strings.TrimSpace(meta.Default); d != "" {
			// Tentatively set, then drop if not visible under current params.
			params[k] = d
			if !paramvalidate.Visible(p, k, params) {
				delete(params, k)
			}
		}
	}
}

// syncBindingSNI copies SSL profile server_name onto demux_sni when demux_sni is empty.
func (s *Service) syncBindingSNI(set *domain.InboundSet) {
	if set == nil {
		return
	}
	_ = s.ensureSSLProfiles()
	for i := range set.Bindings {
		if set.Bindings[i].Params == nil {
			set.Bindings[i].Params = map[string]string{}
		}
		if strings.TrimSpace(set.Bindings[i].Params["demux_sni"]) != "" {
			continue
		}
		id := strings.TrimSpace(set.Bindings[i].Params[domain.BindingParamSSLProfile])
		if id == "" {
			id = defaultSSLProfileID
		}
		p, ok, err := s.findSSLProfile(id)
		if err != nil || !ok {
			continue
		}
		if sn := p.ServerName(); sn != "" {
			set.Bindings[i].Params["demux_sni"] = strings.ToLower(sn)
		}
	}
}

func (s *Service) handleSetsList(w http.ResponseWriter, r *http.Request) {
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	includeSecrets := parseBoolQuery(r, "secrets", false)
	st, _ := s.store.LoadState()
	last, _ := s.store.LoadSmokeLast()
	out := make([]any, 0, len(sets)+1)
	for _, set := range sets {
		out = append(out, s.setPublicViewOptsSmoke(set, contains(st.ActiveSets, set.Name), includeSecrets, last))
	}
	if hub, err := s.store.LoadWgHub(); err == nil && hub.Enabled {
		out = append(out, s.wgActiveSetView(hub, last))
	}
	if includeSecrets {
		okJSON(w, 200, out)
		return
	}
	okJSONETag(w, r, out)
}

// wgActiveSetView synthesizes an Active-proxies row for the singleton WG hub.
func (s *Service) wgActiveSetView(hub domain.WgHub, last *smoke.Report) map[string]any {
	hub.Normalize()
	binding := map[string]any{"preset": smoke.WgSmokePreset}
	m := map[string]any{
		"name":        smoke.WgSmokeSetName,
		"description": "WireGuard",
		"listen":      "::",
		"listen_port": hub.ListenPort,
		"presets":     []string{smoke.WgSmokePreset},
		"has_demux":   false,
		"active":      true,
		"wg_hub":      true,
		"profile":     hub.Profile,
	}
	if sm := last.SmokeFor(smoke.WgSmokeSetName, smoke.WgSmokePreset); sm != nil {
		binding["smoke"] = sm
		m["smoke"] = sm
	}
	m["bindings"] = []map[string]any{binding}
	return m
}

func (s *Service) handleSetsCreate(w http.ResponseWriter, r *http.Request) {
	var set domain.InboundSet
	if err := decodeBody(r, &set); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	normalizeSetCompat(&set)
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	for _, o := range sets {
		if o.Name == set.Name {
			failJSON(w, 409, "cp_name_conflict", "set name exists")
			return
		}
	}
	if set.Listen == "" {
		set.Listen = "::"
	}
	s.syncBindingSNI(&set)
	if err := s.validateSet(set, sets); err != nil {
		code, ec := validateSetHTTP(err)
		failJSON(w, code, ec, err.Error())
		return
	}
	now := time.Now().UTC()
	set.CreatedAt, set.UpdatedAt = now, now
	sets = append(sets, set)
	if err := s.store.SaveSets(sets); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 201, s.setPublicView(set, false))
}

func (s *Service) handleSetsGet(w http.ResponseWriter, r *http.Request) {
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	st, _ := s.store.LoadState()
	name := r.PathValue("name")
	includeSecrets := parseBoolQuery(r, "secrets", false)
	for _, set := range sets {
		if set.Name == name {
			view := s.setPublicViewOpts(set, contains(st.ActiveSets, name), includeSecrets)
			if includeSecrets {
				okJSON(w, 200, view)
				return
			}
			okJSONETag(w, r, view)
			return
		}
	}
	failJSON(w, 404, "not_found", "set not found")
}

func (s *Service) handleSetSubscriptionTags(w http.ResponseWriter, r *http.Request) {
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	name := r.PathValue("name")
	for _, set := range sets {
		if set.Name != name {
			continue
		}
		okJSON(w, 200, s.buildSetSubscriptionTagsPayload(set))
		return
	}
	failJSON(w, 404, "not_found", "set not found")
}

func (s *Service) handleSetsPut(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var set domain.InboundSet
	if err := decodeBody(r, &set); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	set.Name = name
	normalizeSetCompat(&set)
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	idx := -1
	for i := range sets {
		if sets[i].Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		failJSON(w, 404, "not_found", "set not found")
		return
	}
	if set.Listen == "" {
		set.Listen = "::"
	}
	s.syncBindingSNI(&set)
	if err := s.validateSet(set, sets); err != nil {
		code, ec := validateSetHTTP(err)
		failJSON(w, code, ec, err.Error())
		return
	}
	set.CreatedAt = sets[idx].CreatedAt
	set.UpdatedAt = time.Now().UTC()
	if len(set.PeerSecrets) == 0 {
		set.PeerSecrets = sets[idx].PeerSecrets
	}
	if _, err := s.ensurePeerSecrets(&set); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	sets[idx] = set
	if err := s.store.SaveSets(sets); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	st, _ := s.store.LoadState()
	active := contains(st.ActiveSets, name)
	if active {
		if err := s.rematerialize(r.Context()); err != nil {
			failJSONData(w, 422, materializeErrorCode(err), err.Error(), map[string]any{
				"set_persisted":       true,
				"dataplane_unchanged": false,
				"persisted":           true,
			})
			return
		}
	}
	okJSON(w, 200, s.setPublicView(set, active))
}

func (s *Service) handleSetsDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, err := s.store.LoadState()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if contains(st.ActiveSets, name) {
		failJSON(w, 409, "cp_conflict_active", "deactivate set first")
		return
	}
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	out := make([]domain.InboundSet, 0, len(sets))
	found := false
	for _, set := range sets {
		if set.Name == name {
			found = true
			continue
		}
		out = append(out, set)
	}
	if !found {
		failJSON(w, 404, "not_found", "set not found")
		return
	}
	if err := s.store.SaveSets(out); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{"deleted": true})
}

func (s *Service) handleSetsActivate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var found *domain.InboundSet
	for i := range sets {
		if sets[i].Name == name {
			found = &sets[i]
			break
		}
	}
	if found == nil {
		failJSON(w, 404, "not_found", "set not found")
		return
	}
	if found.HasDemux() && !demuxInBinary {
		failJSON(w, 422, "unsupported_build_tag", "demux set requires binary built with with_demux")
		return
	}
	wasOwner := s.cfg.Owner != nil && s.cfg.Owner.Owner() == configowner.ModeControlplane
	if s.cfg.Owner != nil {
		if err := s.claimOwnership(configowner.ModeControlplane, "activate", name); err != nil {
			failJSON(w, 409, "cp_claim_failed", err.Error())
			return
		}
	}
	s.scrubStaleActiveSets()
	st, err := s.store.LoadState()
	if err != nil {
		s.rollbackFirstActivate(wasOwner, nil, name)
		failJSON(w, 500, "internal", err.Error())
		return
	}
	prev := append([]string{}, st.ActiveSets...)
	if !contains(st.ActiveSets, name) {
		st.ActiveSets = append(st.ActiveSets, name)
		if err := s.store.SaveState(st); err != nil {
			s.rollbackFirstActivate(wasOwner, prev, name)
			failJSON(w, 500, "internal", err.Error())
			return
		}
	}
	if err := s.rematerialize(r.Context()); err != nil {
		if cur, loadErr := s.store.LoadState(); loadErr == nil {
			cur.ActiveSets = prev
			_ = s.store.SaveState(cur)
		}
		s.rollbackFirstActivate(wasOwner, prev, name)
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	mode := string(configowner.ModeControlplane)
	if s.cfg.Owner != nil {
		mode = string(s.cfg.Owner.Owner())
	}
	st, _ = s.store.LoadState()
	okJSON(w, 200, map[string]any{"active_sets": st.ActiveSets, "config_mode": mode})
}

func (s *Service) handleSetsDeactivate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, err := s.store.LoadState()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if !contains(st.ActiveSets, name) {
		sets, loadErr := s.store.LoadSets()
		if loadErr != nil {
			failJSON(w, 500, "internal", loadErr.Error())
			return
		}
		found := false
		for _, set := range sets {
			if set.Name == name {
				found = true
				break
			}
		}
		if !found {
			failJSON(w, 404, "not_found", "set not found")
			return
		}
		// Idempotent deactivate: already inactive is OK.
		mode := "idle"
		if s.cfg.Owner != nil {
			mode = string(s.cfg.Owner.Owner())
		}
		okJSON(w, 200, map[string]any{"active_sets": st.ActiveSets, "config_mode": mode, "noop": true})
		return
	}
	if err := s.deactivateSetByName(r.Context(), name); err != nil {
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	mode := "idle"
	if s.cfg.Owner != nil {
		mode = string(s.cfg.Owner.Owner())
	}
	st, _ = s.store.LoadState()
	okJSON(w, 200, map[string]any{"active_sets": st.ActiveSets, "config_mode": mode})
}

// deactivateSetByName removes name from active_sets and rematerializes (or claims idle).
func (s *Service) deactivateSetByName(ctx context.Context, name string) error {
	return s.deactivateSetByNameOpt(ctx, name, true)
}

func (s *Service) deactivateSetByNameOpt(ctx context.Context, name string, rematerialize bool) error {
	st, err := s.store.LoadState()
	if err != nil {
		return err
	}
	if !contains(st.ActiveSets, name) {
		return nil
	}
	prev := append([]string{}, st.ActiveSets...)
	st.ActiveSets = removeStr(st.ActiveSets, name)
	if err := s.store.SaveState(st); err != nil {
		return err
	}
	if !rematerialize {
		return nil
	}
	if len(st.ActiveSets) == 0 {
		hub, _ := s.store.LoadWgHub()
		if hub.Enabled {
			if err := s.rematerialize(ctx); err != nil {
				if cur, loadErr := s.store.LoadState(); loadErr == nil {
					cur.ActiveSets = prev
					_ = s.store.SaveState(cur)
				}
				return err
			}
		} else if s.cfg.Owner != nil {
			if err := s.claimOwnership(configowner.ModeIdle, "deactivate_last_set", name); err != nil && s.log != nil {
				s.log.Warn("controlplane deactivate claim idle failed", "err", err, "set", name)
			}
		}
		return nil
	}
	if err := s.rematerialize(ctx); err != nil {
		if cur, loadErr := s.store.LoadState(); loadErr == nil {
			cur.ActiveSets = prev
			_ = s.store.SaveState(cur)
		}
		return err
	}
	return nil
}

func (s *Service) realityStatusPayload(cfg domain.RealityConfig) map[string]any {
	payload := map[string]any{
		"profiles":       cfg.Profiles,
		"seed_defaults":  defaultRealityProfiles(),
		"updated_at":     cfg.UpdatedAt,
	}
	assignments, err := s.store.LoadRealityAssignments()
	if err == nil {
		active := make([]any, 0, len(assignments))
		for _, a := range assignments {
			active = append(active, map[string]any{
				"inbound_key":      a.InboundKey,
				"sni":              a.SNI,
				"handshake_server": a.HandshakeServer,
				"handshake_port":   a.HandshakePort,
				"short_id":         a.ShortID,
				"updated_at":       a.UpdatedAt,
			})
		}
		sort.Slice(active, func(i, j int) bool {
			ai := active[i].(map[string]any)["inbound_key"].(string)
			aj := active[j].(map[string]any)["inbound_key"].(string)
			return ai < aj
		})
		payload["active_assignments"] = active
	}
	return payload
}

func (s *Service) handleRealityGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadRealityConfig()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSONETag(w, r, s.realityStatusPayload(cfg))
}

func (s *Service) handleRealityPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Profiles []domain.RealityEndpoint `json:"profiles"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	accepted := make([]domain.RealityEndpoint, 0, len(body.Profiles))
	rejected := make([]map[string]any, 0)
	for _, p := range body.Profiles {
		ep, err := normalizeRealityEndpoint(p)
		if err != nil {
			rejected = append(rejected, map[string]any{
				"sni":    strings.TrimSpace(p.SNI),
				"reason": err.Error(),
			})
			continue
		}
		accepted = append(accepted, ep)
	}
	if len(body.Profiles) > 0 && len(accepted) == 0 {
		failJSON(w, 400, "cp_invalid_reality", "all profiles rejected")
		return
	}
	cfg, err := s.loadRealityConfig()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	cfg.Profiles = accepted
	now := time.Now().UTC()
	cfg.UpdatedAt = &now
	if err := s.store.SaveRealityConfig(cfg); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	sets, err := s.activeSetObjects()
	if err == nil {
		if _, _, err := s.ensureRealityAssignments(sets, cfg.Profiles); err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
	}
	if err := s.rematerialize(r.Context()); err != nil {
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	payload := s.realityStatusPayload(cfg)
	payload["accepted"] = accepted
	payload["rejected"] = rejected
	okJSON(w, 200, payload)
}

func (s *Service) handleSub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		failJSON(w, 405, "method_not_allowed", "GET only")
		return
	}
	token := r.PathValue("token")
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var user *domain.User
	for i := range users {
		if subtleConstantTimeEq(users[i].SubToken, token) {
			user = &users[i]
			break
		}
	}
	if user == nil {
		failJSON(w, 404, "not_found", "unknown token")
		return
	}
	if !user.Eligible(time.Now().UTC()) {
		failJSON(w, 403, "cp_user_ineligible", "user ineligible")
		return
	}
	sets, err := s.activeSetObjects()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	hub, err := s.store.LoadWgHub()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if len(sets) == 0 && !hub.Enabled {
		failJSON(w, 409, "cp_no_active_set", "no active sets")
		return
	}
	if changed, err := s.ensureCreds(user); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	} else if changed {
		// Persist lazy backfill so sub and materialize stay consistent.
		for i := range users {
			if users[i].ID == user.ID {
				users[i] = *user
				break
			}
		}
		_ = s.store.SaveUsers(users)
	}
	if hub.Enabled {
		ensured, c2, err := s.ensureWgUserCreds([]domain.User{*user})
		if err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
		if c2 && len(ensured) == 1 {
			*user = ensured[0]
			for i := range users {
				if users[i].ID == user.ID {
					users[i] = *user
					break
				}
			}
			_ = s.store.SaveUsers(users)
		}
	}
	filterSet := r.URL.Query().Get("set")
	filterPresets := parsePresetQuery(r)
	filterVariants := parseRepeatCommaQuery(r, "variant")
	filterTags := parseRepeatCommaQuery(r, "tag")
	filterProfiles := parseRepeatCommaQuery(r, "profile")
	filterFlow := parseRepeatCommaQuery(r, "flow")
	filterNetwork := strings.TrimSpace(r.URL.Query().Get("network"))
	strictFilters := parseBoolQuery(r, "strict_filters", false)
	host := s.publicHost(r)
	_ = s.ensureSSLProfiles()
	sslList, err := s.loadSSLProfiles()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	defHost := host
	defCert, _ := sslCertPaths(s.cfg.DataDir, defaultSSLProfileID)
	for _, sp := range sslList {
		if sp.ID == defaultSSLProfileID {
			if sn := sp.ServerName(); sn != "" {
				defHost = sn
			}
			defCert, _ = sslCertPaths(s.cfg.DataDir, sp.ID)
			break
		}
	}
	profile := domain.DefaultSelfSigned(defHost)
	assignments, err := s.store.LoadRealityAssignments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var hubPtr *domain.WgHub
	if hub.Enabled {
		hubPtr = &hub
	}
	var echByID map[string]materialize.ECHMaterial
	sslByID := map[string]domain.SSLProfile{}
	for _, sp := range s.sslProfilesWithResolvedACMEEmail(sslList) {
		sslByID[sp.ID] = sp
		if !sp.ECHEnabled {
			continue
		}
		keyPath, cfgPath := sslECHPaths(s.cfg.DataDir, sp.ID)
		raw, err := os.ReadFile(cfgPath)
		if err != nil {
			continue
		}
		if echByID == nil {
			echByID = map[string]materialize.ECHMaterial{}
		}
		echByID[sp.ID] = materialize.ECHMaterial{KeyPath: keyPath, ConfigPEM: string(raw)}
	}
	body, err := materialize.RenderSubscription(*user, sets, host, profile, materialize.SubscriptionFilters{
		Set:           filterSet,
		Presets:       filterPresets,
		Variants:      filterVariants,
		Tags:          filterTags,
		Profiles:      filterProfiles,
		Flow:          filterFlow,
		Network:       filterNetwork,
		StrictFilters: strictFilters,
		TLSCertPath:   defCert,
		ECHByID:       echByID,
		SSLProfiles:   sslByID,
		DataDir:       s.cfg.DataDir,
	}, assignments, hubPtr)
	if err != nil {
		if strictFilters && strings.Contains(err.Error(), "cp_invalid_sub_filter") {
			failJSON(w, 400, "cp_invalid_sub_filter", err.Error())
			return
		}
		failJSON(w, 500, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

// parsePresetQuery collects repeatable and comma-separated ?preset= filters.
func parsePresetQuery(r *http.Request) []string {
	return parseRepeatCommaQuery(r, "preset")
}

func uniqueSetPresets(bindings []domain.SetBinding) []string {
	out := make([]string, 0, len(bindings))
	seen := map[string]struct{}{}
	for _, b := range bindings {
		pn := strings.TrimSpace(b.Preset)
		if pn == "" {
			continue
		}
		if _, ok := seen[pn]; ok {
			continue
		}
		seen[pn] = struct{}{}
		out = append(out, pn)
	}
	return out
}

func normalizeSetCompat(set *domain.InboundSet) {
	if set == nil {
		return
	}
	if len(set.Bindings) == 0 && len(set.Presets) > 0 {
		set.Bindings = make([]domain.SetBinding, 0, len(set.Presets))
		for _, pn := range set.Presets {
			pn = strings.TrimSpace(pn)
			if pn == "" {
				continue
			}
			set.Bindings = append(set.Bindings, domain.SetBinding{Preset: pn})
		}
	}
	if len(set.Presets) == 0 && len(set.Bindings) > 0 {
		set.Presets = uniqueSetPresets(set.Bindings)
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func parseRepeatCommaQuery(r *http.Request, key string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, v := range r.URL.Query()[key] {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

func subtleConstantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

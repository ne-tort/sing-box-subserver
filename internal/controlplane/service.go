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
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/demuxrecipes"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/materialize"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/ssh"
)

// Service is the embedded controlplane.
type Service struct {
	cfg   Deps
	store *store.Store
	log   *slog.Logger
	mu    sync.Mutex
	realityLastValidation time.Time

	mgmtTLS   mgmtCertCache
	acmeWatch struct {
		mu        sync.Mutex
		enteredAt time.Time
		everReady bool
		lostSince time.Time
	}

	trafficHooks TrafficHooks
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

// Bootstrap ensures default TLS profile and rematerializes if we own the dataplane.
func (s *Service) Bootstrap(ctx context.Context) {
	if s == nil {
		return
	}
	p, err := s.ensureTLSProfile(false)
	if err != nil && s.log != nil {
		s.log.Warn("controlplane tls profile bootstrap failed", "err", err)
	} else if err == nil {
		if err := s.ensureSafetySelfSignedPEMs(p); err != nil && s.log != nil {
			s.log.Warn("controlplane safety self_signed pems failed", "err", err)
		}
		if p.Mode == domain.TLSModeACMEDomain || p.Mode == domain.TLSModeACMEIP {
			s.noteACMEModeEntered()
			if p.ACME != nil {
				ready, _, _ := acmeCertificateReady(s.cfg.DataDir, p.ACME.Domains)
				s.noteACMEReady(ready)
			}
		}
	}
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
		case <-t.C:
			s.mu.Lock()
			beforeElig := s.eligibilityMapLocked()
			_ = s.applyTrafficResetsLocked(time.Now().UTC())
			s.notifyBecameIneligibleLocked(beforeElig)
			fp := s.eligibilityFingerprintLocked()
			needRealityRefresh := time.Since(s.realityLastValidation) >= realityValidationInterval
			if needRealityRefresh {
				s.realityLastValidation = time.Now().UTC()
			}
			s.mu.Unlock()
			if s.cfg.Owner == nil || s.cfg.Owner.Owner() != configowner.ModeControlplane {
				lastFP = fp
				continue
			}
			if needRealityRefresh {
				sets, err := s.activeSetObjects()
				if err == nil && hasRealityPreset(sets) {
					cfg, changed, err := s.refreshRealityConfig(ctx, true)
					if err == nil {
						if _, aChanged, err := s.ensureRealityAssignments(sets, cfg.EffectiveProfiles); err == nil && (changed || aChanged) {
							_ = s.rematerialize(ctx)
						}
					}
				}
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
		out = append(out, u.ID)
	}
	return out
}

// ApplyTrafficUsage writes cumulative totals from the traffic module into users.json.
// Returns whether eligibility changed (and rematerialize was triggered).
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
		if users[i].TrafficUsedBytes != v {
			users[i].TrafficUsedBytes = v
			users[i].UpdatedAt = now
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
	mux.HandleFunc("GET /v1/controlplane/status", requireAuth(s.handleStatus))
	mux.HandleFunc("GET /v1/controlplane/status/details", requireAuth(s.handleStatusDetails))
	mux.HandleFunc("GET /v1/controlplane/users", requireAuth(s.handleUsersList))
	mux.HandleFunc("POST /v1/controlplane/users", requireAuth(s.handleUsersCreate))
	mux.HandleFunc("GET /v1/controlplane/users/{id}", requireAuth(s.handleUsersGet))
	mux.HandleFunc("PATCH /v1/controlplane/users/{id}", requireAuth(s.handleUsersPatch))
	mux.HandleFunc("DELETE /v1/controlplane/users/{id}", requireAuth(s.handleUsersDelete))
	mux.HandleFunc("POST /v1/controlplane/users/{id}/rotate-token", requireAuth(s.handleUsersRotateToken))
	mux.HandleFunc("POST /v1/controlplane/users/{id}/rotate-creds", requireAuth(s.handleUsersRotateCreds))
	mux.HandleFunc("PUT /v1/controlplane/users/{id}/creds", requireAuth(s.handleUsersPutCreds))
	mux.HandleFunc("GET /v1/controlplane/presets", requireAuth(s.handlePresetsList))
	mux.HandleFunc("GET /v1/controlplane/presets/{name}", requireAuth(s.handlePresetsGet))
	mux.HandleFunc("GET /v1/controlplane/protocols", requireAuth(s.handleProtocolsList))
	mux.HandleFunc("GET /v1/controlplane/protocols/{tag}", requireAuth(s.handleProtocolsGet))
	mux.HandleFunc("GET /v1/controlplane/demux-recipes", requireAuth(s.handleDemuxRecipesList))
	mux.HandleFunc("GET /v1/controlplane/demux-recipes/{name}", requireAuth(s.handleDemuxRecipesGet))
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
	mux.HandleFunc("GET /v1/controlplane/tls", requireAuth(s.handleTLSGet))
	mux.HandleFunc("PUT /v1/controlplane/tls", requireAuth(s.handleTLSPut))
	mux.HandleFunc("POST /v1/controlplane/tls/regenerate", requireAuth(s.handleTLSRegenerate))
	mux.HandleFunc("GET /v1/controlplane/reality", requireAuth(s.handleRealityGet))
	mux.HandleFunc("PUT /v1/controlplane/reality", requireAuth(s.handleRealityPut))
	mux.HandleFunc("GET /v1/controlplane/wg", requireAuth(s.handleWgGet))
	mux.HandleFunc("PUT /v1/controlplane/wg", requireAuth(s.handleWgPut))
	mux.HandleFunc("POST /v1/controlplane/wg/regenerate-awg", requireAuth(s.handleWgRegenerateAWG))
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

func (s *Service) subscriptionURL(r *http.Request, token string) (path, url string) {
	path = "/v1/sub/" + token
	host := s.publicHost(r)
	port := ""
	if s.cfg.Cfg != nil {
		if s.cfg.Cfg.Controlplane.PublicPort > 0 {
			port = strconv.Itoa(s.cfg.Cfg.Controlplane.PublicPort)
		} else if _, p, err := net.SplitHostPort(s.cfg.Cfg.Listen); err == nil {
			port = p
		}
	}
	// CP builds always serve management/sub over HTTPS (CP TLS profile).
	scheme := "https"
	if s.cfg.Cfg != nil && s.cfg.Cfg.HasTLS() {
		scheme = "https"
	}
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
	byName := map[string]domain.InboundSet{}
	for _, set := range sets {
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
		s.publishTrafficPolicyLocked()
		return nil
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
	profile, err := s.ensureTLSProfile(false)
	if err != nil {
		return err
	}
	realityAssignments := map[string]domain.RealityAssignment{}
	if hasRealityPreset(sets) {
		realityCfg, _, err := s.refreshRealityConfig(ctx, false)
		if err != nil {
			return err
		}
		realityAssignments, _, err = s.ensureRealityAssignments(sets, realityCfg.EffectiveProfiles)
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
		RealityAssignments: realityAssignments,
	}
	if hub.Enabled {
		h := hub
		in.WgHub = &h
	}
	pemChanged := false
	if profile.Mode == domain.TLSModeSelfSigned {
		if profile.SelfSigned == nil {
			return fmt.Errorf("self_signed spec missing")
		}
		cert, key, changed, err := ensureSelfSigned(s.cfg.DataDir, *profile.SelfSigned, false)
		if err != nil {
			return fmt.Errorf("tls material: %w", err)
		}
		pemChanged = changed
		in.TLSCertPath, in.TLSKeyPath = cert, key
	} else if profile.Mode == domain.TLSModeACMEDomain || profile.Mode == domain.TLSModeACMEIP {
		// TrustTunnel (and similar PEM-only inbounds) need concrete files; resolve ACME PEMs.
		if cert, key, _, err := s.mgmtMaterialPaths(); err == nil {
			in.TLSCertPath, in.TLSKeyPath = cert, key
		}
	}
	slotTLS, slotChanged, err := s.ensureDemuxSlotTLS(sets)
	if err != nil {
		return err
	}
	if slotChanged {
		pemChanged = true
	}
	in.SlotTLS = slotTLS
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

func (s *Service) ensureDemuxSlotTLS(sets []domain.InboundSet) (map[string]materialize.SlotTLSMaterial, bool, error) {
	out := map[string]materialize.SlotTLSMaterial{}
	changed := false
	for _, set := range sets {
		if !set.HasDemux() {
			continue
		}
		for _, b := range set.EffectiveBindings() {
			sni := ""
			if b.Params != nil {
				sni = strings.TrimSpace(b.Params["demux_sni"])
			}
			if sni == "" {
				continue
			}
			p, err := presets.Get(b.Preset)
			if err != nil {
				continue
			}
			// Reality uses handshake assignment, not PEM slots.
			if presetHasTrait(p, "reality") {
				continue
			}
			needsPEM := presetHasTrait(p, "tls") || presetHasTrait(p, "tls_custom")
			if !needsPEM {
				continue
			}
			cert, key, wrote, err := ensureSlotSelfSigned(s.cfg.DataDir, sni)
			if err != nil {
				return nil, false, fmt.Errorf("slot tls %q: %w", sni, err)
			}
			if wrote {
				changed = true
			}
			out[sni] = materialize.SlotTLSMaterial{CertPath: cert, KeyPath: key}
		}
	}
	return out, changed, nil
}

func (s *Service) ensureTLSProfile(forceDefault bool) (domain.TLSProfile, error) {
	host := ""
	if s.cfg.Cfg != nil {
		host = s.cfg.Cfg.Controlplane.PublicHost
	}
	p, ok, err := s.store.LoadTLSProfile()
	if err != nil {
		return domain.TLSProfile{}, err
	}
	if !ok || forceDefault {
		p = domain.DefaultSelfSigned(host)
		if err := p.Validate(); err != nil {
			return domain.TLSProfile{}, err
		}
		if err := s.store.SaveTLSProfile(p); err != nil {
			return domain.TLSProfile{}, err
		}
		if p.Mode == domain.TLSModeSelfSigned && p.SelfSigned != nil {
			_, _, _, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, false)
			if err != nil {
				return domain.TLSProfile{}, err
			}
		}
		return p, nil
	}
	if err := p.Validate(); err != nil {
		return domain.TLSProfile{}, err
	}
	if p.Mode == domain.TLSModeSelfSigned && p.SelfSigned != nil {
		if _, _, _, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, false); err != nil {
			return domain.TLSProfile{}, err
		}
	} else if err := s.ensureSafetySelfSignedPEMs(p); err != nil {
		return domain.TLSProfile{}, err
	}
	return p, nil
}

func (s *Service) acmeWatchdog(ctx context.Context) {
	p, err := s.ensureTLSProfile(false)
	if err != nil {
		return
	}
	if p.Mode != domain.TLSModeACMEDomain && p.Mode != domain.TLSModeACMEIP {
		return
	}
	domains := []string{}
	if p.ACME != nil {
		domains = p.ACME.Domains
	}
	ready, _, _ := acmeCertificateReady(s.cfg.DataDir, domains)
	s.noteACMEReady(ready)
	if ready {
		return
	}
	ok, reason := s.shouldACMEFallback()
	if !ok {
		return
	}
	s.forceSelfSignedFallback(ctx, reason)
}

func (s *Service) forceSelfSignedFallback(ctx context.Context, reason string) {
	host := ""
	if s.cfg.Cfg != nil {
		host = s.cfg.Cfg.Controlplane.PublicHost
	}
	p := domain.DefaultSelfSigned(host)
	if err := p.Validate(); err != nil {
		if s.log != nil {
			s.log.Error("acme fallback: invalid self_signed profile", "err", err, "reason", reason)
		}
		return
	}
	if err := s.store.SaveTLSProfile(p); err != nil {
		if s.log != nil {
			s.log.Error("acme fallback: save profile failed", "err", err, "reason", reason)
		}
		return
	}
	if _, _, _, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, true); err != nil {
		if s.log != nil {
			s.log.Error("acme fallback: write pem failed", "err", err, "reason", reason)
		}
		return
	}
	// Reset ACME watch state after leaving ACME mode.
	s.acmeWatch.mu.Lock()
	s.acmeWatch.enteredAt = time.Time{}
	s.acmeWatch.everReady = false
	s.acmeWatch.lostSince = time.Time{}
	s.acmeWatch.mu.Unlock()
	s.mgmtTLS.mu.Lock()
	s.mgmtTLS.cert = nil
	s.mgmtTLS.source = ""
	s.mgmtTLS.mu.Unlock()

	if s.log != nil {
		s.log.Error("ACME emergency fallback to self_signed", "reason", reason)
	}
	if err := s.rematerializeForce(ctx, true); err != nil && s.log != nil {
		s.log.Error("acme fallback rematerialize failed", "err", err)
	}
}

func okJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

func failJSON(w http.ResponseWriter, code int, errCode, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": false,
		"error": map[string]any{"code": errCode, "message": msg},
	})
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
		"traffic_reset_at":         u.TrafficResetAt,
		"traffic_reset_period_sec": u.TrafficResetPeriodSec,
		"speed_up_bytes_per_sec":   u.SpeedUpBytesPerSec,
		"speed_down_bytes_per_sec": u.SpeedDownBytesPerSec,
		"has_token":                u.SubToken != "",
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
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}

func (s *Service) handleStatus(w http.ResponseWriter, _ *http.Request) {
	okJSON(w, http.StatusOK, s.buildStatusPayload(false))
}

func (s *Service) handleStatusDetails(w http.ResponseWriter, _ *http.Request) {
	okJSON(w, http.StatusOK, s.buildStatusPayload(true))
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
		"demux_recipes_count":     len(demuxrecipes.All()),
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
	if p, err := s.ensureTLSProfile(false); err == nil {
		tls := s.tlsStatusPayload(p)
		if ms, ok := tls["material_status"]; ok {
			out["tls_material_status"] = ms
		}
		out["tls_mode"] = p.Mode
	}
	if rc, err := s.loadRealityConfig(); err == nil {
		out["reality"] = s.realityStatusPayload(rc)
	}
	out["ownership_health"] = s.ownershipHealth(st)
	out["ready"] = s.computeClientReady(st, out)
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
	if len(st.ActiveSets) == 0 {
		ok = false
		reasons = append(reasons, "no_active_sets")
	}
	if st.Materialize != nil && st.Materialize.LastError != "" {
		ok = false
		reasons = append(reasons, "materialize_error")
	}
	if ms, okTLS := payload["tls_material_status"].(map[string]any); okTLS {
		if ready, _ := ms["ready"].(bool); !ready {
			// ACME may still be issuing; not fatal for self_signed but block "ready" if explicitly false.
			if mode, _ := payload["tls_mode"].(string); mode == string(domain.TLSModeACMEDomain) || mode == string(domain.TLSModeACMEIP) {
				ok = false
				reasons = append(reasons, "tls_not_ready")
			}
		}
	}
	boxUp := false
	supState := ""
	if s.cfg.Supervisor != nil {
		snap := s.cfg.Supervisor.Status()
		boxUp = snap.BoxUp
		supState = string(snap.State)
		if !snap.BoxUp || snap.State != supervisor.StateRunning {
			ok = false
			reasons = append(reasons, "dataplane_not_running")
		}
	}
	return map[string]any{
		"ok":               ok,
		"box_up":           boxUp,
		"supervisor_state": supState,
		"active_sets":      len(st.ActiveSets) > 0,
		"reasons":          reasons,
		"poll":             "GET /v1/controlplane/status → ready.ok == true",
	}
}

func (s *Service) buildActiveSetDetails(active []string, sets []domain.InboundSet) []any {
	if len(active) == 0 {
		return nil
	}
	byName := map[string]domain.InboundSet{}
	for _, set := range sets {
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

func (s *Service) handleUsersList(w http.ResponseWriter, _ *http.Request) {
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	out := make([]any, 0, len(users))
	for _, u := range users {
		out = append(out, redactUser(u, false))
	}
	okJSON(w, 200, out)
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
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		failJSON(w, 400, "bad_request", "name required")
		return
	}
	if err := validateCreds(body.Creds); err != nil {
		failJSON(w, 400, "cp_invalid_creds", strings.TrimPrefix(err.Error(), "cp_invalid_creds: "))
		return
	}
	users, err := s.store.LoadUsers()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	for _, u := range users {
		if u.Name == body.Name {
			failJSON(w, 409, "conflict", "name already exists")
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
	if err := s.rematerialize(r.Context()); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	path, url := s.subscriptionURL(r, u.SubToken)
	data := redactUser(u, true)
	data["subscription_path"] = path
	data["subscription_url"] = url
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
	if v, ok := body["name"].(string); ok && v != "" {
		if v != u.Name {
			for j := range users {
				if j != i && users[j].Name == v {
					failJSON(w, 409, "conflict", "name already exists")
					return
				}
			}
		}
		u.Name = v
	}
	if v, ok := body["enabled"].(bool); ok {
		u.Enabled = v
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
		u.TrafficUsedBytes = uint64(v)
		usedPatched = true
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
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if usedPatched && s.trafficHooks != nil {
		s.trafficHooks.OnTrafficUsedPatched(u.ID, u.TrafficUsedBytes)
	}
	if wasEligible && !u.Eligible(time.Now().UTC()) && s.trafficHooks != nil {
		s.trafficHooks.OnBecameIneligible([]string{u.ID})
	}
	if err := s.rematerialize(r.Context()); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, redactUser(*u, false))
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
	users = append(users[:i], users[i+1:]...)
	if err := s.store.SaveUsers(users); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if s.trafficHooks != nil {
		s.trafficHooks.OnBecameIneligible([]string{deletedID})
	}
	if err := s.rematerialize(r.Context()); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{"deleted": true})
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
	users[i].Creds = map[string]map[string]any{}
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
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, redactUser(users[i], true))
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
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, redactUser(users[i], true))
}

func requestLang(r *http.Request) string {
	return domain.NormalizeLang(r.URL.Query().Get("lang"))
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
		item := map[string]any{
			"name":         pp.Name,
			"tag":          pp.Name,
			"protocol":     pp.Protocol,
			"description":  pp.Description,
			"short_name":   pp.ShortName,
			"traits":       pp.Traits,
			"status":       pp.Status,
			"aliases":      pp.Aliases,
			"scores":       pp.Scores,
			"demux_hints":  pp.DemuxHints,
			"cred_fields":  pp.CredFields,
			"param_fields": pp.ParamFields,
			"optional_params": map[string]any{
				"listen_port": map[string]any{
					"type":        "uint16",
					"required":    false,
					"description": "Public listen port for single-inbound install. Omit to auto-pick a free port (prefers 443).",
					"constraint":  "At most one TCP and one UDP occupant per port.",
				},
			},
		}
		if nets := networksFromTraits(pp.Traits); len(nets) > 0 {
			item["networks"] = nets
		}
		out = append(out, item)
	}
	okJSON(w, 200, out)
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
	proto, _ := presets.GetProtocol(inv.Protocol)
	_, pDesc := domain.ResolveI18n(proto.I18n, lang)
	okJSON(w, 200, map[string]any{
		"name":              pp.Name,
		"tag":               pp.Name,
		"protocol":          pp.Protocol,
		"description":       pp.Description,
		"short_name":        pp.ShortName,
		"traits":            pp.Traits,
		"status":            pp.Status,
		"aliases":           pp.Aliases,
		"scores":            pp.Scores,
		"demux_hints":       pp.DemuxHints,
		"requirements":      pp.Requirements,
		"cred_fields":       pp.CredFields,
		"param_fields":      pp.ParamFields,
		"client_notes":      inv.ClientNotes,
		"optional_params": map[string]any{
			"listen_port": map[string]any{
				"type":        "uint16",
				"required":    false,
				"description": "Public listen port when installing as a single-inbound set (not demux member). Omit to auto-pick.",
				"constraint":  "At most one TCP and one UDP inbound may share a port across sets.",
			},
			"demux_sni": map[string]any{
				"type":        "string",
				"description": "SNI used for demux match / TLS server_name when installed inside a demux group.",
			},
		},
		"inbound_template":  pp.InboundTemplate,
		"outbound_template": pp.OutboundTemplate,
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
		out = append(out, map[string]any{
			"tag":             p.Tag,
			"short_name":      p.ShortName,
			"status":          p.Status,
			"title":           title,
			"description":     desc,
			"invariant_tags":  p.InvariantTags,
			"singbox_type":    p.SingBoxType,
		})
	}
	okJSON(w, 200, out)
}

func (s *Service) handleProtocolsGet(w http.ResponseWriter, r *http.Request) {
	lang := requestLang(r)
	p, err := presets.GetProtocol(r.PathValue("tag"))
	if err != nil {
		failJSON(w, 404, "not_found", err.Error())
		return
	}
	title, desc := domain.ResolveI18n(p.I18n, lang)
	invOut := make([]any, 0, len(p.InvariantTags))
	for _, tag := range p.InvariantTags {
		inv, err := presets.GetInvariant(tag)
		if err != nil {
			continue
		}
		pp := inv.ToProtocolPreset(lang)
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
	okJSON(w, 200, map[string]any{
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

func (s *Service) handleDemuxRecipesList(w http.ResponseWriter, _ *http.Request) {
	all := demuxrecipes.All()
	out := make([]any, 0, len(all))
	for _, r := range all {
		out = append(out, map[string]any{
			"name":             r.Name,
			"description":      r.Description,
			"required_presets": r.RequiredPresets,
			"suggested_port":   r.SuggestedPort,
		})
	}
	okJSON(w, 200, out)
}

func (s *Service) handleDemuxRecipesGet(w http.ResponseWriter, r *http.Request) {
	rec, err := demuxrecipes.Get(r.PathValue("name"))
	if err != nil {
		failJSON(w, 404, "not_found", err.Error())
		return
	}
	okJSON(w, 200, rec)
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
		if err := validateBindingParams(p, b); err != nil {
			return err
		}
	}
	if set.HasDemux() {
		if err := demuxrecipes.ValidateTemplate(set.DemuxTemplate); err != nil {
			return fmt.Errorf("cp_invalid_demux: %w", err)
		}
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

func validateBindingParams(p domain.ProtocolPreset, b domain.SetBinding) error {
	if len(p.ParamFields) == 0 {
		return nil
	}
	for _, field := range p.ParamFields {
		if strings.TrimSpace(b.Params[field]) == "" {
			return fmt.Errorf("cp_invalid_bindings: preset %q requires bindings[].params[%q] (e.g. carrier room URL)", p.Name, field)
		}
	}
	return nil
}

func (s *Service) handleSetsList(w http.ResponseWriter, _ *http.Request) {
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	st, _ := s.store.LoadState()
	out := make([]any, 0, len(sets))
	for _, set := range sets {
		bindings := set.EffectiveBindings()
		m := map[string]any{
			"name": set.Name, "description": set.Description, "listen": set.Listen,
			"listen_port": set.ListenPort, "presets": uniqueSetPresets(bindings), "bindings": bindings,
			"has_demux": set.HasDemux(), "active": contains(st.ActiveSets, set.Name),
		}
		out = append(out, m)
	}
	okJSON(w, 200, out)
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
			failJSON(w, 409, "conflict", "set name exists")
			return
		}
	}
	if set.Listen == "" {
		set.Listen = "::"
	}
	if err := s.validateSet(set, sets); err != nil {
		code := 400
		ec := "bad_request"
		if strings.Contains(err.Error(), "cp_port_conflict") {
			code, ec = 409, "cp_port_conflict"
		}
		if strings.Contains(err.Error(), "cp_unknown_preset") {
			ec = "cp_unknown_preset"
		}
		if strings.Contains(err.Error(), "cp_invalid_demux") {
			ec = "cp_invalid_demux"
		}
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
	okJSON(w, 200, set)
}

func (s *Service) handleSetsGet(w http.ResponseWriter, r *http.Request) {
	sets, err := s.store.LoadSets()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	name := r.PathValue("name")
	for _, set := range sets {
		if set.Name == name {
			okJSON(w, 200, set)
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
	if err := s.validateSet(set, sets); err != nil {
		code, ec := 400, "bad_request"
		if strings.Contains(err.Error(), "cp_port_conflict") {
			code, ec = 409, "cp_port_conflict"
		}
		if strings.Contains(err.Error(), "cp_unknown_preset") {
			ec = "cp_unknown_preset"
		}
		if strings.Contains(err.Error(), "cp_invalid_demux") {
			ec = "cp_invalid_demux"
		}
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
	if contains(st.ActiveSets, name) {
		_ = s.rematerialize(r.Context())
	}
	okJSON(w, 200, set)
}

func (s *Service) handleSetsDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, err := s.store.LoadState()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if contains(st.ActiveSets, name) {
		failJSON(w, 409, "conflict", "deactivate set first")
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
		// Idempotent deactivate: already inactive is OK.
		mode := "idle"
		if s.cfg.Owner != nil {
			mode = string(s.cfg.Owner.Owner())
		}
		okJSON(w, 200, map[string]any{"active_sets": st.ActiveSets, "config_mode": mode, "noop": true})
		return
	}
	st.ActiveSets = removeStr(st.ActiveSets, name)
	if err := s.store.SaveState(st); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if len(st.ActiveSets) == 0 {
		hub, _ := s.store.LoadWgHub()
		if hub.Enabled {
			_ = s.rematerialize(r.Context())
		} else if s.cfg.Owner != nil {
			if err := s.claimOwnership(configowner.ModeIdle, "deactivate_last_set", name); err != nil && s.log != nil {
				s.log.Warn("controlplane deactivate claim idle failed", "err", err, "set", name)
			}
		}
	} else {
		_ = s.rematerialize(r.Context())
	}
	mode := "idle"
	if s.cfg.Owner != nil {
		mode = string(s.cfg.Owner.Owner())
	}
	okJSON(w, 200, map[string]any{"active_sets": st.ActiveSets, "config_mode": mode})
}

func (s *Service) handleTLSGet(w http.ResponseWriter, _ *http.Request) {
	p, err := s.ensureTLSProfile(false)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, s.tlsStatusPayload(p))
}

func (s *Service) handleTLSPut(w http.ResponseWriter, r *http.Request) {
	var p domain.TLSProfile
	if err := decodeBody(r, &p); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if err := p.Validate(); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if err := s.store.SaveTLSProfile(p); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	forceReload := false
	if p.Mode == domain.TLSModeSelfSigned && p.SelfSigned != nil {
		if _, _, changed, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, false); err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		} else {
			forceReload = changed
		}
	} else {
		// Mode switch away from self_signed still needs Apply when CP owns the box.
		forceReload = true
		if err := s.ensureSafetySelfSignedPEMs(p); err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
		if p.Mode == domain.TLSModeACMEDomain || p.Mode == domain.TLSModeACMEIP {
			s.noteACMEModeEntered()
		}
	}
	s.mgmtTLS.mu.Lock()
	s.mgmtTLS.cert = nil
	s.mgmtTLS.mu.Unlock()
	if err := s.rematerializeForce(r.Context(), forceReload); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, s.tlsStatusPayload(p))
}

func (s *Service) handleTLSRegenerate(w http.ResponseWriter, r *http.Request) {
	p, err := s.ensureTLSProfile(false)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if p.Mode != domain.TLSModeSelfSigned || p.SelfSigned == nil {
		failJSON(w, 400, "bad_request", "regenerate only for self_signed mode")
		return
	}
	if _, _, _, err := ensureSelfSigned(s.cfg.DataDir, *p.SelfSigned, true); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if err := s.rematerializeForce(r.Context(), true); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, s.tlsStatusPayload(p))
}

func (s *Service) tlsStatusPayload(p domain.TLSProfile) map[string]any {
	out := map[string]any{
		"mode":        p.Mode,
		"self_signed": p.SelfSigned,
		"acme":        redactACME(p.ACME),
	}
	cert, key := tlsMaterialPaths(s.cfg.DataDir)
	_, certErr := os.Stat(cert)
	_, keyErr := os.Stat(key)
	pemPresent := certErr == nil && keyErr == nil
	status := map[string]any{
		"mode":                p.Mode,
		"acme_data_directory": acmeDataDirectory(s.cfg.DataDir),
	}
	switch p.Mode {
	case domain.TLSModeSelfSigned:
		status["self_signed_cert_present"] = pemPresent
		status["cert_path"] = cert
		status["key_path"] = key
		status["active_material"] = "self_signed_pem"
		status["ready"] = pemPresent
		if !pemPresent {
			status["ready_reason"] = "self_signed pem missing"
		}
	case domain.TLSModeACMEDomain, domain.TLSModeACMEIP:
		status["self_signed_cert_present"] = pemPresent // orphan files may remain
		status["active_material"] = "certificate_provider"
		status["certificate_provider_tag"] = domain.TLSProviderTag
		domains := []string{}
		if p.ACME != nil {
			domains = p.ACME.Domains
		}
		ready, missing, found := acmeCertificateReady(s.cfg.DataDir, domains)
		s.noteACMEReady(ready)
		status["ready"] = ready
		status["acme_certs_found"] = found
		status["acme_certs_missing"] = missing
		if !ready {
			status["ready_reason"] = "waiting for ACME obtain (certmagic)"
		}
	default:
		status["active_material"] = "unknown"
		status["ready"] = false
		status["ready_reason"] = "unknown mode"
	}
	status["mgmt_https"] = true
	status["mgmt_cert_source"] = s.mgmtCertSource()
	if _, _, src, err := s.mgmtMaterialPaths(); err == nil {
		status["mgmt_cert_source"] = src
	}
	out["material_status"] = status
	return out
}

func redactACME(a *domain.ACMESpec) any {
	if a == nil {
		return nil
	}
	cp := *a
	if len(cp.DNS01Challenge) > 0 {
		red := map[string]any{}
		for k, v := range cp.DNS01Challenge {
			ks := strings.ToLower(k)
			if strings.Contains(ks, "token") || strings.Contains(ks, "secret") || strings.Contains(ks, "password") || strings.Contains(ks, "key") {
				red[k] = "[redacted]"
			} else {
				red[k] = v
			}
		}
		cp.DNS01Challenge = red
	}
	return cp
}

func (s *Service) realityStatusPayload(cfg domain.RealityConfig) map[string]any {
	payload := map[string]any{
		"user_overrides":       cfg.UserProfiles,
		"effective_profiles":   cfg.EffectiveProfiles,
		"using_user_overrides": cfg.UsingUserOverrides,
		"updated_at":           cfg.UpdatedAt,
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

func (s *Service) handleRealityGet(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.loadRealityConfig()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, s.realityStatusPayload(cfg))
}

func (s *Service) handleRealityPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Profiles []domain.RealityEndpoint `json:"profiles"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	normalized := make([]domain.RealityEndpoint, 0, len(body.Profiles))
	for _, p := range body.Profiles {
		ep, err := normalizeRealityEndpoint(p)
		if err != nil {
			continue
		}
		normalized = append(normalized, ep)
	}
	cfg, err := s.loadRealityConfig()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	cfg.UserProfiles = normalized
	cfg.UsingUserOverrides = len(normalized) > 0
	now := time.Now().UTC()
	cfg.UpdatedAt = &now
	if err := s.store.SaveRealityConfig(cfg); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	refreshed, _, err := s.refreshRealityConfig(ctx, true)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	sets, err := s.activeSetObjects()
	if err == nil {
		if _, _, err := s.ensureRealityAssignments(sets, refreshed.EffectiveProfiles); err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
	}
	if err := s.rematerialize(r.Context()); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	okJSON(w, 200, s.realityStatusPayload(refreshed))
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
	profile, err := s.ensureTLSProfile(false)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	assignments, err := s.store.LoadRealityAssignments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	var hubPtr *domain.WgHub
	if hub.Enabled {
		hubPtr = &hub
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
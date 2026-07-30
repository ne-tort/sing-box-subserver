//go:build with_controlplane

package controlplane

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

const realityValidationInterval = 5 * time.Minute

var likelyCDNSuffixes = []string{
	".cloudflare.com",
	".cloudfront.net",
	".fastly.net",
	".fastlylb.net",
	".akamaiedge.net",
	".akamai.net",
	".edgekey.net",
	".edgesuite.net",
	".jsdelivr.net",
	".unpkg.com",
}

func defaultRealityProfiles() []domain.RealityEndpoint {
	// Keep in sync with demuxgroups realitySNIPool.
	// Curated via scripts/_curate_reality_sni.py: known hosts, correct www/bare,
	// no Google/Yahoo/Cloudflare/CDN edges, no RU sites; TLS+redirect checked.
	return []domain.RealityEndpoint{
		{SNI: "www.microsoft.com"},
		{SNI: "www.apple.com"},
		{SNI: "www.amazon.com"},
		{SNI: "gateway.icloud.com"},
		{SNI: "www.bing.com"},
		{SNI: "www.wikipedia.org"},
		{SNI: "github.com"},
		{SNI: "stackoverflow.com"},
		{SNI: "www.mozilla.org"},
		{SNI: "ubuntu.com"},
		{SNI: "www.debian.org"},
		{SNI: "www.kernel.org"},
		{SNI: "www.python.org"},
		{SNI: "nodejs.org"},
		{SNI: "www.php.net"},
		{SNI: "www.mysql.com"},
		{SNI: "www.postgresql.org"},
		{SNI: "www.docker.com"},
		{SNI: "kubernetes.io"},
		{SNI: "www.hashicorp.com"},
		{SNI: "www.atlassian.com"},
		{SNI: "www.jetbrains.com"},
		{SNI: "www.adobe.com"},
		{SNI: "www.autodesk.com"},
		{SNI: "www.oracle.com"},
		{SNI: "www.ibm.com"},
		{SNI: "www.amd.com"},
		{SNI: "www.nvidia.com"},
		{SNI: "www.dell.com"},
		{SNI: "www.lenovo.com"},
		{SNI: "www.asus.com"},
		{SNI: "www.samsung.com"},
		{SNI: "www.sony.com"},
		{SNI: "www.lg.com"},
		{SNI: "www.qualcomm.com"},
		{SNI: "www.broadcom.com"},
		{SNI: "www.ericsson.com"},
		{SNI: "www.nokia.com"},
		{SNI: "www.siemens.com"},
		{SNI: "www.bosch.com"},
		{SNI: "www.honeywell.com"},
		{SNI: "www.salesforce.com"},
		{SNI: "www.sap.com"},
		{SNI: "www.servicenow.com"},
		{SNI: "www.workday.com"},
		{SNI: "slack.com"},
		{SNI: "www.notion.so"},
		{SNI: "www.figma.com"},
		{SNI: "www.dropbox.com"},
		{SNI: "www.box.com"},
		{SNI: "asana.com"},
		{SNI: "monday.com"},
		{SNI: "www.shopify.com"},
		{SNI: "stripe.com"},
		{SNI: "www.paypal.com"},
		{SNI: "www.square.com"},
		{SNI: "www.mastercard.com"},
		{SNI: "www.americanexpress.com"},
		{SNI: "www.netflix.com"},
		{SNI: "www.disney.com"},
		{SNI: "www.twitch.tv"},
		{SNI: "www.imdb.com"},
		{SNI: "www.nytimes.com"},
		{SNI: "www.theguardian.com"},
		{SNI: "www.reuters.com"},
		{SNI: "www.bloomberg.com"},
		{SNI: "www.forbes.com"},
		{SNI: "www.wsj.com"},
		{SNI: "www.ft.com"},
		{SNI: "www.economist.com"},
		{SNI: "www.ieee.org"},
		{SNI: "www.acm.org"},
		{SNI: "www.ted.com"},
		{SNI: "www.duolingo.com"},
		{SNI: "www.edx.org"},
		{SNI: "udemy.com"},
		{SNI: "www.khanacademy.org"},
		{SNI: "wordpress.org"},
		{SNI: "wordpress.com"},
		{SNI: "www.wired.com"},
		{SNI: "techcrunch.com"},
		{SNI: "www.theverge.com"},
		{SNI: "arstechnica.com"},
		{SNI: "www.npr.org"},
		{SNI: "www.pbs.org"},
		{SNI: "www.nationalgeographic.com"},
		{SNI: "time.com"},
		{SNI: "www.ap.org"},
		{SNI: "www.nike.com"},
		{SNI: "www.adidas.com"},
		{SNI: "www.ikea.com"},
		{SNI: "www.uniqlo.com"},
		{SNI: "www.zara.com"},
		{SNI: "www.target.com"},
		{SNI: "www.walmart.com"},
		{SNI: "www.costco.com"},
		{SNI: "www.bestbuy.com"},
		{SNI: "www.homedepot.com"},
		{SNI: "www.ebay.com"},
		{SNI: "www.etsy.com"},
		{SNI: "www.booking.com"},
		{SNI: "www.airbnb.com"},
		{SNI: "www.expedia.com"},
		{SNI: "www.tripadvisor.com"},
		{SNI: "www.marriott.com"},
		{SNI: "www.hilton.com"},
		{SNI: "www.uber.com"},
		{SNI: "www.lyft.com"},
		{SNI: "www.agoda.com"},
		{SNI: "www.kayak.com"},
		{SNI: "www.toyota.com"},
		{SNI: "www.honda.com"},
		{SNI: "www.bmw.com"},
		{SNI: "www.mercedes-benz.com"},
		{SNI: "www.audi.com"},
		{SNI: "www.ford.com"},
		{SNI: "www.tesla.com"},
		{SNI: "www.boeing.com"},
		{SNI: "www.airbus.com"},
		{SNI: "www.emirates.com"},
		{SNI: "www.qatarairways.com"},
		{SNI: "www.singaporeair.com"},
		{SNI: "www.lufthansa.com"},
		{SNI: "www.airfrance.com"},
		{SNI: "www.klm.com"},
		{SNI: "www.britishairways.com"},
		{SNI: "www.united.com"},
		{SNI: "www.aa.com"},
		{SNI: "www.verizon.com"},
		{SNI: "www.att.com"},
		{SNI: "www.tmobile.com"},
		{SNI: "www.vodafone.com"},
		{SNI: "www.orange.com"},
		{SNI: "www.bt.com"},
		{SNI: "www.hsbc.com"},
		{SNI: "www.barclays.co.uk"},
		{SNI: "www.jpmorgan.com"},
		{SNI: "www.goldmansachs.com"},
		{SNI: "www.morganstanley.com"},
		{SNI: "www.bankofamerica.com"},
		{SNI: "www.wellsfargo.com"},
		{SNI: "www.chase.com"},
		{SNI: "www.citi.com"},
		{SNI: "www.nasa.gov"},
		{SNI: "www.nih.gov"},
		{SNI: "www.cdc.gov"},
		{SNI: "www.who.int"},
		{SNI: "www.un.org"},
		{SNI: "www.imf.org"},
		{SNI: "www.worldbank.org"},
	}
}

func normalizeRealityEndpoint(in domain.RealityEndpoint) (domain.RealityEndpoint, error) {
	sni := strings.ToLower(strings.TrimSpace(in.SNI))
	if sni == "" {
		return domain.RealityEndpoint{}, fmt.Errorf("sni required")
	}
	if net.ParseIP(sni) != nil {
		return domain.RealityEndpoint{}, fmt.Errorf("sni must be domain, got ip %q", sni)
	}
	hs := strings.ToLower(strings.TrimSpace(in.HandshakeServer))
	if hs == "" {
		hs = sni
	}
	if net.ParseIP(hs) != nil {
		return domain.RealityEndpoint{}, fmt.Errorf("handshake_server must be domain, got ip %q", hs)
	}
	port := in.HandshakePort
	if port == 0 {
		port = 443
	}
	return domain.RealityEndpoint{
		SNI:             sni,
		HandshakeServer: hs,
		HandshakePort:   port,
	}, nil
}

func isLikelyCDNHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	for _, s := range likelyCDNSuffixes {
		if strings.HasSuffix(h, s) {
			return true
		}
	}
	return false
}

func (s *Service) validateRealityEndpoint(ctx context.Context, ep domain.RealityEndpoint) bool {
	// Conservative filter: skip hosts that look like direct CDN edge entries.
	if isLikelyCDNHost(ep.SNI) || isLikelyCDNHost(ep.HandshakeServer) {
		return false
	}
	resolver := net.DefaultResolver
	if _, err := resolver.LookupIPAddr(ctx, ep.SNI); err != nil {
		return false
	}
	if _, err := resolver.LookupIPAddr(ctx, ep.HandshakeServer); err != nil {
		return false
	}
	address := net.JoinHostPort(ep.HandshakeServer, strconv.Itoa(int(ep.HandshakePort)))
	dialer := net.Dialer{Timeout: 2 * time.Second}
	c, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func randomRealityShortID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func generateRealityKeyPair() (privateRawURL string, publicRawURL string, err error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	privateRawURL = base64.RawURLEncoding.EncodeToString(priv.Bytes())
	publicRawURL = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	return privateRawURL, publicRawURL, nil
}

func realityInboundKey(setName, presetName string) string {
	return setName + "/" + presetName
}

func (s *Service) loadRealityConfig() (domain.RealityConfig, error) {
	cfg, ok, err := s.store.LoadRealityConfig()
	if err != nil {
		return domain.RealityConfig{}, err
	}
	if ok {
		return cfg, nil
	}
	now := time.Now().UTC()
	cfg = domain.RealityConfig{
		EffectiveProfiles: defaultRealityProfiles(),
		UpdatedAt:         &now,
	}
	if err := s.store.SaveRealityConfig(cfg); err != nil {
		return domain.RealityConfig{}, err
	}
	return cfg, nil
}

func (s *Service) validateRealityPool(ctx context.Context, profiles []domain.RealityEndpoint) []domain.RealityEndpoint {
	out := make([]domain.RealityEndpoint, 0, len(profiles))
	seen := map[string]struct{}{}
	for _, raw := range profiles {
		ep, err := normalizeRealityEndpoint(raw)
		if err != nil {
			continue
		}
		key := ep.SNI + "|" + ep.HandshakeServer + "|" + strconv.Itoa(int(ep.HandshakePort))
		if _, ok := seen[key]; ok {
			continue
		}
		if s.validateRealityEndpoint(ctx, ep) {
			seen[key] = struct{}{}
			out = append(out, ep)
		}
	}
	return out
}

func hasRealityPreset(sets []domain.InboundSet) bool {
	for _, set := range sets {
		for _, b := range set.EffectiveBindings() {
			p, err := presets.Get(b.Preset)
			if err != nil {
				continue
			}
			for _, t := range p.Traits {
				if t == "reality" {
					return true
				}
			}
		}
	}
	return false
}

func presetHasTrait(p domain.ProtocolPreset, trait string) bool {
	for _, t := range p.Traits {
		if t == trait {
			return true
		}
	}
	return false
}

func (s *Service) refreshRealityConfig(ctx context.Context, force bool) (domain.RealityConfig, bool, error) {
	cfg, err := s.loadRealityConfig()
	if err != nil {
		return domain.RealityConfig{}, false, err
	}
	if !force && cfg.UpdatedAt != nil && time.Since(*cfg.UpdatedAt) < realityValidationInterval {
		return cfg, false, nil
	}
	defaults := s.validateRealityPool(ctx, defaultRealityProfiles())
	if len(defaults) == 0 {
		defaults = normalizedRealityDefaults()
	}
	effective := defaults
	usingUser := false
	if len(cfg.UserProfiles) > 0 {
		validUser := s.validateRealityPool(ctx, cfg.UserProfiles)
		if len(validUser) > 0 {
			effective = validUser
			usingUser = true
		} else {
			// Silent failover: invalid user profile list is dropped.
			cfg.UserProfiles = nil
		}
	}
	now := time.Now().UTC()
	changed := usingUser != cfg.UsingUserOverrides || !sameRealityPool(cfg.EffectiveProfiles, effective) || len(cfg.UserProfiles) == 0 && cfg.UsingUserOverrides
	cfg.EffectiveProfiles = effective
	cfg.UsingUserOverrides = usingUser
	cfg.UpdatedAt = &now
	if changed {
		if err := s.store.SaveRealityConfig(cfg); err != nil {
			return domain.RealityConfig{}, false, err
		}
	}
	return cfg, changed, nil
}

func normalizedRealityDefaults() []domain.RealityEndpoint {
	base := defaultRealityProfiles()
	out := make([]domain.RealityEndpoint, 0, len(base))
	for _, p := range base {
		ep, err := normalizeRealityEndpoint(p)
		if err != nil {
			continue
		}
		out = append(out, ep)
	}
	return out
}

func sameRealityPool(a, b []domain.RealityEndpoint) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func poolContainsEndpoint(pool []domain.RealityEndpoint, ep domain.RealityEndpoint) bool {
	for _, p := range pool {
		if p.SNI == ep.SNI && p.HandshakeServer == ep.HandshakeServer && p.HandshakePort == ep.HandshakePort {
			return true
		}
	}
	return false
}

func (s *Service) ensureRealityAssignments(sets []domain.InboundSet, profiles []domain.RealityEndpoint) (map[string]domain.RealityAssignment, bool, error) {
	assignments, err := s.store.LoadRealityAssignments()
	if err != nil {
		return nil, false, err
	}
	if assignments == nil {
		assignments = map[string]domain.RealityAssignment{}
	}
	if len(profiles) == 0 {
		return assignments, false, nil
	}
	now := time.Now().UTC()
	changed := false
	needed := map[string]struct{}{}
	type needItem struct {
		key       string
		setName   string
		preset    string
		preferSNI string
	}
	var items []needItem
	for _, set := range sets {
		for _, b := range set.EffectiveBindings() {
			p, err := presets.Get(b.Preset)
			if err != nil || !presetHasTrait(p, "reality") {
				continue
			}
			key := realityInboundKey(set.Name, p.Name)
			needed[key] = struct{}{}
			prefer := ""
			if b.Params != nil {
				prefer = strings.ToLower(strings.TrimSpace(b.Params["demux_sni"]))
			}
			items = append(items, needItem{key: key, setName: set.Name, preset: p.Name, preferSNI: prefer})
		}
	}
	usedSNI := map[string]string{} // sni → inbound key (only among needed)
	for _, it := range items {
		a, ok := assignments[it.key]
		if !ok || a.SNI == "" {
			continue
		}
		valid := poolContainsEndpoint(profiles, domain.RealityEndpoint{
			SNI:             a.SNI,
			HandshakeServer: a.HandshakeServer,
			HandshakePort:   a.HandshakePort,
		}) && a.PrivateKeyBase64 != "" && a.PublicKeyBase64 != "" && a.ShortID != ""
		if !valid {
			continue
		}
		sni := strings.ToLower(a.SNI)
		if owner, taken := usedSNI[sni]; taken && owner != it.key {
			continue // duplicate — will reassign below
		}
		usedSNI[sni] = it.key
	}
	for _, it := range items {
		key := it.key
		preferSNI := it.preferSNI
		a, ok := assignments[key]
		valid := ok && poolContainsEndpoint(profiles, domain.RealityEndpoint{
			SNI:             a.SNI,
			HandshakeServer: a.HandshakeServer,
			HandshakePort:   a.HandshakePort,
		}) && a.PrivateKeyBase64 != "" && a.PublicKeyBase64 != "" && a.ShortID != ""
		if valid {
			cur := strings.ToLower(a.SNI)
			if preferSNI != "" && cur != preferSNI {
				if ep, found := findRealityEndpointBySNI(profiles, preferSNI); found {
					if owner, taken := usedSNI[preferSNI]; !taken || owner == key {
						a.SNI = ep.SNI
						a.HandshakeServer = ep.HandshakeServer
						a.HandshakePort = ep.HandshakePort
						a.UpdatedAt = now
						assignments[key] = a
						delete(usedSNI, cur)
						usedSNI[preferSNI] = key
						changed = true
						continue
					}
				}
			}
			if owner, taken := usedSNI[cur]; !taken || owner == key {
				usedSNI[cur] = key
				continue
			}
			// duplicate SNI — fall through
		}
		ep, err := pickRealityEndpoint(profiles, preferSNI, usedSNI, key)
		if err != nil {
			return nil, false, err
		}
		priv, pub, err := generateRealityKeyPair()
		if err != nil {
			return nil, false, err
		}
		shortID, err := randomRealityShortID()
		if err != nil {
			return nil, false, err
		}
		if old, ok := assignments[key]; ok && old.SNI != "" {
			delete(usedSNI, strings.ToLower(old.SNI))
		}
		assignments[key] = domain.RealityAssignment{
			InboundKey:       key,
			SNI:              ep.SNI,
			HandshakeServer:  ep.HandshakeServer,
			HandshakePort:    ep.HandshakePort,
			PrivateKeyBase64: priv,
			PublicKeyBase64:  pub,
			ShortID:          shortID,
			UpdatedAt:        now,
		}
		usedSNI[strings.ToLower(ep.SNI)] = key
		changed = true
	}
	for key := range assignments {
		if _, ok := needed[key]; ok {
			continue
		}
		delete(assignments, key)
		changed = true
	}
	if changed {
		if err := s.store.SaveRealityAssignments(assignments); err != nil {
			return nil, false, err
		}
	}
	return assignments, changed, nil
}

func findRealityEndpointBySNI(pool []domain.RealityEndpoint, sni string) (domain.RealityEndpoint, bool) {
	want := strings.ToLower(strings.TrimSpace(sni))
	for _, ep := range pool {
		if strings.ToLower(ep.SNI) == want {
			return ep, true
		}
	}
	return domain.RealityEndpoint{}, false
}

// pickRealityEndpoint prefers demux_sni, then any unused SNI from the validated pool.
func pickRealityEndpoint(pool []domain.RealityEndpoint, preferSNI string, usedSNI map[string]string, selfKey string) (domain.RealityEndpoint, error) {
	if len(pool) == 0 {
		return domain.RealityEndpoint{}, fmt.Errorf("no validated reality profiles available")
	}
	prefer := strings.ToLower(strings.TrimSpace(preferSNI))
	if prefer != "" {
		if ep, ok := findRealityEndpointBySNI(pool, prefer); ok {
			if owner, taken := usedSNI[prefer]; !taken || owner == selfKey {
				return ep, nil
			}
		}
	}
	unused := make([]domain.RealityEndpoint, 0, len(pool))
	for _, ep := range pool {
		sni := strings.ToLower(ep.SNI)
		if owner, taken := usedSNI[sni]; taken && owner != selfKey {
			continue
		}
		unused = append(unused, ep)
	}
	if len(unused) == 0 {
		// Last resort: allow reuse (demux sync will still force match per inbound).
		return randomRealityEndpoint(pool)
	}
	return randomRealityEndpoint(unused)
}

func randomRealityEndpoint(pool []domain.RealityEndpoint) (domain.RealityEndpoint, error) {
	if len(pool) == 0 {
		return domain.RealityEndpoint{}, fmt.Errorf("no validated reality profiles available")
	}
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return domain.RealityEndpoint{}, err
	}
	idx := int(b[0])<<8 | int(b[1])
	idx %= len(pool)
	return pool[idx], nil
}

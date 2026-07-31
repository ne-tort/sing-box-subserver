//go:build with_controlplane

package materialize

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	"golang.org/x/crypto/curve25519"
)

// Input for building a server config.
type Input struct {
	ActiveSets         []domain.InboundSet
	Users              []domain.User
	PublicHost         string
	DataDir            string
	TLS                domain.TLSProfile
	TLSCertPath        string // self_signed PEM after ensure
	TLSKeyPath         string
	CertManager        domain.CertManager
	DNS                json.RawMessage // raw dns object; empty → default
	Route              json.RawMessage // raw route object; empty → default
	// SlotTLS maps demux_sni → dedicated self-signed PEM paths (optional).
	SlotTLS            map[string]SlotTLSMaterial
	RealityAssignments map[string]domain.RealityAssignment
	WgHub              *domain.WgHub
}

// SlotTLSMaterial is per-SNI PEM material for demux TLS members.
type SlotTLSMaterial struct {
	CertPath string
	KeyPath  string
}

type SubscriptionFilters struct {
	Set           string
	Presets       []string
	Variants      []string
	Tags          []string
	Profiles      []string
	Flow          []string
	Network       string
	StrictFilters bool
}

var leftoverToken = regexp.MustCompile(`\{\{[^{}]+\}\}`)

// Build returns canonical sing-box server JSON.
func Build(in Input) ([]byte, error) {
	if err := in.TLS.Validate(); err != nil {
		return nil, fmt.Errorf("tls profile: %w", err)
	}
	if err := in.CertManager.Validate(); err != nil {
		return nil, fmt.Errorf("cert_manager: %w", err)
	}
	dnsRaw := in.DNS
	if len(bytes.TrimSpace(dnsRaw)) == 0 {
		dnsRaw = domain.DefaultDNSFragment()
	}
	if err := domain.ValidateDNSFragment(dnsRaw); err != nil {
		return nil, err
	}
	routeRaw := in.Route
	if len(bytes.TrimSpace(routeRaw)) == 0 {
		routeRaw = domain.DefaultRouteFragment()
	}
	if err := domain.ValidateRouteFragment(routeRaw); err != nil {
		return nil, err
	}
	var dnsObj, routeObj any
	if err := json.Unmarshal(dnsRaw, &dnsObj); err != nil {
		return nil, fmt.Errorf("dns: %w", err)
	}
	if err := json.Unmarshal(routeRaw, &routeObj); err != nil {
		return nil, fmt.Errorf("route: %w", err)
	}

	serverName := in.TLS.ServerNameForTLS(in.PublicHost)
	inbounds := make([]any, 0)
	for _, set := range in.ActiveSets {
		built, err := buildSet(set, in, serverName)
		if err != nil {
			return nil, err
		}
		inbounds = append(inbounds, built...)
	}
	doc := map[string]any{
		"log":       map[string]any{"level": "warn"},
		"dns":       dnsObj,
		"inbounds":  inbounds,
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "block", "tag": "block"},
		},
		"route": routeObj,
	}
	if in.WgHub != nil && in.WgHub.Enabled {
		ep, err := BuildWireGuardEndpoint(*in.WgHub, in.Users, in.PublicHost)
		if err != nil {
			return nil, err
		}
		if ep != nil {
			doc["endpoints"] = []any{ep}
		}
	}
	if prov := certificateProviders(in); prov != nil {
		doc["certificate_providers"] = prov
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if leftoverToken.Match(raw) {
		return nil, fmt.Errorf("unresolved template tokens in materialize output")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

func certificateProviders(in Input) []any {
	if !in.CertManager.Enabled() {
		return nil
	}
	// Emit when domains configured (sing-box obtains; inbounds opt-in via params.sni).
	a := in.CertManager
	domains := a.NormalizedDomains()
	prov := a.Provider
	if prov == "" {
		prov = "letsencrypt"
	}
	dataDir := filepath.Join(in.DataDir, "controlplane", "acme")
	obj := map[string]any{
		"type":           "acme",
		"tag":            domain.TLSProviderTag,
		"domain":         domains,
		"email":          a.Email,
		"provider":       prov,
		"data_directory": dataDir,
	}
	if a.KeyType != "" {
		obj["key_type"] = a.KeyType
	}
	if a.DisableHTTPChallenge {
		obj["disable_http_challenge"] = true
	}
	if a.DisableTLSALPNChallenge {
		obj["disable_tls_alpn_challenge"] = true
	}
	if a.AlternativeHTTPPort > 0 {
		obj["alternative_http_port"] = a.AlternativeHTTPPort
	}
	if a.AlternativeTLSPort > 0 {
		obj["alternative_tls_port"] = a.AlternativeTLSPort
	}
	if len(a.DNS01Challenge) > 0 {
		obj["dns01_challenge"] = a.DNS01Challenge
	}
	return []any{obj}
}

func activeSetsNeedTLS(sets []domain.InboundSet) bool {
	for _, set := range sets {
		for _, pn := range uniqueBindingPresets(set.EffectiveBindings()) {
			p, err := presets.Get(pn)
			if err != nil {
				continue
			}
			if presetNeedsTLS(p) {
				return true
			}
		}
	}
	return false
}

func buildSet(set domain.InboundSet, in Input, serverName string) ([]any, error) {
	bindings := set.EffectiveBindings()
	if len(bindings) == 0 {
		return nil, fmt.Errorf("set %q: empty presets", set.Name)
	}
	presetsList := uniqueBindingPresets(bindings)
	if !set.HasDemux() && len(presetsList) != 1 {
		return nil, fmt.Errorf("set %q: without demux exactly one preset required", set.Name)
	}
	memberPorts := resolveMemberPorts(set, presetsList)
	out := make([]any, 0, len(presetsList)+1)
	if set.HasDemux() {
		demux, err := cloneMap(set.DemuxTemplate)
		if err != nil {
			return nil, err
		}
		demux["type"] = "demux"
		demux["tag"] = fmt.Sprintf("cp-demux-%s", set.Name)
		demux["listen"] = set.Listen
		demux["listen_port"] = set.ListenPort
		raw, _ := json.Marshal(demux)
		s := string(raw)
		for _, pn := range presetsList {
			s = strings.ReplaceAll(s, "{{tag:"+pn+"}}", fmt.Sprintf("cp-in-%s-%s", set.Name, pn))
		}
		if leftoverToken.MatchString(s) {
			return nil, fmt.Errorf("set %q demux: unresolved tokens", set.Name)
		}
		if err := json.Unmarshal([]byte(s), &demux); err != nil {
			return nil, fmt.Errorf("set %q demux: %w", set.Name, err)
		}
		rewriteDemuxActionsToDial(demux, set.Name, memberPorts)
		syncDemuxSNIFromBindings(demux, set, memberPorts)
		syncDemuxSNIWithReality(demux, set, memberPorts, in.RealityAssignments)
		out = append(out, demux)
	}
	for _, b := range bindings {
		pn := b.Preset
		p, err := presets.Get(pn)
		if err != nil {
			return nil, err
		}
		ib, err := cloneMap(p.InboundTemplate)
		if err != nil {
			return nil, err
		}
		tag := fmt.Sprintf("cp-in-%s-%s", set.Name, pn)
		listen := set.Listen
		port := set.ListenPort
		if set.HasDemux() {
			listen = "127.0.0.1"
			port = memberPorts[canonicalPresetName(pn)]
			if port == 0 {
				port = privatePort(set.Name, pn)
			}
		}
		slotSNI := strings.TrimSpace(b.Params["demux_sni"])
		acmeSNI := strings.TrimSpace(b.Params[domain.BindingParamSNI])
		effectiveServer := serverName
		useCertManager := false
		if !domain.BindingUsesReality(p, b.Params) {
			if acmeSNI != "" && in.CertManager.HasDomain(acmeSNI) {
				effectiveServer = strings.ToLower(acmeSNI)
				useCertManager = true
				// Keep demux match aligned with ACME leaf SNI.
				if set.HasDemux() {
					slotSNI = effectiveServer
				}
			} else if slotSNI != "" {
				effectiveServer = slotSNI
			}
		}
		vars := map[string]string{
			"{{tag}}":         tag,
			"{{listen}}":      listen,
			"{{listen_port}}": strconv.FormatUint(uint64(port), 10),
			"{{server}}":      effectiveServer,
		}
		for field, val := range peerSecretsForPreset(set, p.Name) {
			vars["{{peer."+field+"}}"] = val
		}
		applyBindingParamVars(vars, paramsForDemuxSlot(b.Params, p.Protocol, slotSNI), paramDefaultsForPreset(p.Name))
		ib, err = substituteMap(ib, vars)
		if err != nil {
			return nil, err
		}
		applyCustomPresetInboundKnobs(ib, p.Name, b.Params)
		ib["tag"] = tag
		if presetHasTrait(p, "no_listen") || (p.Protocol == "carrier" && !presetHasTrait(p, "needs_listen")) {
			// SFU / cloudflared underlay does not bind set.listen_port.
			delete(ib, "listen")
			delete(ib, "listen_port")
		} else {
			ib["listen"] = listen
			ib["listen_port"] = port
		}
		if p.Protocol == "carrier" {
			finalizeCarrierInbound(ib, set, p, listen, port, b)
		}

		userArr := make([]any, 0)
		for _, u := range in.Users {
			creds := presets.CredsFor(u.Creds, pn)
			if creds == nil {
				continue
			}
			variants := domain.UserVariantsForProtocol(p.Protocol, b, p.DefaultUserVariants)
			if len(variants) > 0 {
				for _, vv := range variants {
					id := creds[vv.CredentialField]
					if id == nil || id == "" {
						continue
					}
					entry := map[string]any{
						"name": u.Name + "-" + vv.Name,
						"uuid": id,
					}
					if vv.FlowValue != "" {
						entry["flow"] = vv.FlowValue
					}
					userArr = append(userArr, entry)
				}
				continue
			}
			entry := inboundUserEntry(p.Protocol, u.Name, creds)
			userArr = append(userArr, entry)
		}
		ib["users"] = userArr
		if presetHasTrait(p, "shared_key") || presetHasTrait(p, "shared_auth") || presetHasTrait(p, "no_users") {
			delete(ib, "users")
		} else if len(userArr) == 0 {
			if err := applyZeroEligibleFallback(ib, p.Protocol); err != nil {
				return nil, fmt.Errorf("set %q preset %q: %w", set.Name, pn, err)
			}
		} else if p.Protocol == "shadowsocks" && !shadowsocksNeedsServerPassword(ib) {
			delete(ib, "password")
		}
		if domain.BindingUsesReality(p, b.Params) {
			rk := set.Name + "/" + p.Name
			assignment, ok := in.RealityAssignments[rk]
			if !ok {
				return nil, fmt.Errorf("missing reality assignment for %s", rk)
			}
			attachInboundReality(ib, assignment)
		} else if presetHasTrait(p, "tls_custom") {
			certPath, keyPath := in.TLSCertPath, in.TLSKeyPath
			if m, ok := in.SlotTLS[effectiveServer]; ok && m.CertPath != "" {
				certPath, keyPath = m.CertPath, m.KeyPath
			}
			slotIn := in
			slotIn.TLSCertPath, slotIn.TLSKeyPath = certPath, keyPath
			if err := attachTrustTunnelTLS(ib, slotIn, effectiveServer); err != nil {
				return nil, fmt.Errorf("set %q preset %q trusttunnel tls: %w", set.Name, pn, err)
			}
		} else if mode, ok := domain.BindingTLSMode(p, b.Params); ok {
			if mode == "none" {
				delete(ib, "tls")
			} else {
				attachInboundTLS(ib, in, effectiveServer, useCertManager)
				if !useCertManager {
					if m, ok := in.SlotTLS[effectiveServer]; ok && m.CertPath != "" {
						applySlotTLSPaths(ib, m)
					}
				}
				if alpn := strings.TrimSpace(b.Params["demux_alpn"]); alpn != "" {
					applyInboundALPN(ib, strings.Split(alpn, ","))
				}
			}
		} else if presetNeedsTLS(p) {
			attachInboundTLS(ib, in, effectiveServer, useCertManager)
			if !useCertManager {
				if m, ok := in.SlotTLS[effectiveServer]; ok && m.CertPath != "" {
					applySlotTLSPaths(ib, m)
				}
			}
			if alpn := strings.TrimSpace(b.Params["demux_alpn"]); alpn != "" {
				applyInboundALPN(ib, strings.Split(alpn, ","))
			}
		}
		out = append(out, ib)
	}
	return out, nil
}

func canonicalPresetName(pn string) string {
	if c, ok := presets.CanonicalTag(pn); ok {
		return c
	}
	return pn
}

func resolveMemberPorts(set domain.InboundSet, presetsList []string) map[string]uint16 {
	out := map[string]uint16{}
	for _, pn := range presetsList {
		canon := canonicalPresetName(pn)
		if set.MemberPorts != nil {
			if p, ok := set.MemberPorts[canon]; ok && p != 0 {
				out[canon] = p
				out[pn] = p
				continue
			}
			if p, ok := set.MemberPorts[pn]; ok && p != 0 {
				out[canon] = p
				out[pn] = p
				continue
			}
		}
		p := privatePort(set.Name, canon)
		out[canon] = p
		out[pn] = p
	}
	return out
}

// syncDemuxSNIFromBindings aligns demux match SNI with binding demux_sni / params.sni.
func syncDemuxSNIFromBindings(demux map[string]any, set domain.InboundSet, memberPorts map[string]uint16) {
	portToSNI := map[uint16]string{}
	for _, b := range set.EffectiveBindings() {
		p, err := presets.Get(b.Preset)
		if err != nil || domain.BindingUsesReality(p, b.Params) {
			continue
		}
		sni := strings.TrimSpace(b.Params[domain.BindingParamSNI])
		if sni == "" {
			sni = strings.TrimSpace(b.Params["demux_sni"])
		}
		if sni == "" {
			continue
		}
		pn := canonicalPresetName(b.Preset)
		port := memberPorts[pn]
		if port == 0 {
			port = memberPorts[b.Preset]
		}
		if port != 0 {
			portToSNI[port] = strings.ToLower(sni)
		}
	}
	applyDemuxPortSNI(demux, portToSNI)
}

// syncDemuxSNIWithReality forces demux TLS/QUIC SNI matches to Reality assignment SNIs.
// ClientHello SNI for Reality equals assignment.SNI; demux_sni from install must not diverge.
func syncDemuxSNIWithReality(demux map[string]any, set domain.InboundSet, memberPorts map[string]uint16, assignments map[string]domain.RealityAssignment) {
	if len(assignments) == 0 {
		return
	}
	portToSNI := map[uint16]string{}
	for _, b := range set.EffectiveBindings() {
		p, err := presets.Get(b.Preset)
		if err != nil || !presetHasTrait(p, "reality") {
			continue
		}
		rk := set.Name + "/" + p.Name
		a, ok := assignments[rk]
		if !ok || a.SNI == "" {
			continue
		}
		pn := canonicalPresetName(b.Preset)
		port := memberPorts[pn]
		if port == 0 {
			port = memberPorts[b.Preset]
		}
		if port != 0 {
			portToSNI[port] = a.SNI
		}
	}
	applyDemuxPortSNI(demux, portToSNI)
}

func applyDemuxPortSNI(demux map[string]any, portToSNI map[uint16]string) {
	if len(portToSNI) == 0 {
		return
	}
	rules, ok := demux["rules"].([]any)
	if !ok {
		return
	}
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		action, _ := rule["action"].(map[string]any)
		dial, _ := action["dial"].(map[string]any)
		if dial == nil {
			continue
		}
		var port uint16
		switch v := dial["port"].(type) {
		case float64:
			port = uint16(v)
		case int:
			port = uint16(v)
		case uint16:
			port = v
		case json.Number:
			n, _ := v.Int64()
			port = uint16(n)
		}
		sni, ok := portToSNI[port]
		if !ok || sni == "" {
			continue
		}
		match, _ := rule["match"].(map[string]any)
		if match == nil {
			match = map[string]any{}
			rule["match"] = match
		}
		if tlsMatch, ok := match["tls"].(map[string]any); ok {
			tlsMatch["sni"] = []any{sni}
		} else if _, hasSNI := match["sni"]; hasSNI {
			match["sni"] = []any{sni}
		} else {
			match["tls"] = map[string]any{"sni": []any{sni}}
		}
	}
}

// rewriteDemuxActionsToDial converts inject (inbound.tag) into dial/forward to 127.0.0.1.
func rewriteDemuxActionsToDial(demux map[string]any, setName string, memberPorts map[string]uint16) {
	rules, ok := demux["rules"].([]any)
	if !ok {
		return
	}
	prefix := fmt.Sprintf("cp-in-%s-", setName)
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		action, ok := rule["action"].(map[string]any)
		if !ok {
			continue
		}
		if _, hasDial := action["dial"]; hasDial {
			continue
		}
		inbound, ok := action["inbound"].(map[string]any)
		if !ok {
			continue
		}
		tag, _ := inbound["tag"].(string)
		if tag == "" || !strings.HasPrefix(tag, prefix) {
			continue
		}
		preset := strings.TrimPrefix(tag, prefix)
		port := memberPorts[preset]
		if port == 0 {
			port = memberPorts[canonicalPresetName(preset)]
		}
		if port == 0 {
			continue
		}
		delete(action, "inbound")
		action["dial"] = map[string]any{
			"address": "127.0.0.1",
			"port":    port,
		}
	}
}

// applyZeroEligibleFallback keeps Apply/validate succeeding when no eligible users
// remain, without leaving an open proxy or a known shared password.
func applyZeroEligibleFallback(ib map[string]any, protocol string) error {
	secret, err := randomSecret(16)
	if err != nil {
		return err
	}
	switch protocol {
	case "shadowsocks":
		// Multi-user SS with empty users[] fails validate ("missing password").
		delete(ib, "users")
		ib["password"] = secret
	case "socks", "http":
		// Empty users[] often means open proxy — fail closed with inert creds.
		ib["users"] = []any{
			map[string]any{"username": "cp-inert", "password": secret},
		}
	case "mixed":
		ib["users"] = []any{
			map[string]any{"username": "cp-inert", "password": secret},
		}
	case "trojan", "hysteria2", "anytls", "shadowtls", "mieru":
		ib["users"] = []any{
			map[string]any{"name": "cp-inert", "password": secret},
		}
	case "hysteria":
		ib["users"] = []any{
			map[string]any{"name": "cp-inert", "auth_str": secret},
		}
	case "snell":
		ib["users"] = []any{
			map[string]any{"name": "cp-inert", "userkey": secret},
		}
	case "ssh":
		ib["users"] = []any{
			map[string]any{"user": "cp-inert", "password": secret},
		}
	case "cloudflared":
		delete(ib, "users")
	case "naive":
		ib["users"] = []any{
			map[string]any{"username": "cp-inert", "password": secret},
		}
	case "shadowquic", "trusttunnel":
		ib["users"] = []any{
			map[string]any{"username": "cp-inert", "password": secret},
		}
	case "derp":
		priv, err := randomCurve25519PrivateMaterialize()
		if err != nil {
			return err
		}
		pub, err := curve25519PublicFromPrivateMaterialize(priv)
		if err != nil {
			return err
		}
		ib["users"] = []any{
			map[string]any{"name": "cp-inert", "public_key": pub},
		}
		if _, ok := ib["private_key"]; !ok || ib["private_key"] == "" || ib["private_key"] == "{{peer.private_key}}" {
			ib["private_key"] = priv
		}
	case "carrier":
		ib["users"] = []any{
			map[string]any{"name": "cp-inert", "device_id": "cp-inert", "secret": secret},
		}
	case "sudoku":
		// Shared key lives in top-level key; leave users absent.
		delete(ib, "users")
		if _, ok := ib["key"]; !ok || ib["key"] == "" || ib["key"] == "{{peer.key}}" {
			ib["key"] = secret
		}
	case "vless", "vmess", "tuic":
		id, err := randomUUID()
		if err != nil {
			return err
		}
		entry := map[string]any{"name": "cp-inert", "uuid": id}
		if protocol == "tuic" {
			entry["password"] = secret
		}
		ib["users"] = []any{entry}
	default:
		// Unknown protocols: leave as-is (caller/tests may assert empty).
	}
	return nil
}

func randomUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func randomSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomCurve25519PrivateMaterialize() (string, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return "", err
	}
	k[0] &= 248
	k[31] &= 127
	k[31] |= 64
	return base64.RawURLEncoding.EncodeToString(k[:]), nil
}

func curve25519PublicFromPrivateMaterialize(privB64 string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(privB64)
	if err != nil {
		raw, err = base64.StdEncoding.DecodeString(privB64)
	}
	if err != nil {
		return "", err
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("curve25519 private key length %d", len(raw))
	}
	pub, err := curve25519.X25519(raw, curve25519.Basepoint)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(pub), nil
}

func peerSecretsForPreset(set domain.InboundSet, preset string) map[string]string {
	if len(set.PeerSecrets) == 0 {
		return nil
	}
	prefix := preset + "/"
	out := map[string]string{}
	for k, v := range set.PeerSecrets {
		if strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = v
		}
	}
	return out
}

func applyPeerSecretVars(vars map[string]string, set domain.InboundSet, preset string) {
	for field, val := range peerSecretsForPreset(set, preset) {
		vars["{{peer."+field+"}}"] = val
	}
}

func applyUserCredVars(vars map[string]string, userName string, creds map[string]any) {
	vars["{{user.name}}"] = userName
	if creds == nil {
		return
	}
	for k, v := range creds {
		if v == nil {
			continue
		}
		vars["{{user."+k+"}}"] = fmt.Sprint(v)
	}
	// Ensure common placeholders exist even when empty (legacy templates).
	for _, k := range []string{"password", "uuid", "username", "auth_str", "userkey", "private_key", "public_key", "device_id", "secret", "key"} {
		key := "{{user." + k + "}}"
		if _, ok := vars[key]; !ok {
			vars[key] = ""
		}
	}
}

func paramDefaultsForPreset(name string) map[string]string {
	inv, err := presets.GetInvariant(name)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for field, meta := range inv.ParamMeta {
		if strings.TrimSpace(meta.Default) != "" {
			out[field] = strings.TrimSpace(meta.Default)
		}
	}
	notes := inv.ClientNotes
	if notes == nil {
		return out
	}
	if v := notes["ws_path_default"]; v != "" {
		out["ws_path"] = v
	}
	if v := notes["hu_path_default"]; v != "" {
		out["hu_path"] = v
	}
	if v := notes["http_path_default"]; v != "" {
		out["http_path"] = v
	}
	return out
}

func applyBindingParamVars(vars map[string]string, params map[string]string, presetDefaults map[string]string) {
	// Defaults for optional operator overrides; empty for required ParamFields
	// (carrier room, cloudflared token, hy2 masquerade_dir / realm_*) until set.
	defaults := map[string]string{
		"room": "", "token": "", "key": "", "peer": "",
		"server": "", "server_port": "", "vk_hash": "", "wrap_password": "",
		"wg_port": "", "password": "", "device_id": "",
		// ShadowQUIC JLS
		"jls_addr": "www.cloudflare.com:443", "jls_server_name": "www.cloudflare.com",
		// ShadowTLS handshake
		"handshake_server": "www.apple.com",
		// WS / HTTPUpgrade / HTTP transports
		"ws_host": "{{server}}", "ws_path": "/ws",
		"hu_host": "{{server}}", "hu_path": "/upgrade",
		"http_host": "{{server}}", "http_path": "/http",
		// Hy2 masquerade proxy (file/realm use required param_fields)
		"masquerade_url": "https://www.cloudflare.com",
		"masquerade_dir": "",
		"masquerade_mode": "",
		"realm_server_url": "", "realm_id": "",
		// Sudoku / Snell / DERP / SSH / Mieru
		"fallback": "http://127.0.0.1:80",
		"obfs_host": "www.bing.com",
		"path": "/derp",
		"server_version": "SSH-2.0-OpenSSH_8.9",
		"traffic_pattern": "",
		"httpmask_path": "/sudoku", "httpmask_host": "{{server}}",
		// Custom constructors (vless_custom / hy2_custom / wg_custom)
		"transport": "tcp", "tls_mode": "tls",
		"flow": "", "packet_encoding": "xudp", "fingerprint": "chrome",
		"transport_path": "/vless", "transport_host": "{{server}}", "service_name": "GunService",
		"alpn": "h2,http/1.1",
		"obfs": "", "obfs_password": "",
		"up_mbps": "100", "down_mbps": "100",
		"mtu": "1408", "jc": "", "jmin": "", "jmax": "",
		"i1": "", "i2": "", "i3": "", "i4": "", "i5": "",
		// TUIC / TrustTunnel / SS / Naive constructors
		"congestion_control": "bbr", "udp_relay_mode": "native", "zero_rtt": "false",
		"mode": "auto", "method": "aes-128-gcm", "network": "tcp",
	}
	for k, v := range presetDefaults {
		if strings.TrimSpace(v) != "" {
			defaults[k] = v
		}
	}
	for k, v := range defaults {
		vars["{{param."+k+"}}"] = v
	}
	for k, v := range params {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		vars["{{param."+k+"}}"] = v
	}
	// Expand {{server}} inside param defaults after PublicHost is known.
	server := vars["{{server}}"]
	for k, v := range vars {
		if strings.HasPrefix(k, "{{param.") && strings.Contains(v, "{{server}}") {
			vars[k] = strings.ReplaceAll(v, "{{server}}", server)
		}
	}
}

// paramsForDemuxSlot copies binding params and aligns ShadowQUIC JLS SNI with demux_sni.
// Without this, demux/client use demux_sni while inbound jls_upstream stays at cloudflare default → handshake fail.
func paramsForDemuxSlot(params map[string]string, protocol, demuxSNI string) map[string]string {
	out := map[string]string{}
	for k, v := range params {
		out[k] = v
	}
	demuxSNI = strings.TrimSpace(demuxSNI)
	if demuxSNI == "" || !strings.EqualFold(strings.TrimSpace(protocol), "shadowquic") {
		return out
	}
	out["jls_server_name"] = demuxSNI
	if strings.TrimSpace(out["jls_addr"]) == "" || strings.HasPrefix(out["jls_addr"], "www.cloudflare.com") {
		out["jls_addr"] = demuxSNI + ":443"
	}
	return out
}

func carrierLink(obj map[string]any) map[string]any {
	if obj == nil {
		return nil
	}
	link, _ := obj["link"].(map[string]any)
	if link == nil {
		link = map[string]any{}
		obj["link"] = link
	}
	return link
}

func pruneEmptyLinkFields(link map[string]any) {
	if link == nil {
		return
	}
	for k, v := range link {
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) == "" {
				delete(link, k)
			}
		case float64:
			if t == 0 {
				delete(link, k)
			}
		case int:
			if t == 0 {
				delete(link, k)
			}
		case nil:
			delete(link, k)
		}
	}
}

func finalizeCarrierInbound(ib map[string]any, set domain.InboundSet, p domain.ProtocolPreset, listen string, port uint16, b domain.SetBinding) {
	link := carrierLink(ib)
	provider, _ := ib["provider"].(string)
	provider = strings.ToLower(provider)
	switch provider {
	case "peer":
		host := listen
		if host == "" || host == "::" || host == "0.0.0.0" {
			// Underlay may bind all interfaces; keep explicit IP for link.peer.
			host = "0.0.0.0"
		}
		link["peer"] = net.JoinHostPort(host, strconv.FormatUint(uint64(port), 10))
		ib["listen"] = listen
		ib["listen_port"] = port
	case "vk":
		if srv := strings.TrimSpace(fmt.Sprint(link["server"])); srv == "" || strings.Contains(srv, "{{") {
			link["server"] = "0.0.0.0"
		}
		if sp, _ := toUint16(link["server_port"]); sp == 0 {
			link["server_port"] = port
		}
		ib["listen"] = listen
		ib["listen_port"] = port
	}
	if pw := peerSecretsForPreset(set, p.Name)["password"]; pw != "" {
		if cur := strings.TrimSpace(fmt.Sprint(link["password"])); cur == "" || strings.Contains(cur, "{{") {
			link["password"] = pw
		}
	}
	if sp, ok := toUint16(link["server_port"]); ok {
		link["server_port"] = sp
	}
	pruneEmptyLinkFields(link)
	_ = b
}

func finalizeCarrierOutbound(ob map[string]any, set domain.InboundSet, p domain.ProtocolPreset, creds map[string]any, b domain.SetBinding, publicHost, serverName string) {
	delete(ob, "server")
	delete(ob, "server_port")
	delete(ob, "listen")
	delete(ob, "listen_port")
	link := carrierLink(ob)
	provider, _ := ob["provider"].(string)
	provider = strings.ToLower(provider)
	host := publicHost
	if host == "" {
		host = serverName
	}
	switch provider {
	case "peer":
		if peer := strings.TrimSpace(fmt.Sprint(link["peer"])); peer == "" || strings.Contains(peer, "{{") {
			link["peer"] = net.JoinHostPort(host, strconv.FormatUint(uint64(set.ListenPort), 10))
		}
	case "vk":
		if srv := strings.TrimSpace(fmt.Sprint(link["server"])); srv == "" || strings.Contains(srv, "{{") {
			link["server"] = host
		}
		if sp, _ := toUint16(link["server_port"]); sp == 0 {
			link["server_port"] = set.ListenPort
		}
	}
	if creds != nil {
		if id, ok := creds["device_id"]; ok && strings.TrimSpace(fmt.Sprint(link["device_id"])) == "" {
			link["device_id"] = id
		}
		if presetHasTrait(p, "users_auth") {
			if sec, ok := creds["secret"]; ok {
				link["password"] = sec
			}
		}
	}
	if pw := peerSecretsForPreset(set, p.Name)["password"]; pw != "" && !presetHasTrait(p, "users_auth") {
		if cur := strings.TrimSpace(fmt.Sprint(link["password"])); cur == "" || strings.Contains(cur, "{{") {
			link["password"] = pw
		}
	}
	// Room/token/transport from binding params win when still empty after substitute.
	if b.Params != nil {
		for _, k := range []string{"room", "token", "transport", "key", "vk_hash", "wrap_password", "peer", "server"} {
			if v := strings.TrimSpace(b.Params[k]); v != "" {
				if cur := strings.TrimSpace(fmt.Sprint(link[k])); cur == "" || strings.Contains(cur, "{{") {
					link[k] = v
				}
			}
		}
		if v := strings.TrimSpace(b.Params["server_port"]); v != "" {
			if sp, _ := toUint16(link["server_port"]); sp == 0 {
				if n, err := strconv.ParseUint(v, 10, 16); err == nil {
					link["server_port"] = uint16(n)
				}
			}
		}
	}
	if sp, ok := toUint16(link["server_port"]); ok {
		link["server_port"] = sp
	}
	pruneEmptyLinkFields(link)
}

func toUint16(v any) (uint16, bool) {
	switch t := v.(type) {
	case float64:
		if t <= 0 {
			return 0, false
		}
		return uint16(t), true
	case int:
		if t <= 0 {
			return 0, false
		}
		return uint16(t), true
	case uint16:
		return t, t > 0
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(t), 10, 16)
		if err != nil || n == 0 {
			return 0, false
		}
		return uint16(n), true
	default:
		return 0, false
	}
}

func applyShadowsocksOutboundPassword(ob map[string]any, set domain.InboundSet, p domain.ProtocolPreset, creds map[string]any) {
	method, _ := ob["method"].(string)
	peers := peerSecretsForPreset(set, p.Name)
	sp := peers["password"]
	// PSK-only presets (e.g. 2022-chacha): never SIP022 server:user combine.
	if presetHasTrait(p, "shared_key") || presetHasTrait(p, "no_users") || presetHasTrait(p, "shared_auth") {
		if sp != "" {
			ob["password"] = sp
			return
		}
	}
	pw, ok := creds["password"]
	if !ok {
		return
	}
	if strings.HasPrefix(method, "2022-") {
		if sp != "" {
			// SIP022 multi-user client password: server_psk:user_psk
			ob["password"] = sp + ":" + fmt.Sprint(pw)
			return
		}
	}
	ob["password"] = pw
}

func shadowsocksNeedsServerPassword(ib map[string]any) bool {
	method, _ := ib["method"].(string)
	return strings.HasPrefix(method, "2022-")
}

func inboundUserEntry(protocol, name string, creds map[string]any) map[string]any {
	switch protocol {
	case "ssh":
		entry := map[string]any{}
		if u, ok := creds["username"]; ok {
			entry["user"] = u
		} else if u, ok := creds["user"]; ok {
			entry["user"] = u
		} else {
			entry["user"] = name
		}
		if pw, ok := creds["password"]; ok {
			entry["password"] = pw
		}
		if pk, ok := creds["public_key"]; ok {
			entry["public_key"] = pk
		}
		return entry
	case "socks", "http", "naive", "mixed", "shadowquic", "trusttunnel":
		entry := map[string]any{}
		if u, ok := creds["username"]; ok {
			entry["username"] = u
		} else {
			entry["username"] = name
		}
		if pw, ok := creds["password"]; ok {
			entry["password"] = pw
		}
		return entry
	case "derp":
		entry := map[string]any{"name": name}
		if pk, ok := creds["public_key"]; ok {
			entry["public_key"] = pk
		}
		return entry
	case "carrier":
		entry := map[string]any{"name": name}
		if id, ok := creds["device_id"]; ok {
			entry["device_id"] = id
		}
		if sec, ok := creds["secret"]; ok {
			entry["secret"] = sec
		}
		return entry
	case "sudoku":
		// Shared-key protocol — inbound users are not used; keep empty for fallback path.
		return map[string]any{"name": name}
	case "hysteria":
		entry := map[string]any{"name": name}
		if a, ok := creds["auth_str"]; ok {
			entry["auth_str"] = a
		}
		return entry
	case "snell":
		entry := map[string]any{"name": name}
		if k, ok := creds["userkey"]; ok {
			entry["userkey"] = k
		}
		return entry
	case "mieru":
		entry := map[string]any{"name": name}
		if pw, ok := creds["password"]; ok {
			entry["password"] = pw
		}
		return entry
	default:
		entry := map[string]any{"name": name}
		for k, v := range creds {
			entry[k] = v
		}
		return entry
	}
}

func presetNeedsTLS(p domain.ProtocolPreset) bool {
	return presetHasTrait(p, "tls")
}

func presetHasTrait(p domain.ProtocolPreset, want string) bool {
	for _, t := range p.Traits {
		if t == want {
			return true
		}
	}
	return false
}

func attachInboundTLS(ib map[string]any, in Input, serverName string, useCertManager bool) {
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj == nil {
		tlsObj = map[string]any{}
	}
	tlsObj["enabled"] = true
	tlsObj["server_name"] = serverName
	delete(tlsObj, "certificate_path")
	delete(tlsObj, "key_path")
	delete(tlsObj, "certificate_provider")
	delete(tlsObj, "certificate")
	delete(tlsObj, "key")
	if useCertManager {
		tlsObj["certificate_provider"] = domain.TLSProviderTag
	} else {
		tlsObj["certificate_path"] = in.TLSCertPath
		tlsObj["key_path"] = in.TLSKeyPath
	}
	ib["tls"] = tlsObj
}

func attachSubscriptionTLS(ob map[string]any, defaultServer string, b domain.SetBinding, cm domain.CertManager) {
	tlsObj, _ := ob["tls"].(map[string]any)
	if tlsObj == nil {
		tlsObj = map[string]any{}
	}
	tlsObj["enabled"] = true
	sni := strings.TrimSpace(b.Params[domain.BindingParamSNI])
	if sni == "" {
		sni = strings.TrimSpace(b.Params["demux_sni"])
	}
	if sni != "" {
		tlsObj["server_name"] = sni
	} else {
		tlsObj["server_name"] = defaultServer
	}
	if domain.NeedsTLSReportsInsecure(b.Params[domain.BindingParamSNI], cm) {
		tlsObj["insecure"] = true
	} else {
		delete(tlsObj, "insecure")
	}
	ob["tls"] = tlsObj
}

func applySlotTLSPaths(ib map[string]any, m SlotTLSMaterial) {
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj == nil {
		return
	}
	tlsObj["certificate_path"] = m.CertPath
	tlsObj["key_path"] = m.KeyPath
	delete(tlsObj, "certificate_provider")
	delete(tlsObj, "certificate")
	delete(tlsObj, "key")
}

func applyInboundALPN(ib map[string]any, alpns []string) {
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj == nil {
		return
	}
	out := make([]any, 0, len(alpns))
	for _, a := range alpns {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, a)
		}
	}
	if len(out) > 0 {
		tlsObj["alpn"] = out
	}
}

// attachTrustTunnelTLS maps CP TLS material into TrustTunnel's custom tls block
// (certificate/private_key PEM strings, not certificate_path).
func attachTrustTunnelTLS(ib map[string]any, in Input, serverName string) error {
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj == nil {
		tlsObj = map[string]any{}
	}
	tlsObj["server_name"] = serverName
	if hn, _ := ib["hostname"].(string); hn == "" || hn == "{{server}}" {
		ib["hostname"] = serverName
	}
	if in.TLSCertPath == "" || in.TLSKeyPath == "" {
		return fmt.Errorf("trusttunnel requires PEM paths (self_signed or ACME); set Input.TLSCertPath/TLSKeyPath")
	}
	certPEM, err := os.ReadFile(in.TLSCertPath)
	if err != nil {
		return fmt.Errorf("trusttunnel certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(in.TLSKeyPath)
	if err != nil {
		return fmt.Errorf("trusttunnel private_key: %w", err)
	}
	tlsObj["certificate"] = string(certPEM)
	tlsObj["private_key"] = string(keyPEM)
	delete(tlsObj, "skip_verification")
	ib["tls"] = tlsObj
	return nil
}

func attachInboundReality(ib map[string]any, assignment domain.RealityAssignment) {
	tlsObj, _ := ib["tls"].(map[string]any)
	if tlsObj == nil {
		tlsObj = map[string]any{}
	}
	tlsObj["enabled"] = true
	tlsObj["server_name"] = assignment.SNI
	delete(tlsObj, "certificate_path")
	delete(tlsObj, "key_path")
	delete(tlsObj, "certificate_provider")
	delete(tlsObj, "certificate")
	delete(tlsObj, "key")
	tlsObj["reality"] = map[string]any{
		"enabled":     true,
		"private_key": assignment.PrivateKeyBase64,
		"short_id":    []any{assignment.ShortID},
		"handshake": map[string]any{
			"server":      assignment.HandshakeServer,
			"server_port": assignment.HandshakePort,
		},
	}
	ib["tls"] = tlsObj
	alignTransportHostToSNI(ib, assignment.SNI)
}

func privatePort(setName, preset string) uint16 {
	h := uint32(0)
	for _, c := range setName + "/" + preset {
		h = h*33 + uint32(c)
	}
	return uint16(41000 + (h % 20000))
}

func cloneMap(m map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func applyTuicZeroRTT(m map[string]any, preset string, params map[string]string) {
	if preset != "tuic_custom" {
		return
	}
	if strings.EqualFold(strings.TrimSpace(params["zero_rtt"]), "true") {
		m["zero_rtt_handshake"] = true
	} else {
		delete(m, "zero_rtt_handshake")
	}
}

func applyCustomPresetInboundKnobs(ib map[string]any, preset string, params map[string]string) {
	applyTuicZeroRTT(ib, preset, params)
	applyNaiveNetworkKnobs(ib, preset, params)
}

func applyCustomPresetOutboundKnobs(ob map[string]any, preset string, params map[string]string) {
	applyTuicZeroRTT(ob, preset, params)
	applyNaiveNetworkKnobs(ob, preset, params)
}

func applyNaiveNetworkKnobs(m map[string]any, preset string, params map[string]string) {
	if preset != "naive_custom" {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(params["network"]), "udp") {
		return
	}
	m["network"] = "udp"
	if tls, ok := m["tls"].(map[string]any); ok {
		tls["alpn"] = []any{"h3"}
	}
	m["quic_congestion_control"] = "bbr"
	m["quic"] = true
}

func substituteMap(m map[string]any, vars map[string]string) (map[string]any, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	s := string(raw)
	for k, v := range vars {
		esc, _ := json.Marshal(v)
		s = strings.ReplaceAll(s, k, string(esc[1:len(esc)-1]))
	}
	if leftoverToken.MatchString(s) {
		return nil, fmt.Errorf("unresolved template tokens: %s", leftoverToken.FindString(s))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RenderSubscription builds client outbounds JSON for one user.
func RenderSubscription(user domain.User, sets []domain.InboundSet, publicHost string, tls domain.TLSProfile, cm domain.CertManager, filters SubscriptionFilters, realityAssignments map[string]domain.RealityAssignment, hub *domain.WgHub) ([]byte, error) {
	if filters.StrictFilters {
		cat := BuildSubscriptionCatalog(sets)
		if err := cat.Validate(filters); err != nil {
			return nil, fmt.Errorf("cp_invalid_sub_filter: %w", err)
		}
	}
	serverName := tls.ServerNameForTLS(publicHost)
	presetOK := func(pn string) bool {
		if len(filters.Presets) == 0 {
			return true
		}
		for _, f := range filters.Presets {
			if f == pn {
				return true
			}
		}
		return false
	}
	outbounds := make([]any, 0)
	variantOK := func(name string) bool {
		if len(filters.Variants) == 0 {
			return true
		}
		for _, v := range filters.Variants {
			if v == name {
				return true
			}
		}
		return false
	}
	tagAnyOK := func(tags []string) bool {
		if len(filters.Tags) == 0 {
			return true
		}
		for _, t := range tags {
			for _, ft := range filters.Tags {
				if t == ft {
					return true
				}
			}
		}
		return false
	}
	flowAllowed := func(flowKey string) bool {
		if len(filters.Flow) == 0 {
			return true
		}
		for _, f := range filters.Flow {
			if f == flowKey {
				return true
			}
			// small aliases
			if flowKey == "xtls-rprx-vision" && f == "xtls" {
				return true
			}
			if flowKey == "none" && (f == "" || f == "tcp") {
				// "tcp" alias isn't meaningful for VLESS flow, but keeps accidental
				// mixes from breaking filters hard.
				return true
			}
		}
		return false
	}
	for _, set := range sets {
		if filters.Set != "" && set.Name != filters.Set {
			continue
		}
		for _, b := range set.EffectiveBindings() {
			pn := b.Preset
			if !presetOK(pn) {
				continue
			}
			p, err := presets.Get(pn)
			if err != nil {
				return nil, err
			}
			if len(p.OutboundTemplate) == 0 || presetHasTrait(p, "inbound_only") {
				continue
			}
			creds := presets.CredsFor(user.Creds, pn)
			if creds == nil {
				continue
			}
			profiles := domain.ClientProfilesForProtocol(p.Protocol, b, p.DefaultClientProfiles)
			if len(filters.Profiles) > 0 {
				names := make([]string, 0, len(profiles)+len(b.EnabledClientProfiles))
				for _, cp := range profiles {
					names = append(names, cp.Name)
				}
				names = append(names, b.EnabledClientProfiles...)
				if !hasAny(filters.Profiles, names) {
					continue
				}
			}
			profileOK := func(name string) bool {
				if len(filters.Profiles) == 0 {
					return true
				}
				for _, f := range filters.Profiles {
					if f == name {
						return true
					}
				}
				// Legacy: EnabledClientProfiles may be opaque subscription tags
				// (not catalog profile names). Matching those tags admits the
				// resolved catalog profiles for this binding.
				for _, f := range filters.Profiles {
					for _, e := range b.EnabledClientProfiles {
						if f != e {
							continue
						}
						if !domain.IsKnownClientProfile(p.Protocol, f) {
							return true
						}
					}
				}
				return false
			}
			variants := domain.UserVariantsForProtocol(p.Protocol, b, p.DefaultUserVariants)
			if len(variants) > 0 {
				baseTag := fmt.Sprintf("cp-out-%s-%s", set.Name, pn)
				addVlessOutbound := func(tag string, uuid any, flow string, overrides map[string]any) error {
					if uuid == nil || uuid == "" {
						return nil
					}
					ob, err := cloneMap(p.OutboundTemplate)
					if err != nil {
						return err
					}
					vars := map[string]string{
						"{{tag}}":    tag,
						"{{server}}": serverName,
					}
					applyUserCredVars(vars, user.Name, creds)
					vars["{{user.uuid}}"] = fmt.Sprint(uuid)
					applyPeerSecretVars(vars, set, p.Name)
					slotSNI := strings.TrimSpace(b.Params["demux_sni"])
					applyBindingParamVars(vars, paramsForDemuxSlot(b.Params, p.Protocol, slotSNI), paramDefaultsForPreset(p.Name))
					ob, err = substituteMap(ob, vars)
					if err != nil {
						return err
					}
					applyCustomPresetOutboundKnobs(ob, p.Name, b.Params)
					ob["tag"] = tag
					ob["server"] = publicHost
					if publicHost == "" {
						ob["server"] = serverName
					}
					ob["server_port"] = set.ListenPort
					ob["uuid"] = uuid
					if filters.Network == "udp" || filters.Network == "tcp" {
						ob["network"] = filters.Network
					}

					if flow == "" {
						delete(ob, "flow")
					} else {
						ob["flow"] = flow
					}
					domain.ApplyOutboundOverrides(ob, overrides)

					if presetHasTrait(p, "reality") {
						rk := set.Name + "/" + p.Name
						assignment, ok := realityAssignments[rk]
						if !ok {
							return fmt.Errorf("missing reality assignment for %s", rk)
						}
						attachOutboundReality(ob, assignment)
					} else if presetNeedsTLS(p) {
						attachSubscriptionTLS(ob, serverName, b, cm)
					}
					stripUTLSForQUICTransport(ob)
					sanitizeNaiveOutboundTLS(ob)

					outbounds = append(outbounds, ob)
					return nil
				}

				emitProfiles := profiles
				if len(emitProfiles) == 0 {
					emitProfiles = []domain.ClientProfileSpec{{Name: "default", SubscriptionDefault: true}}
				}
				for _, vv := range variants {
					if !variantOK(vv.Name) {
						continue
					}
					bindingTags := append([]string{}, b.SubscriptionTags...)
					allTags := append(bindingTags, vv.QueryTags...)
					flowKey := "none"
					if vv.FlowValue != "" {
						flowKey = vv.FlowValue
					}
					if !flowAllowed(flowKey) {
						continue
					}
					tagSuffix := strings.ReplaceAll(vv.Name, "flow-", "")
					for _, cp := range emitProfiles {
						if !profileOK(cp.Name) {
							continue
						}
						cpTags := append(allTags, cp.QueryTags...)
						if !tagAnyOK(cpTags) {
							continue
						}
						tag := baseTag + "-" + tagSuffix
						if len(emitProfiles) > 1 {
							tag = tag + "-" + cp.Name
						}
						if err := addVlessOutbound(tag, creds[vv.CredentialField], vv.FlowValue, cp.OutboundOverrides); err != nil {
							return nil, err
						}
					}
				}
				continue
			}

			emitProfiles := profiles
			if len(emitProfiles) == 0 {
				emitProfiles = []domain.ClientProfileSpec{{Name: "default", SubscriptionDefault: true}}
			}
			for _, cp := range emitProfiles {
				if !profileOK(cp.Name) {
					continue
				}
				bindingTags := append([]string{}, b.SubscriptionTags...)
				cpTags := append(bindingTags, cp.QueryTags...)
				if !tagAnyOK(cpTags) {
					continue
				}
				ob, err := cloneMap(p.OutboundTemplate)
				if err != nil {
					return nil, err
				}
				tag := fmt.Sprintf("cp-out-%s-%s", set.Name, pn)
				if len(emitProfiles) > 1 {
					tag = tag + "-" + cp.Name
				}
				vars := map[string]string{
					"{{tag}}":         tag,
					"{{server}}":      serverName,
					"{{listen_port}}": strconv.FormatUint(uint64(set.ListenPort), 10),
				}
				applyUserCredVars(vars, user.Name, creds)
				applyPeerSecretVars(vars, set, p.Name)
				slotSNI := strings.TrimSpace(b.Params["demux_sni"])
				applyBindingParamVars(vars, paramsForDemuxSlot(b.Params, p.Protocol, slotSNI), paramDefaultsForPreset(p.Name))
				ob, err = substituteMap(ob, vars)
				if err != nil {
					return nil, err
				}
				applyCustomPresetOutboundKnobs(ob, p.Name, b.Params)
				ob["tag"] = tag
				if p.Protocol == "carrier" {
					finalizeCarrierOutbound(ob, set, p, creds, b, publicHost, serverName)
				} else {
					ob["server"] = publicHost
					if publicHost == "" {
						ob["server"] = serverName
					}
					ob["server_port"] = set.ListenPort
				}
				if p.Protocol == "shadowsocks" {
					applyShadowsocksOutboundPassword(ob, set, p, creds)
				}
				if p.Protocol == "trojan" {
					if pw, ok := creds["password"]; ok {
						ob["password"] = pw
					}
				}
				domain.ApplyOutboundOverrides(ob, cp.OutboundOverrides)
				if presetHasTrait(p, "reality") {
					rk := set.Name + "/" + p.Name
					assignment, ok := realityAssignments[rk]
					if !ok {
						return nil, fmt.Errorf("missing reality assignment for %s", rk)
					}
					attachOutboundReality(ob, assignment)
				} else if presetNeedsTLS(p) {
					attachSubscriptionTLS(ob, serverName, b, cm)
				}
				stripUTLSForQUICTransport(ob)
				sanitizeNaiveOutboundTLS(ob)
				outbounds = append(outbounds, ob)
			}
		}
	}
	sortOutboundsByTag(outbounds)
	doc := map[string]any{
		"outbounds": outbounds,
		"meta": map[string]any{
			"matched": len(outbounds),
		},
	}
	if hub != nil && hub.Enabled {
		ep, err := RenderWireGuardClientEndpoint(user, *hub, publicHost)
		if err != nil {
			return nil, err
		}
		if ep != nil {
			doc["endpoints"] = []any{ep}
		}
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if leftoverToken.Match(raw) {
		return nil, fmt.Errorf("unresolved template tokens in subscription")
	}
	return raw, nil
}

func attachOutboundReality(ob map[string]any, assignment domain.RealityAssignment) {
	tlsObj, _ := ob["tls"].(map[string]any)
	if tlsObj == nil {
		tlsObj = map[string]any{}
	}
	tlsObj["enabled"] = true
	tlsObj["server_name"] = assignment.SNI
	delete(tlsObj, "insecure")
	tlsObj["utls"] = map[string]any{"enabled": true, "fingerprint": "chrome"}
	tlsObj["reality"] = map[string]any{
		"enabled":    true,
		"public_key": assignment.PublicKeyBase64,
		"short_id":   assignment.ShortID,
	}
	if _, ok := tlsObj["alpn"]; !ok {
		tlsObj["alpn"] = []any{"h2", "http/1.1"}
	}
	ob["tls"] = tlsObj
	alignTransportHostToSNI(ob, assignment.SNI)
	stripUTLSForQUICTransport(ob)
}

// alignTransportHostToSNI sets WS/HTTPUpgrade/HTTP Host headers to Reality SNI
// so camouflage Host matches ClientHello SNI (default param.ws_host was {{server}}=public_host).
func alignTransportHostToSNI(obj map[string]any, sni string) {
	sni = strings.TrimSpace(sni)
	if sni == "" || obj == nil {
		return
	}
	tr, _ := obj["transport"].(map[string]any)
	if tr == nil {
		return
	}
	switch tr["type"] {
	case "ws", "httpupgrade", "http":
	default:
		return
	}
	headers, _ := tr["headers"].(map[string]any)
	if headers == nil {
		headers = map[string]any{}
		tr["headers"] = headers
	}
	headers["Host"] = []any{sni}
}

// stripUTLSForQUICTransport removes utls from outbounds whose V2Ray transport
// is QUIC or hysteria (uTLS is unsupported there).
func stripUTLSForQUICTransport(ob map[string]any) {
	tr, _ := ob["transport"].(map[string]any)
	if tr == nil {
		return
	}
	switch tr["type"] {
	case "quic", "hysteria":
	default:
		return
	}
	tlsObj, _ := ob["tls"].(map[string]any)
	if tlsObj == nil {
		return
	}
	delete(tlsObj, "utls")
	ob["tls"] = tlsObj
}

// sanitizeNaiveOutboundTLS strips TLS knobs rejected by cronet-backed naive outbound
// (insecure, utls, alpn, …). Prefer pinning certificate when insecure would be needed.
func sanitizeNaiveOutboundTLS(ob map[string]any) {
	if typ, _ := ob["type"].(string); typ != "naive" {
		return
	}
	tlsObj, _ := ob["tls"].(map[string]any)
	if tlsObj == nil {
		return
	}
	delete(tlsObj, "insecure")
	delete(tlsObj, "utls")
	delete(tlsObj, "alpn")
	delete(tlsObj, "reality")
	delete(tlsObj, "ech")
	ob["tls"] = tlsObj
}

func uniqueBindingPresets(bindings []domain.SetBinding) []string {
	out := make([]string, 0, len(bindings))
	seen := map[string]struct{}{}
	for _, b := range bindings {
		if b.Preset == "" {
			continue
		}
		if _, ok := seen[b.Preset]; ok {
			continue
		}
		seen[b.Preset] = struct{}{}
		out = append(out, b.Preset)
	}
	return out
}

func hasAny(filter []string, values []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		for _, v := range values {
			if f == v {
				return true
			}
		}
	}
	return false
}

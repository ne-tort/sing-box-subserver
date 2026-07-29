//go:build with_controlplane

package materialize

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

// Input for building a server config.
type Input struct {
	ActiveSets     []domain.InboundSet
	Users          []domain.User
	PublicHost     string
	DataDir        string
	TLS            domain.TLSProfile
	TLSCertPath    string // set for self_signed after ensure
	TLSKeyPath     string
	RealityAssignments map[string]domain.RealityAssignment
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
		"log": map[string]any{"level": "warn"},
		"dns": map[string]any{
			"servers": []any{
				map[string]any{"tag": "local", "type": "local"},
			},
		},
		"inbounds": inbounds,
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "block", "tag": "block"},
		},
		"route": map[string]any{
			"final": "direct",
			"rules": []any{},
		},
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
	switch in.TLS.Mode {
	case domain.TLSModeACMEDomain, domain.TLSModeACMEIP:
		if in.TLS.ACME == nil {
			return nil
		}
		if !activeSetsNeedTLS(in.ActiveSets) {
			return nil
		}
		a := in.TLS.ACME
		prov := a.Provider
		if prov == "" {
			prov = "letsencrypt"
		}
		dataDir := filepath.Join(in.DataDir, "controlplane", "acme")
		obj := map[string]any{
			"type":           "acme",
			"tag":            domain.TLSProviderTag,
			"domain":         a.Domains,
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
	default:
		return nil
	}
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
			port = privatePort(set.Name, pn)
		}
		vars := map[string]string{
			"{{tag}}":    tag,
			"{{listen}}": listen,
			"{{server}}": serverName,
		}
		ib, err = substituteMap(ib, vars)
		if err != nil {
			return nil, err
		}
		ib["tag"] = tag
		ib["listen"] = listen
		ib["listen_port"] = port

		userArr := make([]any, 0)
		for _, u := range in.Users {
			creds := u.Creds[pn]
			if creds == nil {
				continue
			}
			variants := domain.UserVariantsForProtocol(p.Protocol, b)
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
			entry := map[string]any{"name": u.Name}
			for k, v := range creds {
				entry[k] = v
			}
			userArr = append(userArr, entry)
		}
		ib["users"] = userArr
		if len(userArr) == 0 {
			if err := applyZeroEligibleFallback(ib, p.Protocol); err != nil {
				return nil, fmt.Errorf("set %q preset %q: %w", set.Name, pn, err)
			}
		} else if p.Protocol == "shadowsocks" {
			delete(ib, "password")
		}
		if presetHasTrait(p, "reality") {
			rk := set.Name + "/" + pn
			assignment, ok := in.RealityAssignments[rk]
			if !ok {
				return nil, fmt.Errorf("missing reality assignment for %s", rk)
			}
			attachInboundReality(ib, assignment)
		} else if presetNeedsTLS(p) {
			attachInboundTLS(ib, in, serverName)
		}
		out = append(out, ib)
	}
	return out, nil
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
	case "trojan", "hysteria", "hysteria2", "anytls":
		ib["users"] = []any{
			map[string]any{"name": "cp-inert", "password": secret},
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

func attachInboundTLS(ib map[string]any, in Input, serverName string) {
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
	switch in.TLS.Mode {
	case domain.TLSModeSelfSigned:
		tlsObj["certificate_path"] = in.TLSCertPath
		tlsObj["key_path"] = in.TLSKeyPath
	case domain.TLSModeACMEDomain, domain.TLSModeACMEIP:
		tlsObj["certificate_provider"] = domain.TLSProviderTag
	}
	ib["tls"] = tlsObj
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
func RenderSubscription(user domain.User, sets []domain.InboundSet, publicHost string, tls domain.TLSProfile, filters SubscriptionFilters, realityAssignments map[string]domain.RealityAssignment) ([]byte, error) {
	if filters.StrictFilters {
		cat := BuildSubscriptionCatalog(sets)
		if err := cat.Validate(filters); err != nil {
			return nil, fmt.Errorf("cp_invalid_sub_filter: %w", err)
		}
	}
	serverName := tls.ServerNameForTLS(publicHost)
	insecure := tls.NeedsTLSReportsInsecure()
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
			if len(filters.Profiles) > 0 && !hasAny(filters.Profiles, b.EnabledClientProfiles) {
				continue
			}
			p, err := presets.Get(pn)
			if err != nil {
				return nil, err
			}
			creds := user.Creds[pn]
			if creds == nil {
				continue
			}
			variants := domain.UserVariantsForProtocol(p.Protocol, b)
			if len(variants) > 0 {
				baseTag := fmt.Sprintf("cp-out-%s-%s", set.Name, pn)
				addVlessOutbound := func(tag string, uuid any, flow string) error {
					if uuid == nil || uuid == "" {
						return nil
					}
					ob, err := cloneMap(p.OutboundTemplate)
					if err != nil {
						return err
					}
					vars := map[string]string{
						"{{tag}}":           tag,
						"{{server}}":        serverName,
						"{{user.name}}":     user.Name,
						"{{user.password}}": fmt.Sprint(creds["password"]),
						"{{user.uuid}}":     fmt.Sprint(uuid),
						"{{user.username}}": fmt.Sprint(creds["username"]),
					}
					ob, err = substituteMap(ob, vars)
					if err != nil {
						return err
					}
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

					if presetHasTrait(p, "reality") {
						rk := set.Name + "/" + pn
						assignment, ok := realityAssignments[rk]
						if !ok {
							return fmt.Errorf("missing reality assignment for %s", rk)
						}
						attachOutboundReality(ob, assignment)
					} else if presetNeedsTLS(p) {
						tlsObj, _ := ob["tls"].(map[string]any)
						if tlsObj == nil {
							tlsObj = map[string]any{}
						}
						tlsObj["enabled"] = true
						tlsObj["server_name"] = serverName
						if insecure {
							tlsObj["insecure"] = true
						} else {
							delete(tlsObj, "insecure")
						}
						ob["tls"] = tlsObj
					}

					outbounds = append(outbounds, ob)
					return nil
				}

				for _, vv := range variants {
					if !variantOK(vv.Name) {
						continue
					}
					bindingTags := append([]string{}, b.SubscriptionTags...)
					allTags := append(bindingTags, vv.QueryTags...)
					if !tagAnyOK(allTags) {
						continue
					}
					flowKey := "none"
					if vv.FlowValue != "" {
						flowKey = vv.FlowValue
					}
					if !flowAllowed(flowKey) {
						continue
					}
					tagSuffix := strings.ReplaceAll(vv.Name, "flow-", "")
					if err := addVlessOutbound(baseTag+"-"+tagSuffix, creds[vv.CredentialField], vv.FlowValue); err != nil {
						return nil, err
					}
				}
				continue
			}

			ob, err := cloneMap(p.OutboundTemplate)
			if err != nil {
				return nil, err
			}
			tag := fmt.Sprintf("cp-out-%s-%s", set.Name, pn)
			vars := map[string]string{
				"{{tag}}":           tag,
				"{{server}}":        serverName,
				"{{user.name}}":     user.Name,
				"{{user.password}}": fmt.Sprint(creds["password"]),
				"{{user.uuid}}":     fmt.Sprint(creds["uuid"]),
				"{{user.username}}": fmt.Sprint(creds["username"]),
			}
			ob, err = substituteMap(ob, vars)
			if err != nil {
				return nil, err
			}
			ob["tag"] = tag
			ob["server"] = publicHost
			if publicHost == "" {
				ob["server"] = serverName
			}
			ob["server_port"] = set.ListenPort
			if p.Protocol == "shadowsocks" {
				if pw, ok := creds["password"]; ok {
					ob["password"] = pw
				}
			}
			if p.Protocol == "trojan" {
				if pw, ok := creds["password"]; ok {
					ob["password"] = pw
				}
			}
			if presetHasTrait(p, "reality") {
				rk := set.Name + "/" + pn
				assignment, ok := realityAssignments[rk]
				if !ok {
					return nil, fmt.Errorf("missing reality assignment for %s", rk)
				}
				attachOutboundReality(ob, assignment)
			} else if presetNeedsTLS(p) {
				tlsObj, _ := ob["tls"].(map[string]any)
				if tlsObj == nil {
					tlsObj = map[string]any{}
				}
				tlsObj["enabled"] = true
				tlsObj["server_name"] = serverName
				if insecure {
					tlsObj["insecure"] = true
				} else {
					delete(tlsObj, "insecure")
				}
				ob["tls"] = tlsObj
			}
			outbounds = append(outbounds, ob)
		}
	}
	sortOutboundsByTag(outbounds)
	doc := map[string]any{"outbounds": outbounds}
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
	tlsObj["utls"] = map[string]any{"enabled": true}
	tlsObj["reality"] = map[string]any{
		"enabled":    true,
		"public_key": assignment.PublicKeyBase64,
		"short_id":   assignment.ShortID,
	}
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

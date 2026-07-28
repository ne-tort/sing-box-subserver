//go:build with_controlplane

package materialize

import (
	"bytes"
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
		for _, pn := range set.Presets {
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
	if len(set.Presets) == 0 {
		return nil, fmt.Errorf("set %q: empty presets", set.Name)
	}
	if !set.HasDemux() && len(set.Presets) != 1 {
		return nil, fmt.Errorf("set %q: without demux exactly one preset required", set.Name)
	}
	out := make([]any, 0, len(set.Presets)+1)
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
		for _, pn := range set.Presets {
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
	for _, pn := range set.Presets {
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
			entry := map[string]any{"name": u.Name}
			for k, v := range creds {
				entry[k] = v
			}
			userArr = append(userArr, entry)
		}
		ib["users"] = userArr
		if p.Protocol == "shadowsocks" {
			delete(ib, "password")
		}
		if presetNeedsTLS(p) {
			attachInboundTLS(ib, in, serverName)
		}
		out = append(out, ib)
	}
	return out, nil
}

func presetNeedsTLS(p domain.ProtocolPreset) bool {
	for _, t := range p.Traits {
		if t == "tls" {
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
func RenderSubscription(user domain.User, sets []domain.InboundSet, publicHost string, tls domain.TLSProfile, filterSet, filterPreset string) ([]byte, error) {
	serverName := tls.ServerNameForTLS(publicHost)
	insecure := tls.NeedsTLSReportsInsecure()
	outbounds := make([]any, 0)
	for _, set := range sets {
		if filterSet != "" && set.Name != filterSet {
			continue
		}
		for _, pn := range set.Presets {
			if filterPreset != "" && pn != filterPreset {
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
			if p.Protocol == "vless" {
				if id, ok := creds["uuid"]; ok {
					ob["uuid"] = id
				}
			}
			if p.Protocol == "trojan" {
				if pw, ok := creds["password"]; ok {
					ob["password"] = pw
				}
			}
			if presetNeedsTLS(p) {
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

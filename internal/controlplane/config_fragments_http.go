//go:build with_controlplane

package controlplane

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/store"
)

type configFragmentKind string

const (
	configFragmentDNS       configFragmentKind = "dns"
	configFragmentRoute     configFragmentKind = "route"
	configFragmentOutbounds configFragmentKind = "outbounds"
)

func (s *Service) handleConfigGet(w http.ResponseWriter, _ *http.Request) {
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	dnsObj, err := unmarshalFragment(f.EffectiveDNS())
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	routeObj, err := unmarshalFragment(f.EffectiveRoute())
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	outboundsObj, err := unmarshalFragment(f.EffectiveOutbounds())
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{
		"dns":       dnsObj,
		"route":     routeObj,
		"outbounds": outboundsObj,
		"is_default": map[string]any{
			"dns":       f.DNSIsDefault(),
			"route":     f.RouteIsDefault(),
			"outbounds": f.OutboundsIsDefault(),
		},
		"config_mode": s.configModeString(),
	})
}

func (s *Service) handleConfigDNSGet(w http.ResponseWriter, r *http.Request) {
	s.handleConfigFragmentGet(w, r, configFragmentDNS)
}

func (s *Service) handleConfigDNSPut(w http.ResponseWriter, r *http.Request) {
	s.handleConfigFragmentPut(w, r, configFragmentDNS)
}

func (s *Service) handleConfigDNSDelete(w http.ResponseWriter, r *http.Request) {
	s.handleConfigFragmentDelete(w, r, configFragmentDNS)
}

func (s *Service) handleConfigRouteGet(w http.ResponseWriter, r *http.Request) {
	s.handleConfigFragmentGet(w, r, configFragmentRoute)
}

func (s *Service) handleConfigRoutePut(w http.ResponseWriter, r *http.Request) {
	s.handleConfigRoutePutWithRulesets(w, r)
}

func (s *Service) handleConfigRouteDelete(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	setFragment(&f, configFragmentRoute, nil)
	if err := s.store.SaveConfigFragments(f); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	_ = s.store.ClearRulesets()
	live, err := s.rematerializeIfOwner(r.Context())
	if err != nil {
		failJSONData(w, 422, materializeErrorCode(err), err.Error(), map[string]any{
			"persisted": true,
		})
		return
	}
	raw, _ := fragmentEffective(f, configFragmentRoute)
	obj, err := unmarshalFragment(raw)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{
		"route":          obj,
		"is_default":     true,
		"config_mode":    s.configModeString(),
		"rematerialized": live,
		"persisted":      true,
	})
}

func (s *Service) handleConfigRoutePutWithRulesets(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Route    json.RawMessage `json:"route"`
		Rulesets []struct {
			Filename      string `json:"filename"`
			ContentBase64 string `json:"content_base64"`
		} `json:"rulesets"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if err := domain.ValidateRouteFragment(body.Route); err != nil {
		failJSON(w, 400, "cp_invalid_config", err.Error())
		return
	}
	files := map[string][]byte{}
	for i, rs := range body.Rulesets {
		safe, err := store.SafeRulesetFilename(rs.Filename)
		if err != nil {
			failJSON(w, 400, "cp_invalid_config", fmt.Sprintf("rulesets[%d]: %v", i, err))
			return
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rs.ContentBase64))
		if err != nil {
			failJSON(w, 400, "cp_invalid_config", fmt.Sprintf("rulesets[%d]: invalid base64", i))
			return
		}
		if len(raw) == 0 {
			failJSON(w, 400, "cp_invalid_config", fmt.Sprintf("rulesets[%d]: empty content", i))
			return
		}
		files[safe] = raw
	}
	// Rewrite relative rule_set paths to basenames only (materialize resolves abs).
	routeRaw, err := normalizeRouteRulesetPaths(body.Route)
	if err != nil {
		failJSON(w, 400, "cp_invalid_config", err.Error())
		return
	}
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	f.Route = routeRaw
	if err := s.store.SaveConfigFragments(f); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if err := s.store.WriteRulesets(files); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	live, err := s.rematerializeIfOwner(r.Context())
	if err != nil {
		failJSONData(w, 422, materializeErrorCode(err), err.Error(), map[string]any{
			"persisted": true,
		})
		return
	}
	obj, _ := unmarshalFragment(routeRaw)
	okJSON(w, 200, map[string]any{
		"route":            obj,
		"config_mode":      s.configModeString(),
		"rematerialized":   live,
		"persisted":        true,
		"rulesets_written": len(files),
	})
}

func normalizeRouteRulesetPaths(raw json.RawMessage) (json.RawMessage, error) {
	var route map[string]any
	if err := json.Unmarshal(raw, &route); err != nil {
		return nil, err
	}
	rsList, ok := route["rule_set"].([]any)
	if ok {
		for i, item := range rsList {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			path, _ := m["path"].(string)
			path = strings.TrimSpace(path)
			if path == "" {
				continue
			}
			safe, err := store.SafeRulesetFilename(filepath.Base(path))
			if err != nil {
				return nil, fmt.Errorf("rule_set[%d].path: %w", i, err)
			}
			m["path"] = safe
			rsList[i] = m
		}
		route["rule_set"] = rsList
	}
	out, err := json.Marshal(route)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) handleConfigOutboundsGet(w http.ResponseWriter, r *http.Request) {
	s.handleConfigFragmentGet(w, r, configFragmentOutbounds)
}

func (s *Service) handleConfigOutboundsPut(w http.ResponseWriter, r *http.Request) {
	s.handleConfigFragmentPut(w, r, configFragmentOutbounds)
}

func (s *Service) handleConfigOutboundsDelete(w http.ResponseWriter, r *http.Request) {
	s.handleConfigFragmentDelete(w, r, configFragmentOutbounds)
}

func (s *Service) handleConfigFragmentGet(w http.ResponseWriter, _ *http.Request, kind configFragmentKind) {
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	raw, isDefault := fragmentEffective(f, kind)
	obj, err := unmarshalFragment(raw)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{
		string(kind):  obj,
		"is_default":  isDefault,
		"config_mode": s.configModeString(),
	})
}

func (s *Service) handleConfigFragmentPut(w http.ResponseWriter, r *http.Request, kind configFragmentKind) {
	body := map[string]json.RawMessage{}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	raw, ok := body[string(kind)]
	if !ok {
		failJSON(w, 400, "bad_request", string(kind)+" required")
		return
	}
	if err := validateFragmentKind(kind, raw); err != nil {
		failJSON(w, 400, "cp_invalid_config", err.Error())
		return
	}
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	setFragment(&f, kind, raw)
	if err := s.store.SaveConfigFragments(f); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	live, err := s.rematerializeIfOwner(r.Context())
	if err != nil {
		failJSONData(w, 422, materializeErrorCode(err), err.Error(), map[string]any{
			"persisted": true,
		})
		return
	}
	obj, _ := unmarshalFragment(raw)
	okJSON(w, 200, map[string]any{
		string(kind):     obj,
		"config_mode":    s.configModeString(),
		"rematerialized": live,
		"persisted":      true,
	})
}

func (s *Service) handleConfigFragmentDelete(w http.ResponseWriter, r *http.Request, kind configFragmentKind) {
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	setFragment(&f, kind, nil)
	if err := s.store.SaveConfigFragments(f); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	live, err := s.rematerializeIfOwner(r.Context())
	if err != nil {
		failJSONData(w, 422, materializeErrorCode(err), err.Error(), map[string]any{
			"persisted": true,
		})
		return
	}
	raw, _ := fragmentEffective(f, kind)
	obj, err := unmarshalFragment(raw)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{
		string(kind):     obj,
		"is_default":     true,
		"config_mode":    s.configModeString(),
		"rematerialized": live,
		"persisted":      true,
	})
}

func fragmentEffective(f domain.ConfigFragments, kind configFragmentKind) (json.RawMessage, bool) {
	switch kind {
	case configFragmentDNS:
		return f.EffectiveDNS(), f.DNSIsDefault()
	case configFragmentRoute:
		return f.EffectiveRoute(), f.RouteIsDefault()
	case configFragmentOutbounds:
		return f.EffectiveOutbounds(), f.OutboundsIsDefault()
	default:
		return nil, true
	}
}

func setFragment(f *domain.ConfigFragments, kind configFragmentKind, raw json.RawMessage) {
	switch kind {
	case configFragmentDNS:
		f.DNS = raw
	case configFragmentRoute:
		f.Route = raw
	case configFragmentOutbounds:
		f.Outbounds = raw
	}
}

func validateFragmentKind(kind configFragmentKind, raw json.RawMessage) error {
	switch kind {
	case configFragmentDNS:
		return domain.ValidateDNSFragment(raw)
	case configFragmentRoute:
		return domain.ValidateRouteFragment(raw)
	case configFragmentOutbounds:
		return domain.ValidateOutboundsFragment(raw)
	default:
		return nil
	}
}

func unmarshalFragment(raw json.RawMessage) (any, error) {
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (s *Service) configModeString() string {
	if s == nil || s.cfg.Owner == nil {
		return string(configowner.ModeIdle)
	}
	return string(s.cfg.Owner.Owner())
}

// rematerializeIfOwner applies live config when CP owns the dataplane.
// Returns rematerialized=true when Apply was attempted under controlplane ownership.
func (s *Service) rematerializeIfOwner(ctx context.Context) (bool, error) {
	if s.cfg.Owner == nil || s.cfg.Owner.Owner() != configowner.ModeControlplane {
		return false, nil
	}
	if err := s.rematerializeForce(ctx, true); err != nil {
		return false, err
	}
	return true, nil
}

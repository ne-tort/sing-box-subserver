//go:build with_controlplane

package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func (s *Service) handleConfigDNSGet(w http.ResponseWriter, _ *http.Request) {
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	raw := f.EffectiveDNS()
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{
		"dns":         obj,
		"is_default":  len(bytes.TrimSpace(f.DNS)) == 0,
		"config_mode": s.configModeString(),
	})
}

func (s *Service) handleConfigDNSPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DNS json.RawMessage `json:"dns"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if err := domain.ValidateDNSFragment(body.DNS); err != nil {
		failJSON(w, 400, "cp_invalid_config", err.Error())
		return
	}
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	f.DNS = body.DNS
	if err := s.store.SaveConfigFragments(f); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	live, err := s.rematerializeIfOwner(r.Context())
	if err != nil {
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	var obj any
	_ = json.Unmarshal(body.DNS, &obj)
	okJSON(w, 200, map[string]any{
		"dns":            obj,
		"config_mode":    s.configModeString(),
		"rematerialized": live,
	})
}

func (s *Service) handleConfigRouteGet(w http.ResponseWriter, _ *http.Request) {
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	raw := f.EffectiveRoute()
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, map[string]any{
		"route":       obj,
		"is_default":  len(bytes.TrimSpace(f.Route)) == 0,
		"config_mode": s.configModeString(),
	})
}

func (s *Service) handleConfigRoutePut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Route json.RawMessage `json:"route"`
	}
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	if err := domain.ValidateRouteFragment(body.Route); err != nil {
		failJSON(w, 400, "cp_invalid_config", err.Error())
		return
	}
	f, err := s.store.LoadConfigFragments()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	f.Route = body.Route
	if err := s.store.SaveConfigFragments(f); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	live, err := s.rematerializeIfOwner(r.Context())
	if err != nil {
		failJSON(w, 422, materializeErrorCode(err), err.Error())
		return
	}
	var obj any
	_ = json.Unmarshal(body.Route, &obj)
	okJSON(w, 200, map[string]any{
		"route":          obj,
		"config_mode":    s.configModeString(),
		"rematerialized": live,
	})
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

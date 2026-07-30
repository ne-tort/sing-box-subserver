//go:build with_controlplane

package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"

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
	okJSON(w, 200, map[string]any{"dns": obj, "is_default": len(bytes.TrimSpace(f.DNS)) == 0})
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
		failJSON(w, 400, "bad_request", err.Error())
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
	if err := s.rematerializeForce(r.Context(), true); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	var obj any
	_ = json.Unmarshal(body.DNS, &obj)
	okJSON(w, 200, map[string]any{"dns": obj})
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
	okJSON(w, 200, map[string]any{"route": obj, "is_default": len(bytes.TrimSpace(f.Route)) == 0})
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
		failJSON(w, 400, "bad_request", err.Error())
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
	if err := s.rematerializeForce(r.Context(), true); err != nil {
		failJSON(w, 422, "config_invalid", err.Error())
		return
	}
	var obj any
	_ = json.Unmarshal(body.Route, &obj)
	okJSON(w, 200, map[string]any{"route": obj})
}

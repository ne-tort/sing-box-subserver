//go:build with_controlplane

package controlplane

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

func (s *Service) wgPublicView(h domain.WgHub) map[string]any {
	h.Normalize()
	hubAddr, _ := h.HubAddress()
	out := map[string]any{
		"enabled":       h.Enabled,
		"profile":       h.Profile,
		"subnet":        h.Subnet,
		"listen_port":   h.ListenPort,
		"system":        h.System,
		"forward_allow": h.ForwardAllow,
		"internet_allow": h.InternetAllowed(),
		"hub_address":   hubAddr,
		"hub_public_key": h.HubPublicKey,
		"has_awg":       len(h.AWG) > 0,
	}
	if h.Name != "" {
		out["name"] = h.Name
	}
	if h.MTU > 0 {
		out["mtu"] = h.MTU
	}
	if h.UpMbps > 0 {
		out["up_mbps"] = h.UpMbps
	}
	if h.DownMbps > 0 {
		out["down_mbps"] = h.DownMbps
	}
	if len(h.AWG) > 0 {
		awgView := map[string]any{}
		for _, k := range []string{"jc", "jmin", "jmax"} {
			if v, ok := h.AWG[k]; ok {
				awgView[k] = v
			}
		}
		if len(awgView) > 0 {
			out["awg"] = awgView
		}
	}
	return out
}

func (s *Service) handleWgGet(w http.ResponseWriter, _ *http.Request) {
	h, err := s.store.LoadWgHub()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	okJSON(w, 200, s.wgPublicView(h))
}

func (s *Service) handleWgPut(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := decodeBody(r, &body); err != nil {
		failJSON(w, 400, "bad_request", err.Error())
		return
	}
	h, err := s.store.LoadWgHub()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	prevEnabled := h.Enabled
	prevProfile := h.Profile
	if v, ok := body["enabled"].(bool); ok {
		h.Enabled = v
	}
	if v, ok := body["profile"].(string); ok && strings.TrimSpace(v) != "" {
		h.Profile = strings.TrimSpace(v)
	}
	if v, ok := body["subnet"].(string); ok && strings.TrimSpace(v) != "" {
		h.Subnet = strings.TrimSpace(v)
	}
	if v, ok := body["listen_port"].(float64); ok && v > 0 {
		h.ListenPort = uint16(v)
	}
	if v, ok := body["system"].(bool); ok {
		h.System = v
	}
	if v, ok := body["forward_allow"].(bool); ok {
		h.ForwardAllow = v
	}
	if v, ok := body["internet_allow"].(bool); ok {
		h.InternetAllow = &v
	}
	if v, ok := body["name"].(string); ok {
		h.Name = strings.TrimSpace(v)
	}
	if v, ok := body["mtu"].(float64); ok {
		h.MTU = int(v)
	}
	if v, ok := body["up_mbps"].(float64); ok {
		h.UpMbps = int(v)
	}
	if v, ok := body["down_mbps"].(float64); ok {
		h.DownMbps = int(v)
	}
	h.Normalize()
	if err := h.Validate(); err != nil {
		failJSON(w, 400, "cp_invalid_wg", err.Error())
		return
	}
	if err := s.validateWgListenPort(h); err != nil {
		failJSON(w, 409, "cp_port_conflict", err.Error())
		return
	}
	forceAWG := h.Profile != domain.WgProfilePlain && (prevProfile != h.Profile || len(h.AWG) == 0)
	if _, err := s.ensureWgHubSecrets(&h, forceAWG); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if h.Profile == domain.WgProfilePlain {
		h.AWG = nil
	} else if err := mergeWgAWGOverrides(&h, body); err != nil {
		failJSON(w, 400, "cp_invalid_wg", err.Error())
		return
	}

	if h.Enabled {
		if s.cfg.Owner != nil {
			if err := s.claimOwnership(configowner.ModeControlplane, "wg_enable", "wg"); err != nil {
				failJSON(w, 409, "cp_claim_failed", err.Error())
				return
			}
		}
	}

	if err := s.store.SaveWgHub(h); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}

	if h.Enabled || prevEnabled {
		if err := s.rematerialize(r.Context()); err != nil {
			failJSON(w, 422, materializeErrorCode(err), err.Error())
			return
		}
	}

	if !h.Enabled {
		st, _ := s.store.LoadState()
		if len(st.ActiveSets) == 0 && s.cfg.Owner != nil {
			_ = s.claimOwnership(configowner.ModeIdle, "wg_disable", "wg")
		}
	}

	okJSON(w, 200, s.wgPublicView(h))
}

func (s *Service) handleWgRegenerateAWG(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.LoadWgHub()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	h.Normalize()
	if h.Profile == domain.WgProfilePlain {
		failJSON(w, 400, "bad_request", "regenerate-awg only for wg_awg2/wg_awg3")
		return
	}
	if _, err := s.ensureWgHubSecrets(&h, true); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if err := s.store.SaveWgHub(h); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if h.Enabled {
		if err := s.rematerialize(r.Context()); err != nil {
			failJSON(w, 422, materializeErrorCode(err), err.Error())
			return
		}
	}
	okJSON(w, 200, s.wgPublicView(h))
}

// mergeWgAWGOverrides applies operator jc/jmin/jmax (or nested awg{}) onto a generated AWG bundle.
// i1–i5 are rejected — this stack strips CPS slots in materialize.
func mergeWgAWGOverrides(h *domain.WgHub, body map[string]any) error {
	if h == nil || h.Profile == domain.WgProfilePlain {
		return nil
	}
	if h.AWG == nil {
		h.AWG = map[string]any{}
	}
	applyInt := func(dst map[string]any, key string, v any) error {
		switch n := v.(type) {
		case float64:
			if n != float64(int(n)) || n < 0 {
				return fmt.Errorf("awg.%s must be a non-negative integer", key)
			}
			dst[key] = int(n)
		case int:
			if n < 0 {
				return fmt.Errorf("awg.%s must be a non-negative integer", key)
			}
			dst[key] = n
		case string:
			s := strings.TrimSpace(n)
			if s == "" {
				return nil
			}
			var i int
			if _, err := fmt.Sscanf(s, "%d", &i); err != nil || i < 0 {
				return fmt.Errorf("awg.%s must be a non-negative integer", key)
			}
			dst[key] = i
		default:
			return fmt.Errorf("awg.%s has unsupported type", key)
		}
		return nil
	}
	allowed := map[string]struct{}{"jc": {}, "jmin": {}, "jmax": {}}
	if nested, ok := body["awg"].(map[string]any); ok {
		for k, v := range nested {
			k = strings.ToLower(strings.TrimSpace(k))
			if _, ok := allowed[k]; !ok {
				if k == "i1" || k == "i2" || k == "i3" || k == "i4" || k == "i5" {
					return fmt.Errorf("awg.%s is not supported (CPS slots stripped)", k)
				}
				continue
			}
			if err := applyInt(h.AWG, k, v); err != nil {
				return err
			}
		}
	}
	for _, k := range []string{"jc", "jmin", "jmax"} {
		if v, ok := body[k]; ok {
			if err := applyInt(h.AWG, k, v); err != nil {
				return err
			}
		}
	}
	jmin, _ := h.AWG["jmin"].(int)
	jmax, _ := h.AWG["jmax"].(int)
	if jmin > 0 && jmax > 0 && jmax < jmin {
		return fmt.Errorf("awg.jmax must be >= awg.jmin")
	}
	return nil
}

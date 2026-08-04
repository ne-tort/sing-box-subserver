//go:build with_controlplane

package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/wgawg"
)

func (s *Service) wgPublicView(h domain.WgHub) map[string]any {
	h.Normalize()
	hubAddr, _ := h.HubAddress()
	out := map[string]any{
		"enabled":        h.Enabled,
		"profile":        h.Profile,
		"subnet":         h.Subnet,
		"listen_port":    h.ListenPort,
		"system":         h.System,
		"peer_relay":     h.PeerRelay,
		"internet_allow": h.InternetAllowed(),
		"exit_user_id":   h.ExitUserID,
		"hub_address":    hubAddr,
		"hub_public_key": h.HubPublicKey,
		"has_awg":        len(h.AWG) > 0,
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
	out["peer_keepalive"] = h.PeerKeepalive
	ck := h.ClientKeepalive
	if ck <= 0 {
		ck = 25
	}
	out["client_keepalive"] = ck
	if len(h.AWG) > 0 {
		awgView := map[string]any{}
		for _, k := range []string{
			"jc", "jmin", "jmax",
			"s1", "s2", "s3", "s4",
			"h1", "h2", "h3", "h4",
			"id", "ip", "ib",
			"i1", "i2", "i3", "i4", "i5",
			"signature_protocol",
			"header_protection_key", "content_padding_addition",
			"rekey_after_time", "rekey_timeout", "reject_after_time",
			"keepalive_timeout", "max_handshake_attempts",
		} {
			if v, ok := h.AWG[k]; ok {
				awgView[k] = v
			}
		}
		if len(awgView) > 0 {
			out["awg"] = awgView
		}
		manual := false
		for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
			if v, ok := h.AWG[k]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
				manual = true
				break
			}
		}
		if manual {
			out["manual_init"] = true
			out["masquerade_mode"] = "none"
		} else if ip, _ := h.AWG["ip"].(string); strings.TrimSpace(ip) != "" {
			out["masquerade_mode"] = strings.ToLower(strings.TrimSpace(ip))
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
	if v, ok := body["peer_relay"].(bool); ok {
		h.PeerRelay = v
	} else if v, ok := body["forward_allow"].(bool); ok {
		// Legacy alias from older clients.
		h.PeerRelay = v
	}
	if v, ok := body["internet_allow"].(bool); ok {
		h.InternetAllow = &v
	}
	if _, ok := body["exit_user_id"]; ok {
		v, _ := body["exit_user_id"].(string)
		h.ExitUserID = strings.TrimSpace(v)
		if h.ExitUserID != "" {
			users, err := s.store.LoadUsers()
			if err != nil {
				failJSON(w, 500, "internal", err.Error())
				return
			}
			found := false
			for _, u := range users {
				if u.ID != h.ExitUserID {
					continue
				}
				found = true
				if firstWgCreds(u.Creds) == nil {
					failJSON(w, 400, "cp_invalid_wg", fmt.Sprintf("exit_user_id %q has no WG credentials", h.ExitUserID))
					return
				}
				break
			}
			if !found {
				failJSON(w, 400, "cp_invalid_wg", fmt.Sprintf("exit_user_id %q not found", h.ExitUserID))
				return
			}
			h.PeerRelay = true
		}
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
	if v, ok := body["peer_keepalive"].(float64); ok {
		if v < 0 {
			v = 0
		}
		h.PeerKeepalive = int(v)
	}
	if v, ok := body["client_keepalive"].(float64); ok {
		if v < 0 {
			v = 0
		}
		h.ClientKeepalive = int(v)
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
	if h.Profile == domain.WgProfilePlain {
		h.AWG = nil
	} else {
		// Apply operator overrides first so ensureWgHubSecrets does not redraw sugar
		// over an explicit masquerade_mode=none / manual i1–i5 intent.
		if err := mergeWgAWGOverrides(&h, body); err != nil {
			failJSON(w, 400, "cp_invalid_wg", err.Error())
			return
		}
	}
	if _, err := s.ensureWgHubSecrets(&h, forceAWG); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if h.Profile == domain.WgProfilePlain {
		h.AWG = nil
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
	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("masquerade")))
	}
	// Optional JSON body: {"mode":"none"|"quic"|..., "id":"sni.example"}
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body != nil {
		if mode == "" {
			if v, ok := body["mode"].(string); ok {
				mode = strings.ToLower(strings.TrimSpace(v))
			}
		}
		if mode == "" {
			if v, ok := body["masquerade_mode"].(string); ok {
				mode = strings.ToLower(strings.TrimSpace(v))
			}
		}
	}
	preserveID := ""
	if h.AWG != nil {
		preserveID = strings.TrimSpace(fmt.Sprint(h.AWG["id"]))
		if preserveID == "<nil>" {
			preserveID = ""
		}
	}
	if body != nil {
		if v, ok := body["id"].(string); ok && strings.TrimSpace(v) != "" {
			preserveID = strings.TrimSpace(v)
		}
		if v, ok := body["masquerade_url"].(string); ok && strings.TrimSpace(v) != "" {
			preserveID = strings.TrimSpace(v)
		}
	}

	awg3 := h.Profile == domain.WgProfileAWG3
	bundle, err := wgawg.BundleFromExisting(awg3, h.AWG, mode)
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if !wgawg.HasManualCPS(bundle) && preserveID != "" {
		bundle["id"] = preserveID
	}
	h.AWG = bundle

	if strings.TrimSpace(h.HubPrivateKey) == "" || strings.TrimSpace(h.HubPublicKey) == "" {
		if _, err := s.ensureWgHubSecrets(&h, false); err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
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

// mergeWgAWGOverrides applies operator overrides onto a generated AWG bundle.
// Allows jc/jmin/jmax, s1–s4, h1–h4, masquerade id/ip/ib, and optional i1–i5
// (manual CPS). Setting any iN clears id/ip/ib; setting id/ip/ib clears i1–i5.
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
	applyString := func(dst map[string]any, key string, v any) {
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" {
			delete(dst, key)
			return
		}
		dst[key] = s
	}

	intKeys := map[string]struct{}{
		"jc": {}, "jmin": {}, "jmax": {},
		"s1": {}, "s2": {}, "s3": {}, "s4": {},
	}
	stringKeys := map[string]struct{}{
		"h1": {}, "h2": {}, "h3": {}, "h4": {},
		"id": {}, "ip": {}, "ib": {},
		"i1": {}, "i2": {}, "i3": {}, "i4": {}, "i5": {},
		"header_protection_key": {}, "content_padding_addition": {},
		"rekey_after_time": {}, "rekey_timeout": {}, "reject_after_time": {},
		"keepalive_timeout": {}, "max_handshake_attempts": {},
	}

	applyKV := func(k string, v any) error {
		k = strings.ToLower(strings.TrimSpace(k))
		if _, ok := intKeys[k]; ok {
			return applyInt(h.AWG, k, v)
		}
		if _, ok := stringKeys[k]; ok {
			applyString(h.AWG, k, v)
			return nil
		}
		return nil
	}

	if nested, ok := body["awg"].(map[string]any); ok {
		for k, v := range nested {
			if err := applyKV(k, v); err != nil {
				return err
			}
		}
	}
	for _, k := range []string{
		"jc", "jmin", "jmax", "s1", "s2", "s3", "s4",
		"h1", "h2", "h3", "h4", "id", "ip", "ib",
		"i1", "i2", "i3", "i4", "i5",
	} {
		if v, ok := body[k]; ok {
			if err := applyKV(k, v); err != nil {
				return err
			}
		}
	}

	// Top-level masquerade helpers from the client draft.
	if v, ok := body["masquerade_mode"].(string); ok {
		mode := strings.ToLower(strings.TrimSpace(v))
		switch mode {
		case "", "none":
			delete(h.AWG, "ip")
			delete(h.AWG, "id")
			delete(h.AWG, "ib")
		case "quic", "dns", "stun", "sip":
			h.AWG["ip"] = mode
			for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
				delete(h.AWG, k)
			}
		}
	}
	if v, ok := body["masquerade_url"].(string); ok {
		host := strings.TrimSpace(v)
		host = strings.TrimPrefix(host, "https://")
		host = strings.TrimPrefix(host, "http://")
		if i := strings.IndexAny(host, "/:"); i >= 0 {
			host = host[:i]
		}
		if host != "" {
			h.AWG["id"] = host
		}
	}
	if v, ok := body["manual_init"].(bool); ok && v {
		for _, k := range []string{"id", "ip", "ib"} {
			delete(h.AWG, k)
		}
	}

	manualCPS := false
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
		if v, ok := h.AWG[k]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
			manualCPS = true
			break
		}
	}
	if manualCPS {
		for _, k := range []string{"id", "ip", "ib"} {
			delete(h.AWG, k)
		}
	} else if ip, _ := h.AWG["ip"].(string); strings.TrimSpace(ip) != "" {
		for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
			delete(h.AWG, k)
		}
		ip = strings.ToLower(strings.TrimSpace(ip))
		switch ip {
		case "quic", "dns", "stun", "sip":
			h.AWG["ip"] = ip
		default:
			return fmt.Errorf("awg.ip must be one of quic|dns|stun|sip")
		}
		if _, ok := h.AWG["ib"]; !ok || strings.TrimSpace(fmt.Sprint(h.AWG["ib"])) == "" {
			h.AWG["ib"] = "chrome"
		}
	}

	jmin, _ := h.AWG["jmin"].(int)
	jmax, _ := h.AWG["jmax"].(int)
	if jmin > 0 && jmax > 0 && jmax < jmin {
		return fmt.Errorf("awg.jmax must be >= awg.jmin")
	}
	return nil
}

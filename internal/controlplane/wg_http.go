//go:build with_controlplane

package controlplane

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/paramvalidate"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/wgawg"
)

func (s *Service) wgPublicView(h domain.WgHub) map[string]any {
	h.Normalize()
	hubAddr, _ := h.HubAddress()
	out := map[string]any{
		"enabled":          h.Enabled,
		"profile":          h.Profile,
		"subnet":           h.Subnet,
		"listen_port":      h.ListenPort,
		"system":           h.System,
		"peer_relay":       h.PeerRelay,
		"internet_allow":   h.InternetAllowed(),
		"exit_user_id":     h.ExitUserID,
		"hub_address":      hubAddr,
		"hub_public_key":   h.HubPublicKey,
		"has_obfuscation":  h.HasObfuscation(),
		"has_awg":          h.HasObfuscation(), // compat alias
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

	switch h.Profile {
	case domain.WgProfileAWG2:
		if len(h.AWG2) > 0 {
			view := cloneAWGView(h.AWG2, false)
			out["awg2"] = view
			fillMasqueradeMeta(out, h.AWG2)
		}
	case domain.WgProfileAWG3:
		if len(h.AWG3) > 0 {
			view := cloneAWGView(h.AWG3, true)
			out["awg3"] = view
			fillMasqueradeMeta(out, h.AWG3)
		}
	case domain.WgProfilePathology:
		if len(h.Pathology) > 0 {
			out["pathology"] = clonePathologyView(h.Pathology)
		}
	}
	return out
}

func cloneAWGView(src map[string]any, awg3 bool) map[string]any {
	keys := []string{
		"jc", "jmin", "jmax",
		"s1", "s2", "s3", "s4",
		"h1", "h2", "h3", "h4",
		"id", "ip", "ib",
		"i1", "i2", "i3", "i4", "i5",
		"signature_protocol",
	}
	if awg3 {
		keys = append(keys,
			"header_protection_key", "content_padding_addition",
			"rekey_after_time", "rekey_timeout", "reject_after_time",
			"keepalive_timeout", "max_handshake_attempts",
		)
	}
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := src[k]; ok {
			out[k] = v
		}
	}
	return out
}

func clonePathologyView(src map[string]any) map[string]any {
	keys := []string{
		"enabled", "key", "auto", "persona", "pad_budget",
		"idle_persona", "pad_strategy", "start_cover", "start_gap_ms",
		"cover_interval_ms", "low_entropy", "frame", "frame_dcid_len",
		"start_decoy", "cipher", "preset", "rotate_sec", "intensity",
		"mode", "dialog",
	}
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := src[k]; ok {
			out[k] = v
		}
	}
	return out
}

func fillMasqueradeMeta(out map[string]any, block map[string]any) {
	manual := false
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
		if v, ok := block[k]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
			manual = true
			break
		}
	}
	if manual {
		out["manual_init"] = true
		out["masquerade_mode"] = "none"
	} else if ip, _ := block["ip"].(string); strings.TrimSpace(ip) != "" {
		out["masquerade_mode"] = strings.ToLower(strings.TrimSpace(ip))
	}
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
	if err := rejectFlatAWGBody(body); err != nil {
		failJSON(w, 400, "cp_invalid_wg", err.Error())
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
	forceObf := domain.NeedsObfuscation(h.Profile) && (prevProfile != h.Profile || !h.HasObfuscation())
	if h.Profile == domain.WgProfilePlain {
		h.ClearObfuscation()
	} else {
		if err := mergeWgObfuscation(&h, body); err != nil {
			if pe, ok := err.(*paramvalidate.Error); ok {
				failJSON(w, 400, pe.Code, pe.Error())
				return
			}
			failJSON(w, 400, "cp_invalid_wg", err.Error())
			return
		}
	}
	if _, err := s.ensureWgHubSecrets(&h, forceObf); err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	if h.Profile == domain.WgProfilePlain {
		h.ClearObfuscation()
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

func (s *Service) handleWgRegenerateObfuscation(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.LoadWgHub()
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	h.Normalize()

	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("masquerade")))
	}
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	profileOverride := ""
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
		if v, ok := body["profile"].(string); ok {
			profileOverride = strings.TrimSpace(v)
		}
	}
	// Client may send profile ahead of Apply, or mode=pathology while hub still has AWG.
	if profileOverride != "" {
		h.Profile = profileOverride
	} else if mode == "pathology" {
		h.Profile = domain.WgProfilePathology
	}
	h.Normalize()
	if err := h.Validate(); err != nil {
		failJSON(w, 400, "cp_invalid_wg", err.Error())
		return
	}
	if !domain.NeedsObfuscation(h.Profile) {
		failJSON(w, 400, "bad_request", "regenerate-obfuscation only for wg_awg2/wg_awg3/wg_pathology")
		return
	}

	switch h.Profile {
	case domain.WgProfilePathology:
		// Optional body.auto overrides preserved flag for this regenerate.
		prev := h.Pathology
		if body != nil {
			if v, ok := body["auto"].(bool); ok {
				if prev == nil {
					prev = map[string]any{}
				}
				prev["auto"] = v
			}
		}
		bundle, err := wgawg.RegeneratePathology(prev)
		if err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
		h.SetActiveObfuscation(bundle)
	default:
		// mode=pathology must not leak into AWG sugar preference.
		if mode == "pathology" {
			mode = ""
		}
		prev := h.ActiveObfuscation()
		preserveID := ""
		if prev != nil {
			preserveID = strings.TrimSpace(fmt.Sprint(prev["id"]))
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
		bundle, err := wgawg.BundleFromExisting(awg3, prev, mode)
		if err != nil {
			failJSON(w, 500, "internal", err.Error())
			return
		}
		if !wgawg.HasManualCPS(bundle) && preserveID != "" {
			bundle["id"] = preserveID
		}
		h.SetActiveObfuscation(bundle)
	}

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

func rejectFlatAWGBody(body map[string]any) error {
	if body == nil {
		return nil
	}
	if _, ok := body["awg"]; ok {
		return fmt.Errorf("legacy top-level awg rejected; use nested awg2|awg3|pathology")
	}
	for _, k := range []string{
		"jc", "jmin", "jmax", "s1", "s2", "s3", "s4",
		"h1", "h2", "h3", "h4", "id", "ip", "ib",
		"i1", "i2", "i3", "i4", "i5",
		"header_protection_key", "content_padding_addition",
		"rekey_after_time", "rekey_timeout", "reject_after_time",
		"keepalive_timeout", "max_handshake_attempts",
		"masquerade_mode", "masquerade_url", "manual_init",
	} {
		if _, ok := body[k]; ok {
			return fmt.Errorf("flat AWG key %q rejected; use nested awg2|awg3|pathology", k)
		}
	}
	return nil
}

// mergeWgObfuscation applies nested awg2/awg3/pathology overrides from PUT body.
func mergeWgObfuscation(h *domain.WgHub, body map[string]any) error {
	if h == nil || !domain.NeedsObfuscation(h.Profile) {
		return nil
	}
	switch h.Profile {
	case domain.WgProfileAWG2:
		if nested, ok := body["awg2"].(map[string]any); ok {
			dst := map[string]any{}
			for k, v := range h.AWG2 {
				dst[k] = v
			}
			if err := mergeAWGMap(dst, nested, false); err != nil {
				return err
			}
			h.AWG2 = dst
			h.AWG3, h.Pathology = nil, nil
		}
	case domain.WgProfileAWG3:
		if nested, ok := body["awg3"].(map[string]any); ok {
			dst := map[string]any{}
			for k, v := range h.AWG3 {
				dst[k] = v
			}
			if err := mergeAWGMap(dst, nested, true); err != nil {
				return err
			}
			h.AWG3 = dst
			h.AWG2, h.Pathology = nil, nil
		}
	case domain.WgProfilePathology:
		if nested, ok := body["pathology"].(map[string]any); ok {
			dst := map[string]any{}
			for k, v := range h.Pathology {
				dst[k] = v
			}
			if err := mergePathologyMap(dst, nested); err != nil {
				return err
			}
			if err := validatePathologyAgainstCatalog(dst); err != nil {
				return err
			}
			h.Pathology = dst
			h.AWG2, h.AWG3 = nil, nil
		}
	}
	return nil
}

func mergeAWGMap(dst, src map[string]any, awg3 bool) error {
	applyInt := func(key string, v any) error {
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
	applyString := func(key string, v any) {
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
		"signature_protocol": {},
	}
	if awg3 {
		stringKeys["header_protection_key"] = struct{}{}
		stringKeys["content_padding_addition"] = struct{}{}
		stringKeys["rekey_after_time"] = struct{}{}
		stringKeys["rekey_timeout"] = struct{}{}
		stringKeys["reject_after_time"] = struct{}{}
		stringKeys["keepalive_timeout"] = struct{}{}
		stringKeys["max_handshake_attempts"] = struct{}{}
	}
	for k, v := range src {
		k = strings.ToLower(strings.TrimSpace(k))
		if _, ok := intKeys[k]; ok {
			if err := applyInt(k, v); err != nil {
				return err
			}
			continue
		}
		if _, ok := stringKeys[k]; ok {
			applyString(k, v)
		}
	}
	manualCPS := false
	for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
		if v, ok := dst[k]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
			manualCPS = true
			break
		}
	}
	if manualCPS {
		for _, k := range []string{"id", "ip", "ib"} {
			delete(dst, k)
		}
	} else if ip, _ := dst["ip"].(string); strings.TrimSpace(ip) != "" {
		for _, k := range []string{"i1", "i2", "i3", "i4", "i5"} {
			delete(dst, k)
		}
		ip = strings.ToLower(strings.TrimSpace(ip))
		switch ip {
		case "quic", "dns", "stun", "sip":
			dst["ip"] = ip
		default:
			return fmt.Errorf("awg.ip must be one of quic|dns|stun|sip")
		}
		if _, ok := dst["ib"]; !ok || strings.TrimSpace(fmt.Sprint(dst["ib"])) == "" {
			dst["ib"] = "chrome"
		}
	}
	jmin, _ := dst["jmin"].(int)
	jmax, _ := dst["jmax"].(int)
	if jmin > 0 && jmax > 0 && jmax < jmin {
		return fmt.Errorf("awg.jmax must be >= awg.jmin")
	}
	// Broad envelopes around junk-ranges.seed.json defaults (operator may edit).
	awgRanges := map[string][2]int{
		"jc": {0, 128}, "jmin": {0, 2048}, "jmax": {0, 4096},
		"s1": {0, 512}, "s2": {0, 512}, "s3": {0, 512}, "s4": {0, 512},
	}
	for k, lim := range awgRanges {
		n, ok := dst[k].(int)
		if !ok {
			continue
		}
		if n < lim[0] || n > lim[1] {
			return fmt.Errorf("awg.%s out of range (%d..%d)", k, lim[0], lim[1])
		}
	}
	return nil
}

func mergePathologyMap(dst, src map[string]any) error {
	boolKeys := map[string]struct{}{"enabled": {}, "auto": {}, "low_entropy": {}}
	intKeys := map[string]struct{}{
		"pad_budget": {}, "start_cover": {}, "cover_interval_ms": {},
		"frame_dcid_len": {}, "rotate_sec": {}, "intensity": {},
	}
	stringKeys := map[string]struct{}{
		"key": {}, "persona": {}, "idle_persona": {}, "pad_strategy": {},
		"start_gap_ms": {}, "frame": {}, "start_decoy": {}, "cipher": {},
		"preset": {}, "mode": {}, "dialog": {},
	}
	for k, v := range src {
		k = strings.ToLower(strings.TrimSpace(k))
		if _, ok := boolKeys[k]; ok {
			switch b := v.(type) {
			case bool:
				dst[k] = b
			case string:
				s := strings.ToLower(strings.TrimSpace(b))
				switch s {
				case "true", "1", "yes":
					dst[k] = true
				case "false", "0", "no":
					dst[k] = false
				default:
					return fmt.Errorf("pathology.%s must be bool", k)
				}
			default:
				return fmt.Errorf("pathology.%s must be bool", k)
			}
			continue
		}
		if _, ok := intKeys[k]; ok {
			switch n := v.(type) {
			case float64:
				if n != float64(int(n)) {
					return fmt.Errorf("pathology.%s must be integer", k)
				}
				dst[k] = int(n)
			case int:
				dst[k] = n
			case string:
				s := strings.TrimSpace(n)
				if s == "" {
					delete(dst, k)
					continue
				}
				i, err := strconv.Atoi(s)
				if err != nil {
					return fmt.Errorf("pathology.%s must be integer", k)
				}
				dst[k] = i
			case nil:
				delete(dst, k)
			default:
				return fmt.Errorf("pathology.%s must be integer", k)
			}
			continue
		}
		if _, ok := stringKeys[k]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s == "" || s == "<nil>" {
				delete(dst, k)
			} else {
				dst[k] = s
			}
		}
	}
	if _, ok := dst["enabled"]; !ok {
		dst["enabled"] = true
	}
	if n, ok := dst["pad_budget"].(int); ok && (n < 0 || n > 255) {
		return fmt.Errorf("pathology.pad_budget out of range (0..255)")
	}
	if n, ok := dst["intensity"].(int); ok && (n < 1 || n > 5) {
		return fmt.Errorf("pathology.intensity out of range (1..5)")
	}
	return nil
}

var pathologyAdvancedKeys = []string{
	"persona", "pad_budget", "preset", "intensity", "frame", "cipher", "dialog",
	"idle_persona", "pad_strategy", "start_cover", "cover_interval_ms",
	"frame_dcid_len", "rotate_sec", "start_gap_ms", "start_decoy", "mode", "low_entropy",
}

func pathologyMapAsStrings(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch t := v.(type) {
		case bool:
			if t {
				out[k] = "true"
			} else {
				out[k] = "false"
			}
		case int:
			out[k] = strconv.Itoa(t)
		case float64:
			if t == float64(int(t)) {
				out[k] = strconv.Itoa(int(t))
			} else {
				out[k] = fmt.Sprint(t)
			}
		default:
			s := strings.TrimSpace(fmt.Sprint(v))
			if s == "" || s == "<nil>" {
				continue
			}
			out[k] = s
		}
	}
	return out
}

// validatePathologyAgainstCatalog checks nested pathology knobs via wg_pathology ParamMeta.
func validatePathologyAgainstCatalog(m map[string]any) error {
	if m == nil {
		return nil
	}
	// When auto=true, advanced knobs are not applicable — drop them before schema check.
	if wgawg.PathologyAuto(m) {
		for _, k := range pathologyAdvancedKeys {
			delete(m, k)
		}
	}
	inv, err := presets.GetInvariant(domain.WgProfilePathology)
	if err != nil {
		return nil // catalog missing — merge-level checks already ran
	}
	pp := inv.ToProtocolPreset("en")
	params := pathologyMapAsStrings(m)
	// mtu is a hub field, not a pathology nested key.
	delete(params, "mtu")
	if err := paramvalidate.Validate(pp, params); err != nil {
		if pe, ok := err.(*paramvalidate.Error); ok {
			return pe
		}
		return err
	}
	return nil
}

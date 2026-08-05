//go:build with_controlplane

package smoke

import (
	"encoding/json"
	"fmt"
	"strings"
)

// HairpinLocalHost is the loopback address used for self-test dials.
const HairpinLocalHost = "127.0.0.1"

// WgSmokeSetName / WgSmokePreset identify the synthetic WG hub row in Active proxies + smoke.
const (
	WgSmokeSetName = "wg"
	WgSmokePreset  = "wireguard"
	WgSmokeTag     = "cp-wg"
)

// RewriteServersToHairpin sets outbound/endpoint server fields to 127.0.0.1
// while leaving tls.server_name / Reality / SNI intact.
func RewriteServersToHairpin(outbounds []any) error {
	for i, raw := range outbounds {
		ob, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rewriteDialTargetsToHairpin(ob)
		outbounds[i] = ob
	}
	return nil
}

func rewriteDialTargetsToHairpin(ob map[string]any) {
	if _, has := ob["server"]; has {
		ob["server"] = HairpinLocalHost
	}
	// WireGuard client peers use address+port (not top-level server).
	if peers, ok := ob["peers"].([]any); ok {
		for i, p := range peers {
			pm, ok := p.(map[string]any)
			if !ok || pm == nil {
				continue
			}
			if _, has := pm["address"]; has {
				pm["address"] = HairpinLocalHost
			}
			if _, has := pm["server"]; has {
				pm["server"] = HairpinLocalHost
			}
			peers[i] = pm
		}
		ob["peers"] = peers
	}
	// DERP uses host for WS/HTTP Host; keep public host for SNI/Host, dial via server.
	// Hairpin against self-signed / missing ACME must skip verify.
	if typ, _ := ob["type"].(string); strings.EqualFold(typ, "derp") {
		if tlsObj, ok := ob["tls"].(map[string]any); ok && tlsObj != nil {
			tlsObj["insecure"] = true
			ob["tls"] = tlsObj
		}
	}
}

// CloneOutbounds deep-copies outbound maps via JSON.
func CloneOutbounds(in []any) ([]any, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	var out []any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ExtractOutbounds parses RenderSubscription JSON body outbounds.
func ExtractOutbounds(subJSON []byte) ([]any, error) {
	var doc struct {
		Outbounds []any `json:"outbounds"`
	}
	if err := json.Unmarshal(subJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse subscription: %w", err)
	}
	return doc.Outbounds, nil
}

// ExtractEndpoints parses RenderSubscription JSON body endpoints (e.g. WireGuard).
func ExtractEndpoints(subJSON []byte) ([]any, error) {
	var doc struct {
		Endpoints []any `json:"endpoints"`
	}
	if err := json.Unmarshal(subJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse subscription endpoints: %w", err)
	}
	return doc.Endpoints, nil
}

func outboundTag(ob map[string]any) string {
	return fmt.Sprint(ob["tag"])
}

func isWireGuardType(ob map[string]any) bool {
	typ, _ := ob["type"].(string)
	return strings.EqualFold(strings.TrimSpace(typ), "wireguard")
}

//go:build with_controlplane

package smoke

import (
	"encoding/json"
	"fmt"
)

// HairpinLocalHost is the loopback address used for self-test dials.
const HairpinLocalHost = "127.0.0.1"

// RewriteServersToHairpin sets outbound/endpoint server fields to 127.0.0.1
// while leaving tls.server_name / Reality / SNI intact.
func RewriteServersToHairpin(outbounds []any) error {
	for i, raw := range outbounds {
		ob, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, has := ob["server"]; has {
			ob["server"] = HairpinLocalHost
		}
		// Carrier / nested link.peer may embed host:port — leave as-is (skipped protocols).
		outbounds[i] = ob
	}
	return nil
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

// ExtractOutbounds parses RenderSubscription JSON body.
func ExtractOutbounds(subJSON []byte) ([]any, error) {
	var doc struct {
		Outbounds []any `json:"outbounds"`
	}
	if err := json.Unmarshal(subJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse subscription: %w", err)
	}
	return doc.Outbounds, nil
}

func outboundTag(ob map[string]any) string {
	return fmt.Sprint(ob["tag"])
}

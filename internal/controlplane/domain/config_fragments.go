//go:build with_controlplane

package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

var leftoverFragmentToken = regexp.MustCompile(`\{\{[^{}]+\}\}`)

// ConfigFragments holds raw sing-box dns/route/outbounds JSON for materialize.
type ConfigFragments struct {
	DNS       json.RawMessage `json:"dns,omitempty"`
	Route     json.RawMessage `json:"route,omitempty"`
	Outbounds json.RawMessage `json:"outbounds,omitempty"`
}

// DefaultDNSFragment is the minimal dns block.
func DefaultDNSFragment() json.RawMessage {
	return json.RawMessage(`{"servers":[{"tag":"local","type":"local"}]}`)
}

// DefaultRouteFragment is the minimal route block.
func DefaultRouteFragment() json.RawMessage {
	return json.RawMessage(`{"final":"direct","rules":[]}`)
}

// DefaultOutboundsFragment is the minimal outbounds array.
func DefaultOutboundsFragment() json.RawMessage {
	return json.RawMessage(`[{"type":"direct","tag":"direct"},{"type":"block","tag":"block"}]`)
}

// EffectiveDNS returns override or default.
func (f ConfigFragments) EffectiveDNS() json.RawMessage {
	if len(bytes.TrimSpace(f.DNS)) == 0 {
		return DefaultDNSFragment()
	}
	return f.DNS
}

// EffectiveRoute returns override or default.
func (f ConfigFragments) EffectiveRoute() json.RawMessage {
	if len(bytes.TrimSpace(f.Route)) == 0 {
		return DefaultRouteFragment()
	}
	return f.Route
}

// EffectiveOutbounds returns override or default.
func (f ConfigFragments) EffectiveOutbounds() json.RawMessage {
	if len(bytes.TrimSpace(f.Outbounds)) == 0 {
		return DefaultOutboundsFragment()
	}
	return f.Outbounds
}

// DNSIsDefault reports whether dns override is unset.
func (f ConfigFragments) DNSIsDefault() bool {
	return len(bytes.TrimSpace(f.DNS)) == 0
}

// RouteIsDefault reports whether route override is unset.
func (f ConfigFragments) RouteIsDefault() bool {
	return len(bytes.TrimSpace(f.Route)) == 0
}

// OutboundsIsDefault reports whether outbounds override is unset.
func (f ConfigFragments) OutboundsIsDefault() bool {
	return len(bytes.TrimSpace(f.Outbounds)) == 0
}

// ValidateDNSFragment checks a raw dns JSON object.
func ValidateDNSFragment(raw json.RawMessage) error {
	return validateFragmentObject(raw, "dns", true)
}

// ValidateRouteFragment checks a raw route JSON object.
func ValidateRouteFragment(raw json.RawMessage) error {
	if err := validateFragmentObject(raw, "route", false); err != nil {
		return err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("route: %w", err)
	}
	final, ok := obj["final"]
	if !ok {
		return fmt.Errorf("route: final required")
	}
	s, ok := final.(string)
	if !ok || s == "" {
		return fmt.Errorf("route: final must be a non-empty string")
	}
	return nil
}

// ValidateOutboundsFragment checks a raw outbounds JSON array.
func ValidateOutboundsFragment(raw json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return fmt.Errorf("outbounds: empty json")
	}
	if leftoverFragmentToken.Match(raw) {
		return fmt.Errorf("outbounds: unresolved template tokens")
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return fmt.Errorf("outbounds: must be a json array: %w", err)
	}
	if arr == nil {
		return fmt.Errorf("outbounds: must be a json array")
	}
	if len(arr) == 0 {
		return fmt.Errorf("outbounds: must be non-empty")
	}
	seen := make(map[string]struct{}, len(arr))
	for i, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok || obj == nil {
			return fmt.Errorf("outbounds[%d]: must be a json object", i)
		}
		typ, _ := obj["type"].(string)
		if typ == "" {
			return fmt.Errorf("outbounds[%d]: type required", i)
		}
		tag, _ := obj["tag"].(string)
		if tag == "" {
			return fmt.Errorf("outbounds[%d]: tag required", i)
		}
		if _, dup := seen[tag]; dup {
			return fmt.Errorf("outbounds: duplicate tag %q", tag)
		}
		seen[tag] = struct{}{}
	}
	return nil
}

func validateFragmentObject(raw json.RawMessage, name string, requireServers bool) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return fmt.Errorf("%s: empty json", name)
	}
	if leftoverFragmentToken.Match(raw) {
		return fmt.Errorf("%s: unresolved template tokens", name)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if obj == nil {
		return fmt.Errorf("%s: must be a json object", name)
	}
	if requireServers {
		servers, ok := obj["servers"]
		if !ok {
			return fmt.Errorf("%s: servers required", name)
		}
		arr, ok := servers.([]any)
		if !ok {
			return fmt.Errorf("%s: servers must be an array", name)
		}
		if len(arr) == 0 {
			return fmt.Errorf("%s: servers must be non-empty", name)
		}
	}
	return nil
}

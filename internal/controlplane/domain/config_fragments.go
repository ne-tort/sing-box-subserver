//go:build with_controlplane

package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

var leftoverFragmentToken = regexp.MustCompile(`\{\{[^{}]+\}\}`)

// ConfigFragments holds raw sing-box dns/route JSON objects for materialize.
type ConfigFragments struct {
	DNS   json.RawMessage `json:"dns,omitempty"`
	Route json.RawMessage `json:"route,omitempty"`
}

// DefaultDNSFragment is the minimal dns block.
func DefaultDNSFragment() json.RawMessage {
	return json.RawMessage(`{"servers":[{"tag":"local","type":"local"}]}`)
}

// DefaultRouteFragment is the minimal route block.
func DefaultRouteFragment() json.RawMessage {
	return json.RawMessage(`{"final":"direct","rules":[]}`)
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

// ValidateDNSFragment checks a raw dns JSON object.
func ValidateDNSFragment(raw json.RawMessage) error {
	return validateFragmentObject(raw, "dns", true)
}

// ValidateRouteFragment checks a raw route JSON object.
func ValidateRouteFragment(raw json.RawMessage) error {
	return validateFragmentObject(raw, "route", false)
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

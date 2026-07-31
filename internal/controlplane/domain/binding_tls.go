package domain

import "strings"

// BindingTLSMode returns tls_mode for custom constructors that declare it in ParamMeta.
// controlled=false means the preset uses static traits (stock Reality/TLS).
func BindingTLSMode(p ProtocolPreset, params map[string]string) (mode string, controlled bool) {
	if !p.CustomPreset {
		return "", false
	}
	meta, ok := p.ParamMeta["tls_mode"]
	if !ok {
		return "", false
	}
	mode = strings.ToLower(strings.TrimSpace(params["tls_mode"]))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(meta.Default))
	}
	if mode == "" {
		mode = "tls"
	}
	return mode, true
}

// BindingUsesReality reports whether this binding must receive a Reality assignment.
func BindingUsesReality(p ProtocolPreset, params map[string]string) bool {
	if mode, ok := BindingTLSMode(p, params); ok {
		return mode == "reality"
	}
	for _, t := range p.Traits {
		if t == "reality" {
			return true
		}
	}
	return false
}

// BindingNeedsPEMTLS reports whether inbound/outbound should attach PEM/ACME TLS
// (not Reality, not plain).
func BindingNeedsPEMTLS(p ProtocolPreset, params map[string]string) bool {
	if mode, ok := BindingTLSMode(p, params); ok {
		return mode == "tls"
	}
	for _, t := range p.Traits {
		if t == "tls" || t == "tls_custom" {
			return true
		}
	}
	return false
}

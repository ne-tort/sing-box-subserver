//go:build with_controlplane

package materialize

import (
	"strings"
)

// applyTLSHandshakeKnobs merges optional TLS handshake fields onto a tls object.
// Keys: tls_alpn|alpn, tls_min_version|min_version, tls_max_version|max_version,
// tls_cipher_suites|cipher_suites, tls_curve_preferences|curve_preferences.
func applyTLSHandshakeKnobs(tlsObj map[string]any, params map[string]string) {
	if tlsObj == nil || params == nil {
		return
	}
	alpn := strings.TrimSpace(params["tls_alpn"])
	if alpn == "" {
		alpn = strings.TrimSpace(params["alpn"])
	}
	if alpn != "" {
		tlsObj["alpn"] = splitCSVAny(alpn)
	}
	if v := firstNonEmpty(params["tls_min_version"], params["min_version"]); v != "" {
		tlsObj["min_version"] = v
	}
	if v := firstNonEmpty(params["tls_max_version"], params["max_version"]); v != "" {
		tlsObj["max_version"] = v
	}
	if v := firstNonEmpty(params["tls_cipher_suites"], params["cipher_suites"]); v != "" {
		tlsObj["cipher_suites"] = splitCSVAny(v)
	}
	if v := firstNonEmpty(params["tls_curve_preferences"], params["curve_preferences"]); v != "" {
		tlsObj["curve_preferences"] = splitCSVAny(v)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func splitCSVAny(raw string) []any {
	parts := strings.Split(raw, ",")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// applyInboundECH attaches tls.ech when material is present.
func applyInboundECH(tlsObj map[string]any, keyPath string) {
	if tlsObj == nil || strings.TrimSpace(keyPath) == "" {
		return
	}
	tlsObj["ech"] = map[string]any{
		"enabled":  true,
		"key_path": keyPath,
	}
}

// applyOutboundECH attaches client ECH config lines for subscription.
func applyOutboundECH(tlsObj map[string]any, configPEM string) {
	if tlsObj == nil {
		return
	}
	configPEM = strings.TrimSpace(configPEM)
	if configPEM == "" {
		return
	}
	lines := strings.Split(configPEM, "\n")
	arr := make([]any, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		if ln != "" {
			arr = append(arr, ln)
		}
	}
	if len(arr) == 0 {
		return
	}
	tlsObj["ech"] = map[string]any{
		"enabled": true,
		"config":  arr,
	}
}

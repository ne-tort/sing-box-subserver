//go:build with_controlplane

package domain

import "strings"

// EffectiveLeafServerName resolves inbound tls.server_name for non-Reality TLS.
// Prefer SSL profile ServerName via materialize; this helper falls back to demux_sni / host.
func EffectiveLeafServerName(params map[string]string, fallback string) string {
	if params != nil {
		if s := strings.ToLower(strings.TrimSpace(params["demux_sni"])); s != "" {
			return s
		}
	}
	return strings.ToLower(strings.TrimSpace(fallback))
}

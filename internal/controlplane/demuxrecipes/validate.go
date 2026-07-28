//go:build with_controlplane

package demuxrecipes

import (
	"fmt"
	"strings"
)

// ValidateTemplate checks demux_template authoring rules before Apply.
// Empty match objects (e.g. {"tls":{}}) are rejected — they become sing-box "empty match".
func ValidateTemplate(tmpl map[string]any) error {
	if len(tmpl) == 0 {
		return fmt.Errorf("demux_template empty")
	}
	rulesRaw, ok := tmpl["rules"]
	if !ok {
		return fmt.Errorf("demux_template.rules required")
	}
	rules, ok := rulesRaw.([]any)
	if !ok {
		// allow []map from typed JSON into map[string]any via encoding/json
		switch typed := rulesRaw.(type) {
		case []map[string]any:
			rules = make([]any, len(typed))
			for i := range typed {
				rules[i] = typed[i]
			}
		default:
			return fmt.Errorf("demux_template.rules must be an array")
		}
	}
	if len(rules) == 0 {
		return fmt.Errorf("demux_template.rules must be non-empty")
	}
	for i, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("demux_template.rules[%d] must be an object", i)
		}
		matchRaw, ok := rule["match"]
		if !ok {
			return fmt.Errorf("demux_template.rules[%d].match required", i)
		}
		match, ok := matchRaw.(map[string]any)
		if !ok {
			return fmt.Errorf("demux_template.rules[%d].match must be an object", i)
		}
		if err := validateMatch(match, i); err != nil {
			return err
		}
		if _, ok := rule["action"]; !ok {
			return fmt.Errorf("demux_template.rules[%d].action required", i)
		}
	}
	return nil
}

func validateMatch(match map[string]any, ruleIdx int) error {
	if len(match) == 0 {
		return fmt.Errorf("demux_template.rules[%d].match is empty (use protocol/always/tls.sni)", ruleIdx)
	}
	if always, ok := match["always"]; ok {
		if b, ok := always.(bool); ok && b {
			return nil
		}
		return fmt.Errorf("demux_template.rules[%d].match.always must be true when set", ruleIdx)
	}
	if proto, ok := match["protocol"]; ok {
		s, ok := proto.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return fmt.Errorf("demux_template.rules[%d].match.protocol must be a non-empty string", ruleIdx)
		}
		return nil
	}
	if tlsRaw, ok := match["tls"]; ok {
		tlsObj, ok := tlsRaw.(map[string]any)
		if !ok {
			return fmt.Errorf("demux_template.rules[%d].match.tls must be an object", ruleIdx)
		}
		if len(tlsObj) == 0 {
			return fmt.Errorf("demux_template.rules[%d].match.tls is empty (use protocol:\"tls\" or tls.sni)", ruleIdx)
		}
		if sni, ok := tlsObj["sni"]; ok {
			arr, ok := sni.([]any)
			if !ok || len(arr) == 0 {
				return fmt.Errorf("demux_template.rules[%d].match.tls.sni must be a non-empty array", ruleIdx)
			}
			return nil
		}
		// other tls keys (alpn, etc.) — accept if non-empty object
		return nil
	}
	if quicRaw, ok := match["quic"]; ok {
		quicObj, ok := quicRaw.(map[string]any)
		if !ok {
			return fmt.Errorf("demux_template.rules[%d].match.quic must be an object", ruleIdx)
		}
		if len(quicObj) == 0 {
			return fmt.Errorf("demux_template.rules[%d].match.quic is empty (use protocol:\"quic\")", ruleIdx)
		}
		return nil
	}
	// Other match keys (http, ssh, …) — require at least one non-empty nested value.
	for k, v := range match {
		if nested, ok := v.(map[string]any); ok && len(nested) == 0 {
			return fmt.Errorf("demux_template.rules[%d].match.%s is empty", ruleIdx, k)
		}
	}
	return nil
}

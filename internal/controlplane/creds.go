//go:build with_controlplane

package controlplane

import (
	"fmt"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

// validateCreds checks operator-supplied credential maps against the preset catalog.
func validateCreds(creds map[string]map[string]any) error {
	if creds == nil {
		return nil
	}
	for presetName, fields := range creds {
		p, err := presets.Get(presetName)
		if err != nil {
			return fmt.Errorf("cp_invalid_creds: unknown preset %q", presetName)
		}
		if fields == nil {
			return fmt.Errorf("cp_invalid_creds: preset %q creds must be an object", presetName)
		}
		allowed := make(map[string]struct{}, len(p.CredFields))
		for _, f := range p.CredFields {
			allowed[f] = struct{}{}
		}
		for k, v := range fields {
			if _, ok := allowed[k]; !ok {
				return fmt.Errorf("cp_invalid_creds: preset %q unexpected field %q", presetName, k)
			}
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("cp_invalid_creds: preset %q field %q must be a string", presetName, k)
			}
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("cp_invalid_creds: preset %q field %q must be non-empty", presetName, k)
			}
		}
	}
	return nil
}

// mergeUserCreds merges operator-supplied preset field maps into u.Creds.
// For each supplied preset, fields overwrite existing keys; unspecified fields are kept.
func mergeUserCreds(u *domain.User, creds map[string]map[string]any) {
	if u == nil || len(creds) == 0 {
		return
	}
	if u.Creds == nil {
		u.Creds = map[string]map[string]any{}
	}
	for presetName, fields := range creds {
		if fields == nil {
			continue
		}
		dst := u.Creds[presetName]
		if dst == nil {
			dst = map[string]any{}
		}
		for k, v := range fields {
			dst[k] = v
		}
		u.Creds[presetName] = dst
	}
}

func credFieldEmpty(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	if !ok {
		return true
	}
	return strings.TrimSpace(s) == ""
}

func generateCredField(field string) (any, error) {
	if field == "uuid" || strings.HasPrefix(field, "uuid") {
		tok, err := randomToken()
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("%s-%s-%s-%s-%s", tok[0:8], tok[8:12], tok[12:16], tok[16:20], tok[20:32]), nil
	}
	return randomPassword()
}

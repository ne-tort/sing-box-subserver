//go:build with_controlplane

package paramvalidate

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

// Error is a machine-readable params validation failure.
type Error struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Field)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Validate checks binding params against preset ParamMeta / ParamFields.
func Validate(pp domain.ProtocolPreset, params map[string]string) error {
	if params == nil {
		params = map[string]string{}
	}
	// Required
	for _, f := range pp.ParamFields {
		if f == "" {
			continue
		}
		req := true
		if m, ok := pp.ParamMeta[f]; ok && m.Required != nil {
			req = *m.Required
		}
		if !req {
			continue
		}
		if !visible(pp, f, params) {
			continue
		}
		if strings.TrimSpace(params[f]) == "" {
			return &Error{Code: "cp_param_required", Field: f, Message: "required parameter missing"}
		}
	}
	for f, meta := range pp.ParamMeta {
		if meta.Required != nil && *meta.Required {
			if !visible(pp, f, params) {
				continue
			}
			if strings.TrimSpace(params[f]) == "" {
				return &Error{Code: "cp_param_required", Field: f, Message: "required parameter missing"}
			}
		}
	}

	for f, raw := range params {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		meta, ok := pp.ParamMeta[f]
		if !ok {
			continue
		}
		if !visible(pp, f, params) {
			continue
		}
		typ := meta.Type
		if typ == "" {
			typ = "string"
		}
		switch typ {
		case "bool":
			if _, err := strconv.ParseBool(raw); err != nil && raw != "0" && raw != "1" {
				return &Error{Code: "cp_param_type", Field: f, Message: "expected bool"}
			}
		case "uint16":
			n, err := strconv.ParseUint(raw, 10, 16)
			if err != nil {
				return &Error{Code: "cp_param_type", Field: f, Message: "expected uint16"}
			}
			if meta.Min != nil && float64(n) < *meta.Min {
				return &Error{Code: "cp_param_range", Field: f, Message: "below min"}
			}
			if meta.Max != nil && float64(n) > *meta.Max {
				return &Error{Code: "cp_param_range", Field: f, Message: "above max"}
			}
		case "enum":
			if len(meta.Enum) > 0 && !contains(meta.Enum, raw) {
				return &Error{Code: "cp_param_enum", Field: f, Message: "value not in enum"}
			}
		}
		if len(meta.Enum) > 0 && typ != "enum" && !contains(meta.Enum, raw) {
			return &Error{Code: "cp_param_enum", Field: f, Message: "value not in enum"}
		}
		for _, other := range meta.ConflictsWith {
			if strings.TrimSpace(params[other]) != "" {
				return &Error{Code: "cp_param_conflict", Field: f, Message: "conflicts with " + other}
			}
		}
		for _, need := range meta.Requires {
			if strings.TrimSpace(params[need]) == "" {
				return &Error{Code: "cp_param_requires", Field: f, Message: "requires " + need}
			}
		}
	}
	return nil
}

func visible(pp domain.ProtocolPreset, field string, params map[string]string) bool {
	meta, ok := pp.ParamMeta[field]
	if !ok || len(meta.VisibleWhen) == 0 {
		return true
	}
	for _, c := range meta.VisibleWhen {
		v := strings.TrimSpace(params[c.Key])
		if c.NotEmpty && v == "" {
			return false
		}
		if c.Equals != "" && v != c.Equals {
			return false
		}
		if len(c.In) > 0 && !contains(c.In, v) {
			return false
		}
	}
	return true
}

func contains(list []string, want string) bool {
	for _, x := range list {
		if x == want {
			return true
		}
	}
	return false
}

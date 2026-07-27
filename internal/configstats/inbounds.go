package configstats

import (
	"encoding/json"

	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

// CountInbounds returns len(inbounds) from last-good server JSON, or 0.
func CountInbounds(sup *supervisor.Supervisor) int {
	if sup == nil {
		return 0
	}
	raw, _, err := sup.LastGoodConfig()
	if err != nil || len(raw) == 0 {
		return 0
	}
	var doc struct {
		Inbounds []json.RawMessage `json:"inbounds"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0
	}
	return len(doc.Inbounds)
}

//go:build with_controlplane

package demuxgroups

import (
	"encoding/json"
	"fmt"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/catalogsqlite"
)

func loadFromCatalog() ([]Group, error) {
	conn, err := catalogsqlite.DB()
	if err != nil {
		return nil, err
	}
	rows, err := conn.Query(`
SELECT tag,short_name,status,suggested_port,networks_json,scores_json,i18n_json,notes
FROM demux_groups ORDER BY tag`)
	if err != nil {
		return nil, err
	}
	type rawGroup struct {
		g        Group
		networks string
		scores   string
		i18n     string
	}
	var raws []rawGroup
	for rows.Next() {
		var rg rawGroup
		var port int
		if err := rows.Scan(&rg.g.Tag, &rg.g.ShortName, &rg.g.Status, &port, &rg.networks, &rg.scores, &rg.i18n, &rg.g.Notes); err != nil {
			_ = rows.Close()
			return nil, err
		}
		rg.g.SuggestedPort = uint16(port)
		raws = append(raws, rg)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	out := make([]Group, 0, len(raws))
	for _, rg := range raws {
		g := rg.g
		_ = json.Unmarshal([]byte(rg.networks), &g.Networks)
		_ = json.Unmarshal([]byte(rg.scores), &g.Scores)
		_ = json.Unmarshal([]byte(rg.i18n), &g.I18n)
		slots, err := loadSlots(g.Tag)
		if err != nil {
			return nil, err
		}
		g.Slots = slots
		out = append(out, g)
	}
	return out, nil
}

func loadSlots(groupTag string) ([]Slot, error) {
	conn, err := catalogsqlite.DB()
	if err != nil {
		return nil, err
	}
	rows, err := conn.Query(`
SELECT id,role,default_preset,substitutes_json,match_hint,preferred_alpn_json
FROM demux_slots WHERE group_tag = ? ORDER BY slot_order, id`, groupTag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Slot
	for rows.Next() {
		var s Slot
		var role, subs, alpn string
		if err := rows.Scan(&s.ID, &role, &s.DefaultPreset, &subs, &s.MatchHint, &alpn); err != nil {
			return nil, err
		}
		s.Role = Role(role)
		_ = json.Unmarshal([]byte(subs), &s.Substitutes)
		_ = json.Unmarshal([]byte(alpn), &s.PreferredALPN)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ValidateGroupShape rejects groups that demux cannot route (e.g. >1 protocol_only QUIC).
func ValidateGroupShape(g Group) error {
	var protocolOnlyQUIC int
	for _, s := range g.Slots {
		if s.Role == RoleQUIC && s.MatchHint == "protocol_only" {
			protocolOnlyQUIC++
		}
	}
	if protocolOnlyQUIC > 1 {
		return fmt.Errorf("demux group %q: at most one protocol_only QUIC slot allowed (got %d)", g.Tag, protocolOnlyQUIC)
	}
	seen := map[string]string{}
	for _, s := range g.Slots {
		p := s.DefaultPreset
		if prev, ok := seen[p]; ok {
			return fmt.Errorf("demux group %q: duplicate default preset %q on slots %q and %q", g.Tag, p, prev, s.ID)
		}
		seen[p] = s.ID
	}
	return nil
}

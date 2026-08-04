//go:build with_controlplane

package catalogsqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// demuxGroupSeed is the JSON shape under ref/demux/*.json (matches demuxgroups.Group).
type demuxGroupSeed struct {
	Tag           string           `json:"tag"`
	ShortName     string           `json:"short_name"`
	Status        string           `json:"status"`
	SuggestedPort uint16           `json:"suggested_port,omitempty"`
	Networks      []string         `json:"networks"`
	I18n          map[string]struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"i18n"`
	Slots []struct {
		ID            string   `json:"id"`
		Role          string   `json:"role"`
		DefaultPreset string   `json:"default_preset"`
		Substitutes   []string `json:"substitutes"`
		MatchHint     string   `json:"match_hint,omitempty"`
		PreferredALPN []string `json:"preferred_alpn,omitempty"`
	} `json:"slots"`
	Scores map[string]int `json:"scores,omitempty"`
	Notes  string         `json:"notes,omitempty"`
}

func importDemuxRefs(conn *sql.DB) (int, error) {
	dir := "ref/demux"
	entries, err := fs.ReadDir(catalogRefFS, dir)
	if err != nil {
		return 0, fmt.Errorf("list demux ref: %w", err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := fs.ReadFile(catalogRefFS, path.Join(dir, e.Name()))
		if err != nil {
			return n, err
		}
		var g demuxGroupSeed
		if err := json.Unmarshal(raw, &g); err != nil {
			return n, fmt.Errorf("demux %s: %w", e.Name(), err)
		}
		if g.Tag == "" || len(g.Slots) == 0 {
			return n, fmt.Errorf("demux %s: incomplete", e.Name())
		}
		if err := insertDemuxGroup(conn, g); err != nil {
			return n, fmt.Errorf("demux %s: %w", g.Tag, err)
		}
		n++
	}
	return n, nil
}

func insertDemuxGroup(conn *sql.DB, g demuxGroupSeed) error {
	networks, err := json.Marshal(g.Networks)
	if err != nil {
		return err
	}
	scores, err := json.Marshal(g.Scores)
	if err != nil {
		return err
	}
	i18n, err := json.Marshal(g.I18n)
	if err != nil {
		return err
	}
	_, err = conn.Exec(`
INSERT INTO demux_groups(tag,short_name,status,suggested_port,networks_json,scores_json,i18n_json,notes)
VALUES(?,?,?,?,?,?,?,?)`,
		g.Tag, g.ShortName, g.Status, int(g.SuggestedPort),
		string(networks), string(scores), string(i18n), g.Notes)
	if err != nil {
		return err
	}
	for i, s := range g.Slots {
		subs, err := json.Marshal(s.Substitutes)
		if err != nil {
			return err
		}
		alpn, err := json.Marshal(s.PreferredALPN)
		if err != nil {
			return err
		}
		if s.ID == "" || s.DefaultPreset == "" {
			return fmt.Errorf("slot #%d incomplete", i)
		}
		// Validate preset ownership at seed time.
		if !aliasExists(conn, s.DefaultPreset) {
			return fmt.Errorf("slot %s: unknown default_preset %q", s.ID, s.DefaultPreset)
		}
		for _, sub := range s.Substitutes {
			if !aliasExists(conn, sub) {
				return fmt.Errorf("slot %s: unknown substitute %q", s.ID, sub)
			}
		}
		_, err = conn.Exec(`
INSERT INTO demux_slots(group_tag,id,role,default_preset,substitutes_json,match_hint,preferred_alpn_json,slot_order)
VALUES(?,?,?,?,?,?,?,?)`,
			g.Tag, s.ID, s.Role, s.DefaultPreset, string(subs), s.MatchHint, string(alpn), i)
		if err != nil {
			return err
		}
	}
	return nil
}

func aliasExists(conn *sql.DB, tag string) bool {
	var n int
	_ = conn.QueryRow(`SELECT COUNT(1) FROM aliases WHERE alias = ?`, tag).Scan(&n)
	return n > 0
}

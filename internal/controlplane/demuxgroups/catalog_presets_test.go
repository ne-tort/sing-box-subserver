//go:build with_controlplane

package demuxgroups

import (
	"strings"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/catalogsqlite"
)

func TestDemuxCatalogPresetRefsExist(t *testing.T) {
	t.Parallel()
	seen := map[string]struct{}{}
	for _, g := range All() {
		if err := ValidateGroupShape(g); err != nil {
			t.Errorf("%v", err)
		}
		for _, slot := range g.Slots {
			check := func(tag string) {
				if tag == "" {
					return
				}
				if _, ok := seen[tag]; ok {
					return
				}
				seen[tag] = struct{}{}
				if !catalogsqlite.Owns(tag) {
					t.Errorf("group %s slot %s: preset %q not in catalogsqlite", g.Tag, slot.ID, tag)
					return
				}
				proto, ok := catalogsqlite.ProtocolOf(tag)
				if !ok || proto == "" {
					t.Errorf("group %s slot %s: preset %q missing protocol", g.Tag, slot.ID, tag)
				}
			}
			check(slot.DefaultPreset)
			for _, s := range slot.Substitutes {
				check(s)
			}
		}
	}
}

func TestDemuxGroupsSeedParity(t *testing.T) {
	t.Parallel()
	fromSQL := All()
	if len(fromSQL) != 10 {
		t.Fatalf("sqlite demux groups=%d want 10", len(fromSQL))
	}
	builtin := BuiltinGroups()
	if len(builtin) != len(fromSQL) {
		t.Fatalf("builtin=%d sqlite=%d", len(builtin), len(fromSQL))
	}
	byTag := map[string]Group{}
	for _, g := range fromSQL {
		byTag[g.Tag] = g
	}
	for _, g := range builtin {
		got, ok := byTag[g.Tag]
		if !ok {
			t.Fatalf("sqlite missing group %s", g.Tag)
		}
		if len(got.Slots) != len(g.Slots) {
			t.Fatalf("%s slots sqlite=%d builtin=%d", g.Tag, len(got.Slots), len(g.Slots))
		}
		for i := range g.Slots {
			if got.Slots[i].ID != g.Slots[i].ID || got.Slots[i].DefaultPreset != g.Slots[i].DefaultPreset {
				t.Fatalf("%s slot[%d] mismatch sqlite=%+v builtin=%+v", g.Tag, i, got.Slots[i], g.Slots[i])
			}
		}
	}
}

func TestNoMultipleProtocolOnlyQUIC(t *testing.T) {
	t.Parallel()
	for _, g := range All() {
		n := 0
		for _, s := range g.Slots {
			if s.Role == RoleQUIC && s.MatchHint == "protocol_only" {
				n++
			}
		}
		if n > 1 {
			t.Fatalf("%s: %d protocol_only QUIC slots", g.Tag, n)
		}
	}
}

func TestStableDefaultsNotDemuxLab(t *testing.T) {
	t.Parallel()
	for _, g := range All() {
		if !strings.EqualFold(g.Status, "stable") {
			continue
		}
		for _, slot := range g.Slots {
			compat := demuxCompatForPreset(slot.DefaultPreset, slot.Role, slot.MatchHint)
			if compat == "demux_lab" {
				t.Errorf("stable group %s slot %s default %q is demux_lab", g.Tag, slot.ID, slot.DefaultPreset)
			}
		}
	}
}

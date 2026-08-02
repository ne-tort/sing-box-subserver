//go:build with_controlplane

package demuxgroups

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/presets"
)

func TestDemuxCatalogPresetRefsExist(t *testing.T) {
	t.Parallel()
	seen := map[string]struct{}{}
	for _, g := range All() {
		for _, slot := range g.Slots {
			check := func(tag string) {
				if tag == "" {
					return
				}
				if _, ok := seen[tag]; ok {
					return
				}
				seen[tag] = struct{}{}
				if _, err := presets.Get(tag); err != nil {
					t.Errorf("group %s slot %s: unknown preset %q: %v", g.Tag, slot.ID, tag, err)
				}
			}
			check(slot.DefaultPreset)
			for _, s := range slot.Substitutes {
				check(s)
			}
		}
	}
}

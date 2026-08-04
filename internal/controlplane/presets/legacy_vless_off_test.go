//go:build with_controlplane

package presets

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/catalogsqlite"
)

func TestLegacyJSONCatalogRemoved(t *testing.T) {
	dataDir := filepath.Join("data")
	if st, err := os.Stat(dataDir); err == nil && st.IsDir() {
		t.Fatal("legacy presets/data directory must be deleted")
	}
	if _, err := os.Stat(filepath.Join("data", "index.json")); err == nil {
		t.Fatal("legacy presets/data/index.json must be deleted")
	}
	if _, err := os.Stat("loader.go"); err == nil {
		t.Fatal("legacy presets/loader.go must be deleted")
	}
	if !catalogsqlite.Owns("vless_ws_tls") {
		t.Fatal("vless_ws_tls must be owned by catalogsqlite")
	}
	if _, err := Get("vless_ws_tls"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range Protocols() {
		if p.Tag == "vless" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Protocols() must expose vless from catalogsqlite")
	}
}

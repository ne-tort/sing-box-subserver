//go:build with_controlplane

// One-shot: dump demuxgroups.All() into catalogsqlite/ref/demux/*.json
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/demuxgroups"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	outDir := filepath.Join(root, "internal", "controlplane", "catalogsqlite", "ref", "demux")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}
	groups := demuxgroups.BuiltinGroups()
	for _, g := range groups {
		raw, err := json.MarshalIndent(g, "", "  ")
		if err != nil {
			panic(err)
		}
		path := filepath.Join(outDir, g.Tag+".json")
		if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
			panic(err)
		}
		fmt.Println("wrote", path)
	}
	fmt.Printf("dumped %d demux groups\n", len(groups))
}

//go:build with_controlplane

// Generates internal/controlplane/catalogsqlite/data/vless.sqlite from ref/vless.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/catalogsqlite"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	out := filepath.Join(root, "internal", "controlplane", "catalogsqlite", "data", "vless.sqlite")
	if err := catalogsqlite.BuildSeedFile(out); err != nil {
		fmt.Fprintf(os.Stderr, "gen catalogsqlite seed: %v\n", err)
		os.Exit(1)
	}
	st, _ := os.Stat(out)
	fmt.Printf("wrote %s (%d bytes)\n", out, st.Size())
}

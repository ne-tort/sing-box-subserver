//go:build ignore

// Generates demux group install JSON for the Docker matrix harness.
// Usage: go run -tags with_controlplane gen_install.go <group_tag> [listen_port] [slot=preset,...]
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/demuxgroups"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: gen_install.go <group_tag> [listen_port] [slot=preset,...]")
		os.Exit(2)
	}
	port := uint16(0)
	slotArg := ""
	if len(os.Args) >= 3 {
		if strings.Contains(os.Args[2], "=") {
			slotArg = os.Args[2]
		} else {
			n, err := strconv.ParseUint(os.Args[2], 10, 16)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			port = uint16(n)
		}
	}
	if len(os.Args) >= 4 {
		slotArg = os.Args[3]
	}
	slots := map[string]string{}
	if slotArg != "" {
		for _, part := range strings.Split(slotArg, ",") {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) != 2 {
				continue
			}
			slots[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	res, err := demuxgroups.BuildInstall(demuxgroups.InstallRequest{
		GroupTag:   os.Args[1],
		SetName:    "dg-matrix",
		Listen:     "::",
		ListenPort: port,
		SlotPreset: slots,
	}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
}

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ne-tort/sing-box-subserver/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print agent and sing-box versions and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("agent_version=%s\n", version.AgentVersion)
		fmt.Printf("agent_commit=%s\n", version.AgentCommit)
		fmt.Printf("singbox_version=%s\n", version.SingBoxVersion())
		fmt.Printf("singbox_commit=%s\n", version.SingBoxCommit)
		os.Exit(0)
	}

	fmt.Fprintln(os.Stderr, "sing-box-subserver skeleton: pass -version; supervisor not implemented yet")
	os.Exit(0)
}

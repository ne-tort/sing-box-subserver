package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ne-tort/sing-box-subserver/internal/app"
	"github.com/ne-tort/sing-box-subserver/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print agent and sing-box versions and exit")
	configPath := flag.String("config", "", "path to agent YAML config")
	exitOnBootFailure := flag.Bool("exit-on-boot-failure", false, "exit if last-good boot start fails")
	flag.Parse()

	if *showVersion {
		fmt.Printf("agent_version=%s\n", version.AgentVersion)
		fmt.Printf("agent_commit=%s\n", version.AgentCommit)
		fmt.Printf("singbox_version=%s\n", version.SingBoxVersion())
		fmt.Printf("singbox_commit=%s\n", version.SingBoxCommit)
		os.Exit(0)
	}

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "usage: subserver -config /path/to/agent.yaml")
		os.Exit(2)
	}

	if err := app.Run(app.Options{
		ConfigPath:        *configPath,
		ExitOnBootFailure: *exitOnBootFailure,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

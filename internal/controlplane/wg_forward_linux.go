//go:build with_controlplane && linux

package controlplane

import (
	"os/exec"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

// applyWgForwardRules clears legacy same-iface iptables rules.
// Peer isolation / relay is now controlled by sing-box-lx `peer_relay` on the hub endpoint.
func applyWgForwardRules(hub domain.WgHub) error {
	hub.Normalize()
	ifName := strings.TrimSpace(hub.Name)
	if ifName == "" {
		ifName = "wg-cp0"
	}
	_ = clearWgForwardIptables(ifName)
	return nil
}

func clearWgForwardIptables(ifName string) error {
	chain := "SUI_CP_WG_FORWARD"
	_ = exec.Command("iptables", "-D", "FORWARD", "-j", chain).Run()
	_ = exec.Command("iptables", "-F", chain).Run()
	_ = exec.Command("iptables", "-X", chain).Run()
	_ = ifName
	return nil
}

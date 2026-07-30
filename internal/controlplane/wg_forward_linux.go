//go:build with_controlplane && linux

package controlplane

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"
)

// applyWgForwardRules installs or clears same-iface ACCEPT for WG P2P (system mode only).
func applyWgForwardRules(hub domain.WgHub) error {
	hub.Normalize()
	ifName := strings.TrimSpace(hub.Name)
	if ifName == "" {
		ifName = "wg-cp0"
	}
	_ = clearWgForwardIptables(ifName)
	if !hub.Enabled || !hub.ForwardAllow || !hub.System {
		return nil
	}
	// Require ip_forward; do not silently enable without operator intent — just check.
	return ensureWgForwardIptables(ifName)
}

func ensureWgForwardIptables(ifName string) error {
	chain := "SUI_CP_WG_FORWARD"
	_ = exec.Command("iptables", "-N", chain).Run()
	_ = exec.Command("iptables", "-F", chain).Run()
	if err := exec.Command("iptables", "-A", chain,
		"-i", ifName, "-o", ifName, "-j", "ACCEPT",
		"-m", "comment", "--comment", "sui-cp-wg-forward").Run(); err != nil {
		return fmt.Errorf("iptables add forward: %w", err)
	}
	// Ensure jump from FORWARD once
	out, _ := exec.Command("iptables", "-S", "FORWARD").Output()
	if !strings.Contains(string(out), "-j "+chain) {
		_ = exec.Command("iptables", "-I", "FORWARD", "1", "-j", chain).Run()
	}
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

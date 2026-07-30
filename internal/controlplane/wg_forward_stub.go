//go:build with_controlplane && !linux

package controlplane

import "github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"

func applyWgForwardRules(hub domain.WgHub) error {
	_ = hub
	// Firewall P2P rules are Linux-only (iptables).
	return nil
}

//go:build with_controlplane

package controlplane

import "github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"

// TrafficHooks is implemented by traffic/cpbridge when both modules are enabled.
type TrafficHooks interface {
	OnMaterialize(users []domain.User, sets []domain.InboundSet)
	OnTrafficReset(userID string)
}

//go:build with_controlplane

package controlplane

import "github.com/ne-tort/sing-box-subserver/internal/controlplane/domain"

// TrafficHooks is implemented by traffic/cpbridge when both modules are enabled.
type TrafficHooks interface {
	// OnMaterialize refreshes subject manifest + shaping limits from CP users/sets.
	OnMaterialize(users []domain.User, sets []domain.InboundSet)
	// OnTrafficReset zeroes live+store counters for a CP user (periodic/admin reset).
	OnTrafficReset(userID string)
	// OnTrafficUsedPatched syncs absolute usage into the traffic store after admin PATCH.
	OnTrafficUsedPatched(userID string, used uint64)
	// OnBecameIneligible kicks live dataplane sessions for users that just lost eligibility
	// (quota/expiry/disable) before rematerialize omits their creds.
	OnBecameIneligible(userIDs []string)
}

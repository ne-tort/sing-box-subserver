package box

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
)

// TrafficHook attaches optional accounting/shaping trackers around box lifecycle.
// Implemented by internal/traffic.Module when built with with_traffic.
type TrafficHook interface {
	Trackers() []adapter.ConnectionTracker
	OnBoxStarted(ctx context.Context)
	OnBoxStopped()
}

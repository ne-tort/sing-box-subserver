//go:build !(with_traffic && with_controlplane)

package cpbridge

import (
	"context"
	"log/slog"
)

// Bridge is a no-op without both with_traffic and with_controlplane.
type Bridge struct{}

// Attach returns nil when traffic+controlplane are not both enabled.
func Attach(cp any, mod any, log *slog.Logger) *Bridge {
	return nil
}

func (b *Bridge) Run(context.Context) {}
func (b *Bridge) SyncNow(context.Context) {}

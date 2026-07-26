//go:build !with_naive_outbound

package box

import (
	"github.com/sagernet/sing-box/adapter/outbound"
)

func registerNaiveOutbound(registry *outbound.Registry) {
	// Intentionally omitted in slim server profile (no with_naive_outbound).
	_ = registry
}

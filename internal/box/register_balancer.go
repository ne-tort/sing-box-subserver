//go:build with_balancer

package box

import (
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/group/balancer"
)

func registerBalancerOutbound(registry *outbound.Registry) {
	balancer.RegisterOutbound(registry)
}

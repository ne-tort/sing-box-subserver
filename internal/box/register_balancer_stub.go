//go:build !with_balancer

package box

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

func registerBalancerOutbound(registry *outbound.Registry) {
	outbound.Register[option.BalancerOutboundOptions](registry, C.TypeBalancer, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.BalancerOutboundOptions) (adapter.Outbound, error) {
		return nil, E.New("balancer is not included in this build, rebuild with -tags with_balancer")
	})
}

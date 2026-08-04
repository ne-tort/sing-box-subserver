//go:build !with_shadowquic

package box

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

func registerShadowQUICInbound(registry *inbound.Registry) {
	inbound.Register[option.ShadowQUICInboundOptions](registry, C.TypeShadowQUIC, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ShadowQUICInboundOptions) (adapter.Inbound, error) {
		return nil, E.New(`shadowquic inbound is not included in this build, rebuild with -tags with_shadowquic`)
	})
}

func registerShadowQUICOutbound(registry *outbound.Registry) {
	outbound.Register[option.ShadowQUICOutboundOptions](registry, C.TypeShadowQUIC, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.ShadowQUICOutboundOptions) (adapter.Outbound, error) {
		return nil, E.New(`shadowquic outbound is not included in this build, rebuild with -tags with_shadowquic`)
	})
}

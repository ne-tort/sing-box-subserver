//go:build !with_derp

package box

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

func registerDERPInbound(registry *inbound.Registry) {
	inbound.Register[option.DERPInboundOptions](registry, C.TypeDERP, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.DERPInboundOptions) (adapter.Inbound, error) {
		return nil, E.New("derp is not included in this build, rebuild with -tags with_derp")
	})
}

func registerDERPOutbound(registry *outbound.Registry) {
	outbound.Register[option.DERPOutboundOptions](registry, C.TypeDERP, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.DERPOutboundOptions) (adapter.Outbound, error) {
		return nil, E.New("derp is not included in this build, rebuild with -tags with_derp")
	})
}

func registerDERPService(registry *service.Registry) {
	service.Register[option.DERPServiceOptions](registry, C.TypeDERP, func(ctx context.Context, logger log.ContextLogger, tag string, options option.DERPServiceOptions) (adapter.Service, error) {
		return nil, E.New("derp service is not included in this build, rebuild with -tags with_derp")
	})
}

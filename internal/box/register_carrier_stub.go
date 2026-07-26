//go:build !with_carrier

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

func registerCarrierInbound(registry *inbound.Registry) {
	inbound.Register[option.CarrierInboundOptions](registry, C.TypeCarrier, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.CarrierInboundOptions) (adapter.Inbound, error) {
		return nil, E.New("carrier is not included in this build, rebuild with -tags with_carrier")
	})
}

func registerCarrierOutbound(registry *outbound.Registry) {
	outbound.Register[option.CarrierOutboundOptions](registry, C.TypeCarrier, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.CarrierOutboundOptions) (adapter.Outbound, error) {
		return nil, E.New("carrier is not included in this build, rebuild with -tags with_carrier")
	})
}

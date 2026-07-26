//go:build with_carrier

package box

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/carrier"
)

func registerCarrierInbound(registry *inbound.Registry) {
	carrier.RegisterInbound(registry)
}

func registerCarrierOutbound(registry *outbound.Registry) {
	carrier.RegisterOutbound(registry)
}

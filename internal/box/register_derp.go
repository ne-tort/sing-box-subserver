//go:build with_derp

package box

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/protocol/derp"
	derpsvc "github.com/sagernet/sing-box/service/derp"
)

func registerDERPInbound(registry *inbound.Registry) {
	derp.RegisterInbound(registry)
}

func registerDERPOutbound(registry *outbound.Registry) {
	derp.RegisterOutbound(registry)
}

func registerDERPService(registry *service.Registry) {
	derpsvc.Register(registry)
}

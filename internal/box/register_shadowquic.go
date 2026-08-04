//go:build with_shadowquic

package box

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/protocol/shadowquic"
)

func registerShadowQUICInbound(registry *inbound.Registry) {
	shadowquic.RegisterInbound(registry)
}

func registerShadowQUICOutbound(registry *outbound.Registry) {
	shadowquic.RegisterOutbound(registry)
}

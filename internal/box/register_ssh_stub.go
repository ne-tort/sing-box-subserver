//go:build !with_ssh

package box

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
)

func registerSSHInbound(registry *inbound.Registry) {
	inbound.Register[option.SSHInboundOptions](registry, C.TypeSSH, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.SSHInboundOptions) (adapter.Inbound, error) {
		return nil, E.New("ssh inbound is not included in this build, rebuild with -tags with_ssh")
	})
}

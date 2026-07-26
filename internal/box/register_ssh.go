//go:build with_ssh

package box

import (
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/protocol/ssh"
)

func registerSSHInbound(registry *inbound.Registry) {
	ssh.RegisterInbound(registry)
}

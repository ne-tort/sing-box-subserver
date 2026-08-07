//go:build with_controlplane

package controlplane

import (
	"context"
	"net"
	"strings"

	"github.com/ne-tort/sing-box-subserver/internal/controlplane/freedns"
)

func (s *Service) resolveBootstrapIPv4(ctx context.Context) (net.IP, error) {
	host := ""
	if s.cfg.Cfg != nil {
		host = strings.TrimSpace(s.cfg.Cfg.Controlplane.PublicHost)
	}
	return freedns.ResolvePublicIPv4(ctx, host)
}

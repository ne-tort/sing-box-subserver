//go:build !with_demux

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

func registerDemuxInbound(registry *inbound.Registry) {
	inbound.Register[option.DemuxInboundOptions](registry, C.TypeDemux, func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.DemuxInboundOptions) (adapter.Inbound, error) {
		return nil, E.New("demux is not included in this build, rebuild with -tags with_demux")
	})
}

func registerDemuxInjectFeeds(ctx context.Context) context.Context { return ctx }

func registerDemuxInjectFeedsFresh(ctx context.Context) context.Context { return ctx }

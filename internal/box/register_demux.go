//go:build with_demux

package box

import (
	"context"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/protocol/demux"
	"github.com/sagernet/sing-box/protocol/demux/tcp_inject"
	"github.com/sagernet/sing-box/protocol/demux/udp_inject"
	"github.com/sagernet/sing/service"
)

func registerDemuxInbound(registry *inbound.Registry) {
	demux.RegisterInbound(registry)
}

func registerDemuxInjectFeeds(ctx context.Context) context.Context {
	if service.FromContext[adapter.UDPListenFeed](ctx) != nil &&
		service.FromContext[adapter.TCPListenFeed](ctx) != nil {
		return ctx
	}
	return registerDemuxInjectFeedsFresh(ctx)
}

func registerDemuxInjectFeedsFresh(ctx context.Context) context.Context {
	p := udp_inject.NewProvider()
	ctx = service.ContextWithPtr(ctx, p)
	ctx = service.ContextWith[adapter.UDPListenFeed](ctx, p)
	tp := tcp_inject.NewProvider()
	ctx = service.ContextWithPtr(ctx, tp)
	ctx = service.ContextWith[adapter.TCPListenFeed](ctx, tp)
	tcp_inject.SetActiveProvider(tp)
	return ctx
}

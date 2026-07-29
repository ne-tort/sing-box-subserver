//go:build with_traffic

package tracker

import (
	"context"
	"io"
	"net"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"golang.org/x/time/rate"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
)

const rateLimitBurst = 1 << 20 // 1 MiB; WaitN rejects n > burst

type userLiveLimiters struct {
	up   *rate.Limiter
	down *rate.Limiter
}

// RateLimitTracker throttles connections by metadata.User (dataplane key).
type RateLimitTracker struct {
	access   sync.RWMutex
	limits   map[string]domain.SpeedLimit
	live     map[string]*userLiveLimiters
	skipTags map[string]struct{}
}

func NewRateLimitTracker() *RateLimitTracker {
	return &RateLimitTracker{
		limits:   make(map[string]domain.SpeedLimit),
		live:     make(map[string]*userLiveLimiters),
		skipTags: make(map[string]struct{}),
	}
}

func (t *RateLimitTracker) SetSkipInboundTags(tags []string) {
	t.access.Lock()
	defer t.access.Unlock()
	t.skipTags = make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		if tag != "" {
			t.skipTags[tag] = struct{}{}
		}
	}
}

func (t *RateLimitTracker) skip(inboundTag string) bool {
	t.access.RLock()
	defer t.access.RUnlock()
	_, ok := t.skipTags[inboundTag]
	return ok
}

func (t *RateLimitTracker) Reset() {
	t.access.Lock()
	defer t.access.Unlock()
	t.limits = make(map[string]domain.SpeedLimit)
	t.live = make(map[string]*userLiveLimiters)
}

// SetLimits replaces per-dataplane-key caps and updates live limiters.
func (t *RateLimitTracker) SetLimits(m map[string]domain.SpeedLimit) {
	t.access.Lock()
	defer t.access.Unlock()
	if m == nil {
		m = make(map[string]domain.SpeedLimit)
	}
	t.limits = m
	for name, lim := range m {
		if lim.UpBytesPerSec <= 0 && lim.DownBytesPerSec <= 0 {
			delete(t.live, name)
			continue
		}
		live, ok := t.live[name]
		if !ok {
			t.live[name] = newLiveLimiters(lim)
			continue
		}
		applyLiveLimiters(live, lim)
	}
	for name := range t.live {
		if _, ok := m[name]; !ok {
			delete(t.live, name)
		}
	}
}

func newLiveLimiters(lim domain.SpeedLimit) *userLiveLimiters {
	live := &userLiveLimiters{}
	if lim.UpBytesPerSec > 0 {
		live.up = rate.NewLimiter(rate.Limit(lim.UpBytesPerSec), rateLimitBurst)
	}
	if lim.DownBytesPerSec > 0 {
		live.down = rate.NewLimiter(rate.Limit(lim.DownBytesPerSec), rateLimitBurst)
	}
	return live
}

func applyLiveLimiters(live *userLiveLimiters, lim domain.SpeedLimit) {
	if lim.UpBytesPerSec > 0 {
		if live.up == nil {
			live.up = rate.NewLimiter(rate.Limit(lim.UpBytesPerSec), rateLimitBurst)
		} else {
			live.up.SetLimit(rate.Limit(lim.UpBytesPerSec))
		}
	} else {
		live.up = nil
	}
	if lim.DownBytesPerSec > 0 {
		if live.down == nil {
			live.down = rate.NewLimiter(rate.Limit(lim.DownBytesPerSec), rateLimitBurst)
		} else {
			live.down.SetLimit(rate.Limit(lim.DownBytesPerSec))
		}
	} else {
		live.down = nil
	}
}

func (t *RateLimitTracker) lookupLive(user string) *userLiveLimiters {
	t.access.RLock()
	lim, ok := t.limits[user]
	if !ok || (lim.UpBytesPerSec <= 0 && lim.DownBytesPerSec <= 0) {
		t.access.RUnlock()
		return nil
	}
	live, ok := t.live[user]
	t.access.RUnlock()
	if ok {
		return live
	}
	t.access.Lock()
	defer t.access.Unlock()
	lim, ok = t.limits[user]
	if !ok || (lim.UpBytesPerSec <= 0 && lim.DownBytesPerSec <= 0) {
		return nil
	}
	if live, ok = t.live[user]; ok {
		return live
	}
	live = newLiveLimiters(lim)
	t.live[user] = live
	return live
}

func (t *RateLimitTracker) RoutedConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) net.Conn {
	if t.skip(metadata.Inbound) || metadata.User == "" {
		return conn
	}
	t.access.RLock()
	n := len(t.limits)
	t.access.RUnlock()
	if n == 0 {
		return conn
	}
	live := t.lookupLive(metadata.User)
	if live == nil || (live.up == nil && live.down == nil) {
		return conn
	}
	return wrapRateLimitedConn(ctx, conn, live.up, live.down)
}

func (t *RateLimitTracker) RoutedPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) N.PacketConn {
	if t.skip(metadata.Inbound) || metadata.User == "" {
		return conn
	}
	t.access.RLock()
	n := len(t.limits)
	t.access.RUnlock()
	if n == 0 {
		return conn
	}
	live := t.lookupLive(metadata.User)
	if live == nil || (live.up == nil && live.down == nil) {
		return conn
	}
	return wrapRateLimitedPacketConn(ctx, conn, live.up, live.down)
}

func (t *RateLimitTracker) RoutedFlow(context.Context, adapter.InboundContext, adapter.Rule, adapter.Outbound) tun.FlowTracker {
	return nil
}

func waitNChunked(ctx context.Context, lim *rate.Limiter, n int) error {
	for n > 0 {
		chunk := n
		if burst := lim.Burst(); chunk > burst {
			chunk = burst
		}
		if err := lim.WaitN(ctx, chunk); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
}

type rateLimitedConn struct {
	net.Conn
	ctx  context.Context
	up   *rate.Limiter
	down *rate.Limiter
}

func wrapRateLimitedConn(ctx context.Context, conn net.Conn, up, down *rate.Limiter) net.Conn {
	return &rateLimitedConn{Conn: conn, ctx: ctx, up: up, down: down}
}

func (c *rateLimitedConn) Upstream() any { return c.Conn }
func (c *rateLimitedConn) ReaderReplaceable() bool { return false }
func (c *rateLimitedConn) WriterReplaceable() bool { return false }

func (c *rateLimitedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil || n == 0 || c.down == nil {
		return n, err
	}
	if werr := waitNChunked(c.ctx, c.down, n); werr != nil {
		return n, werr
	}
	return n, err
}

func (c *rateLimitedConn) Write(p []byte) (int, error) {
	if c.up != nil {
		if err := waitNChunked(c.ctx, c.up, len(p)); err != nil {
			return 0, err
		}
	}
	return c.Conn.Write(p)
}

func (c *rateLimitedConn) ReadBuffer(buffer *buf.Buffer) error {
	var err error
	if r, ok := c.Conn.(N.ExtendedReader); ok {
		err = r.ReadBuffer(buffer)
	} else {
		n, readErr := c.Conn.Read(buffer.FreeBytes())
		buffer.Truncate(n)
		if n > 0 && readErr == io.EOF {
			err = nil
		} else {
			err = readErr
		}
	}
	if err != nil || buffer.Len() == 0 || c.down == nil {
		return err
	}
	return waitNChunked(c.ctx, c.down, buffer.Len())
}

func (c *rateLimitedConn) WriteBuffer(buffer *buf.Buffer) error {
	if c.up != nil && buffer.Len() > 0 {
		if err := waitNChunked(c.ctx, c.up, buffer.Len()); err != nil {
			return err
		}
	}
	if w, ok := c.Conn.(N.ExtendedWriter); ok {
		return w.WriteBuffer(buffer)
	}
	defer buffer.Release()
	return common.Error(c.Conn.Write(buffer.Bytes()))
}

type rateLimitedPacketConn struct {
	N.PacketConn
	ctx  context.Context
	up   *rate.Limiter
	down *rate.Limiter
}

func wrapRateLimitedPacketConn(ctx context.Context, conn N.PacketConn, up, down *rate.Limiter) N.PacketConn {
	return &rateLimitedPacketConn{PacketConn: conn, ctx: ctx, up: up, down: down}
}

func (c *rateLimitedPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	destination, err = c.PacketConn.ReadPacket(buffer)
	if err != nil || buffer.Len() == 0 || c.down == nil {
		return
	}
	if werr := waitNChunked(c.ctx, c.down, buffer.Len()); werr != nil {
		err = werr
	}
	return
}

func (c *rateLimitedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	if c.up != nil && buffer.Len() > 0 {
		if err := waitNChunked(c.ctx, c.up, buffer.Len()); err != nil {
			return err
		}
	}
	return c.PacketConn.WritePacket(buffer, destination)
}

func (c *rateLimitedPacketConn) Upstream() any { return c.PacketConn }

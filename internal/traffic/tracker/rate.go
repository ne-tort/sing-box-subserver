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

// Burst sized for realistic shaping: large enough for waitNChunked against
// typical sing-box read sizes, small enough that a 1 MiB transfer is throttled.
const (
	rateLimitBurstMin = 16 << 10 // 16 KiB
	rateLimitBurstMax = 64 << 10 // 64 KiB
)

func burstFor(bytesPerSec int64) int {
	b := int(bytesPerSec)
	if b < rateLimitBurstMin {
		b = rateLimitBurstMin
	}
	if b > rateLimitBurstMax {
		b = rateLimitBurstMax
	}
	return b
}

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

// Limits returns a copy of current shaping map (tests / diagnostics).
func (t *RateLimitTracker) Limits() map[string]domain.SpeedLimit {
	t.access.RLock()
	defer t.access.RUnlock()
	out := make(map[string]domain.SpeedLimit, len(t.limits))
	for k, v := range t.limits {
		out[k] = v
	}
	return out
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
		live.up = rate.NewLimiter(rate.Limit(lim.UpBytesPerSec), burstFor(lim.UpBytesPerSec))
	}
	if lim.DownBytesPerSec > 0 {
		live.down = rate.NewLimiter(rate.Limit(lim.DownBytesPerSec), burstFor(lim.DownBytesPerSec))
	}
	return live
}

func applyLiveLimiters(live *userLiveLimiters, lim domain.SpeedLimit) {
	if lim.UpBytesPerSec > 0 {
		if live.up == nil {
			live.up = rate.NewLimiter(rate.Limit(lim.UpBytesPerSec), burstFor(lim.UpBytesPerSec))
		} else {
			live.up.SetLimit(rate.Limit(lim.UpBytesPerSec))
			live.up.SetBurst(burstFor(lim.UpBytesPerSec))
		}
	} else {
		live.up = nil
	}
	if lim.DownBytesPerSec > 0 {
		if live.down == nil {
			live.down = rate.NewLimiter(rate.Limit(lim.DownBytesPerSec), burstFor(lim.DownBytesPerSec))
		} else {
			live.down.SetLimit(rate.Limit(lim.DownBytesPerSec))
			live.down.SetBurst(burstFor(lim.DownBytesPerSec))
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
	// Always wrap when a user is present: Read/Write re-lookup limiters so
	// clearing speed_* immediately unthrottles without reconnect.
	return wrapRateLimitedConn(ctx, conn, t, metadata.User)
}

func (t *RateLimitTracker) RoutedPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) N.PacketConn {
	if t.skip(metadata.Inbound) || metadata.User == "" {
		return conn
	}
	return wrapRateLimitedPacketConn(ctx, conn, t, metadata.User)
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
	t    *RateLimitTracker
	user string
}

func wrapRateLimitedConn(ctx context.Context, conn net.Conn, t *RateLimitTracker, user string) net.Conn {
	return &rateLimitedConn{Conn: conn, ctx: ctx, t: t, user: user}
}

func (c *rateLimitedConn) limiters() (up, down *rate.Limiter) {
	live := c.t.lookupLive(c.user)
	if live == nil {
		return nil, nil
	}
	return live.up, live.down
}

func (c *rateLimitedConn) Upstream() any           { return c.Conn }
func (c *rateLimitedConn) ReaderReplaceable() bool { return false }
func (c *rateLimitedConn) WriterReplaceable() bool { return false }

func (c *rateLimitedConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	// Inbound: Read = bytes from client = user upload.
	up, _ := c.limiters()
	if err != nil || n == 0 || up == nil {
		return n, err
	}
	if werr := waitNChunked(c.ctx, up, n); werr != nil {
		return n, werr
	}
	return n, err
}

func (c *rateLimitedConn) Write(p []byte) (int, error) {
	// Inbound: Write = bytes to client = user download.
	_, down := c.limiters()
	if down != nil {
		if err := waitNChunked(c.ctx, down, len(p)); err != nil {
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
	up, _ := c.limiters()
	if err != nil || buffer.Len() == 0 || up == nil {
		return err
	}
	return waitNChunked(c.ctx, up, buffer.Len())
}

func (c *rateLimitedConn) WriteBuffer(buffer *buf.Buffer) error {
	_, down := c.limiters()
	if down != nil && buffer.Len() > 0 {
		if err := waitNChunked(c.ctx, down, buffer.Len()); err != nil {
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
	t    *RateLimitTracker
	user string
}

func wrapRateLimitedPacketConn(ctx context.Context, conn N.PacketConn, t *RateLimitTracker, user string) N.PacketConn {
	return &rateLimitedPacketConn{PacketConn: conn, ctx: ctx, t: t, user: user}
}

func (c *rateLimitedPacketConn) limiters() (up, down *rate.Limiter) {
	live := c.t.lookupLive(c.user)
	if live == nil {
		return nil, nil
	}
	return live.up, live.down
}

func (c *rateLimitedPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	destination, err = c.PacketConn.ReadPacket(buffer)
	// Inbound packet: Read = from client = upload.
	up, _ := c.limiters()
	if err != nil || buffer.Len() == 0 || up == nil {
		return
	}
	if werr := waitNChunked(c.ctx, up, buffer.Len()); werr != nil {
		err = werr
	}
	return
}

func (c *rateLimitedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	// Inbound packet: Write = to client = download.
	_, down := c.limiters()
	if down != nil && buffer.Len() > 0 {
		if err := waitNChunked(c.ctx, down, buffer.Len()); err != nil {
			return err
		}
	}
	return c.PacketConn.WritePacket(buffer, destination)
}

func (c *rateLimitedPacketConn) Upstream() any { return c.PacketConn }

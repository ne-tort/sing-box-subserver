//go:build with_traffic

package tracker

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

// ConnInfo is one tracked TCP/UDP session.
type ConnInfo struct {
	ID         string
	Conn       net.Conn
	PacketConn network.PacketConn
	Inbound    string
	User       string // dataplane metadata.User
}

// ConnTracker tracks live sessions so ineligible users can be kicked without
// restarting the whole box (s-ui ConnTracker pattern + CloseConnByUser).
type ConnTracker struct {
	access      sync.Mutex
	connections map[string]*ConnInfo
	seq         atomic.Uint64
	skipTags    map[string]struct{}
}

func NewConnTracker() *ConnTracker {
	return &ConnTracker{
		connections: make(map[string]*ConnInfo),
		skipTags:    make(map[string]struct{}),
	}
}

func (c *ConnTracker) SetSkipInboundTags(tags []string) {
	c.access.Lock()
	defer c.access.Unlock()
	c.skipTags = make(map[string]struct{}, len(tags))
	for _, t := range tags {
		if t != "" {
			c.skipTags[t] = struct{}{}
		}
	}
}

func (c *ConnTracker) skip(inbound string) bool {
	c.access.Lock()
	defer c.access.Unlock()
	_, ok := c.skipTags[inbound]
	return ok
}

func (c *ConnTracker) Reset() {
	c.access.Lock()
	defer c.access.Unlock()
	for _, info := range c.connections {
		if info.Conn != nil {
			_ = info.Conn.Close()
		}
		if info.PacketConn != nil {
			_ = info.PacketConn.Close()
		}
	}
	c.connections = make(map[string]*ConnInfo)
}

func (c *ConnTracker) generateConnectionID() string {
	return fmt.Sprintf("%d", c.seq.Add(1))
}

func (c *ConnTracker) RoutedConnection(_ context.Context, conn net.Conn, metadata adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) net.Conn {
	if c.skip(metadata.Inbound) {
		return conn
	}
	id := c.generateConnectionID()
	info := &ConnInfo{ID: id, Conn: conn, Inbound: metadata.Inbound, User: metadata.User}
	c.track(id, info)
	return &trackedConn{Conn: conn, tracker: c, connID: id}
}

func (c *ConnTracker) RoutedPacketConnection(_ context.Context, conn network.PacketConn, metadata adapter.InboundContext, _ adapter.Rule, _ adapter.Outbound) network.PacketConn {
	if c.skip(metadata.Inbound) {
		return conn
	}
	id := c.generateConnectionID()
	info := &ConnInfo{ID: id, PacketConn: conn, Inbound: metadata.Inbound, User: metadata.User}
	c.track(id, info)
	return &trackedPacketConn{PacketConn: conn, tracker: c, connID: id}
}

func (c *ConnTracker) RoutedFlow(context.Context, adapter.InboundContext, adapter.Rule, adapter.Outbound) tun.FlowTracker {
	return nil
}

// CloseConnByUsers closes sessions whose metadata.User is in keys. Returns closed count.
func (c *ConnTracker) CloseConnByUsers(keys []string) int {
	if c == nil || len(keys) == 0 {
		return 0
	}
	want := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k != "" {
			want[k] = struct{}{}
		}
	}
	c.access.Lock()
	defer c.access.Unlock()
	n := 0
	for id, info := range c.connections {
		if _, ok := want[info.User]; !ok {
			continue
		}
		if info.Conn != nil {
			_ = info.Conn.Close()
		}
		if info.PacketConn != nil {
			_ = info.PacketConn.Close()
		}
		delete(c.connections, id)
		n++
	}
	return n
}

// CloseConnByInbound closes all sessions for an inbound tag.
func (c *ConnTracker) CloseConnByInbound(inbound string) int {
	if c == nil || inbound == "" {
		return 0
	}
	c.access.Lock()
	defer c.access.Unlock()
	n := 0
	for id, info := range c.connections {
		if info.Inbound != inbound {
			continue
		}
		if info.Conn != nil {
			_ = info.Conn.Close()
		}
		if info.PacketConn != nil {
			_ = info.PacketConn.Close()
		}
		delete(c.connections, id)
		n++
	}
	return n
}

// ActiveCount returns tracked session count (tests/diagnostics).
func (c *ConnTracker) ActiveCount() int {
	if c == nil {
		return 0
	}
	c.access.Lock()
	defer c.access.Unlock()
	return len(c.connections)
}

func (c *ConnTracker) track(id string, info *ConnInfo) {
	c.access.Lock()
	defer c.access.Unlock()
	c.connections[id] = info
}

func (c *ConnTracker) untrack(id string) {
	c.access.Lock()
	defer c.access.Unlock()
	delete(c.connections, id)
}

// trackedConn unwraps for Copy (Reader/WriterReplaceable) like s-ui / sing-box route.trackedConn.
type trackedConn struct {
	net.Conn
	tracker     *ConnTracker
	connID      string
	untrackOnce sync.Once
}

func (w *trackedConn) doUntrack() {
	w.untrackOnce.Do(func() { w.tracker.untrack(w.connID) })
}

func (w *trackedConn) Close() error {
	w.doUntrack()
	return w.Conn.Close()
}

func (w *trackedConn) Upstream() any            { return w.Conn }
func (w *trackedConn) ReaderReplaceable() bool  { return true }
func (w *trackedConn) WriterReplaceable() bool  { return true }

type trackedPacketConn struct {
	network.PacketConn
	tracker     *ConnTracker
	connID      string
	untrackOnce sync.Once
}

func (w *trackedPacketConn) doUntrack() {
	w.untrackOnce.Do(func() { w.tracker.untrack(w.connID) })
}

func (w *trackedPacketConn) ReadPacket(buffer *buf.Buffer) (destination M.Socksaddr, err error) {
	return w.PacketConn.ReadPacket(buffer)
}

func (w *trackedPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	return w.PacketConn.WritePacket(buffer, destination)
}

func (w *trackedPacketConn) Close() error {
	w.doUntrack()
	return w.PacketConn.Close()
}

func (w *trackedPacketConn) Upstream() any           { return w.PacketConn }
func (w *trackedPacketConn) ReaderReplaceable() bool { return true }
func (w *trackedPacketConn) WriterReplaceable() bool { return true }

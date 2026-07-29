//go:build with_traffic

package tracker

import (
	"context"
	"net"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/atomic"
	"github.com/sagernet/sing/common/bufio"
	"github.com/sagernet/sing/common/network"
)

// Counter holds uplink/downlink atomics.
// Convention (matches s-ui): read = up (client→server), write = down (server→client).
type Counter struct {
	read  *atomic.Int64
	write *atomic.Int64
}

// Delta is a swapped counter snapshot.
type Delta struct {
	Key  string
	Up   int64
	Down int64
}

// StatsTracker implements adapter.ConnectionTracker for byte accounting.
type StatsTracker struct {
	access    sync.Mutex
	inbounds  map[string]Counter
	outbounds map[string]Counter
	users     map[string]Counter
	skipTags  map[string]struct{} // WG / special tags that skip L4 wraps
}

func NewStatsTracker() *StatsTracker {
	return &StatsTracker{
		inbounds:  make(map[string]Counter),
		outbounds: make(map[string]Counter),
		users:     make(map[string]Counter),
		skipTags:  make(map[string]struct{}),
	}
}

// SetSkipInboundTags marks inbound tags that must not be wrapped (e.g. WireGuard).
func (c *StatsTracker) SetSkipInboundTags(tags []string) {
	c.access.Lock()
	defer c.access.Unlock()
	c.skipTags = make(map[string]struct{}, len(tags))
	for _, t := range tags {
		if t != "" {
			c.skipTags[t] = struct{}{}
		}
	}
}

func (c *StatsTracker) Reset() {
	c.access.Lock()
	defer c.access.Unlock()
	c.inbounds = make(map[string]Counter)
	c.outbounds = make(map[string]Counter)
	c.users = make(map[string]Counter)
}

func (c *StatsTracker) loadOrCreate(obj *map[string]Counter, name string) Counter {
	counter, loaded := (*obj)[name]
	if loaded {
		return counter
	}
	counter = Counter{read: &atomic.Int64{}, write: &atomic.Int64{}}
	(*obj)[name] = counter
	return counter
}

func (c *StatsTracker) getReadCounters(inbound, outbound, user string) ([]*atomic.Int64, []*atomic.Int64) {
	var readCounter []*atomic.Int64
	var writeCounter []*atomic.Int64
	c.access.Lock()
	defer c.access.Unlock()
	if inbound != "" {
		ctr := c.loadOrCreate(&c.inbounds, inbound)
		readCounter = append(readCounter, ctr.read)
		writeCounter = append(writeCounter, ctr.write)
	}
	if outbound != "" {
		ctr := c.loadOrCreate(&c.outbounds, outbound)
		readCounter = append(readCounter, ctr.read)
		writeCounter = append(writeCounter, ctr.write)
	}
	if user != "" {
		ctr := c.loadOrCreate(&c.users, user)
		readCounter = append(readCounter, ctr.read)
		writeCounter = append(writeCounter, ctr.write)
	}
	return readCounter, writeCounter
}

func (c *StatsTracker) skip(inboundTag string) bool {
	c.access.Lock()
	defer c.access.Unlock()
	_, ok := c.skipTags[inboundTag]
	return ok
}

func (c *StatsTracker) RoutedConnection(_ context.Context, conn net.Conn, metadata adapter.InboundContext, _ adapter.Rule, matchOutbound adapter.Outbound) net.Conn {
	if c.skip(metadata.Inbound) {
		return conn
	}
	outTag := ""
	if matchOutbound != nil {
		outTag = matchOutbound.Tag()
	}
	readCounter, writeCounter := c.getReadCounters(metadata.Inbound, outTag, metadata.User)
	return bufio.NewInt64CounterConn(conn, readCounter, writeCounter)
}

func (c *StatsTracker) RoutedPacketConnection(_ context.Context, conn network.PacketConn, metadata adapter.InboundContext, _ adapter.Rule, matchOutbound adapter.Outbound) network.PacketConn {
	if c.skip(metadata.Inbound) {
		return conn
	}
	outTag := ""
	if matchOutbound != nil {
		outTag = matchOutbound.Tag()
	}
	readCounter, writeCounter := c.getReadCounters(metadata.Inbound, outTag, metadata.User)
	return bufio.NewInt64CounterPacketConn(conn, readCounter, nil, writeCounter, nil)
}

func (c *StatsTracker) RoutedFlow(context.Context, adapter.InboundContext, adapter.Rule, adapter.Outbound) tun.FlowTracker {
	return nil
}

// AddUserTraffic injects bytes (e.g. future WG IpcGet path).
func (c *StatsTracker) AddUserTraffic(user string, up, down int64) {
	if user == "" || (up <= 0 && down <= 0) {
		return
	}
	c.access.Lock()
	defer c.access.Unlock()
	ctr := c.loadOrCreate(&c.users, user)
	if up > 0 {
		ctr.read.Add(up)
	}
	if down > 0 {
		ctr.write.Add(down)
	}
}

// AddInboundTraffic injects inbound bytes.
func (c *StatsTracker) AddInboundTraffic(inbound string, up, down int64) {
	if inbound == "" || (up <= 0 && down <= 0) {
		return
	}
	c.access.Lock()
	defer c.access.Unlock()
	ctr := c.loadOrCreate(&c.inbounds, inbound)
	if up > 0 {
		ctr.read.Add(up)
	}
	if down > 0 {
		ctr.write.Add(down)
	}
}

// DiscardUserKeys zeros live user counters without emitting a flush sample.
// Used after admin/reset so the next Flush cannot resurrect discarded bytes.
func (c *StatsTracker) DiscardUserKeys(keys []string) {
	if c == nil || len(keys) == 0 {
		return
	}
	c.access.Lock()
	defer c.access.Unlock()
	for _, k := range keys {
		if ctr, ok := c.users[k]; ok {
			ctr.read.Store(0)
			ctr.write.Store(0)
		}
	}
}

// InjectUserTraffic is a test/helper to add live bytes for a dataplane user key.
func (c *StatsTracker) InjectUserTraffic(user string, up, down int64) {
	c.AddUserTraffic(user, up, down)
}

// SwapDeltas atomically resets live counters and returns non-zero deltas.
func (c *StatsTracker) SwapDeltas() (inbounds, outbounds, users []Delta) {
	c.access.Lock()
	defer c.access.Unlock()
	inbounds = swapMap(c.inbounds)
	outbounds = swapMap(c.outbounds)
	users = swapMap(c.users)
	return
}

func swapMap(m map[string]Counter) []Delta {
	out := make([]Delta, 0, len(m))
	for k, ctr := range m {
		up := ctr.read.Swap(0)
		down := ctr.write.Swap(0)
		if up > 0 || down > 0 {
			out = append(out, Delta{Key: k, Up: up, Down: down})
		}
	}
	return out
}

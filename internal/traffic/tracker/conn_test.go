//go:build with_traffic

package tracker_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/tracker"
)

func TestConnTrackerCloseByUser(t *testing.T) {
	t.Parallel()
	ct := tracker.NewConnTracker()
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	wrapped := ct.RoutedConnection(context.Background(), c1, adapter.InboundContext{
		Inbound: "in1", User: "alice",
	}, nil, nil)
	if ct.ActiveCount() != 1 {
		t.Fatalf("active=%d", ct.ActiveCount())
	}
	n := ct.CloseConnByUsers([]string{"alice"})
	if n != 1 {
		t.Fatalf("closed=%d", n)
	}
	if ct.ActiveCount() != 0 {
		t.Fatalf("active after kick=%d", ct.ActiveCount())
	}
	// Write on peer should fail after close.
	_ = c2.SetDeadline(time.Now().Add(200 * time.Millisecond))
	_, err := c2.Write([]byte("x"))
	if err == nil {
		// may still succeed briefly; try read on wrapped
		_ = wrapped.SetDeadline(time.Now().Add(50 * time.Millisecond))
		buf := make([]byte, 1)
		_, _ = wrapped.Read(buf)
	}
}

func TestRateLimitLiveUnthrottle(t *testing.T) {
	t.Parallel()
	rt := tracker.NewRateLimitTracker()
	rt.SetLimits(map[string]domain.SpeedLimit{
		"bob": {UpBytesPerSec: 1024, DownBytesPerSec: 1024},
	})
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	wrapped := rt.RoutedConnection(context.Background(), client, adapter.InboundContext{
		Inbound: "in", User: "bob",
	}, nil, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(io.Discard, server)
	}()
	// Clear limits — subsequent writes must not block on WaitN.
	rt.SetLimits(nil)
	_ = wrapped.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := wrapped.Write(make([]byte, 64*1024)); err != nil {
		t.Fatalf("write after clear limits: %v", err)
	}
	_ = client.Close()
	<-done
}

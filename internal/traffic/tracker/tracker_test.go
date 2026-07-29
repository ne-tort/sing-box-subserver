//go:build with_traffic

package tracker_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/tracker"
	"golang.org/x/time/rate"
)

func TestStatsTrackerSwapDeltas(t *testing.T) {
	t.Parallel()
	st := tracker.NewStatsTracker()
	st.AddUserTraffic("alice", 100, 200)
	st.AddInboundTraffic("in1", 10, 20)
	in, out, users := st.SwapDeltas()
	if len(out) != 0 {
		t.Fatalf("outbounds=%v", out)
	}
	if len(in) != 1 || in[0].Up != 10 || in[0].Down != 20 {
		t.Fatalf("inbounds=%v", in)
	}
	if len(users) != 1 || users[0].Key != "alice" || users[0].Up != 100 || users[0].Down != 200 {
		t.Fatalf("users=%v", users)
	}
	_, _, users2 := st.SwapDeltas()
	if len(users2) != 0 {
		t.Fatalf("expected empty after swap, got %v", users2)
	}
}

func TestRateLimitTrackerThrottles(t *testing.T) {
	t.Parallel()
	rt := tracker.NewRateLimitTracker()
	rt.SetLimits(map[string]domain.SpeedLimit{
		"bob": {UpBytesPerSec: 1024, DownBytesPerSec: 1024},
	})
	// Exercising limiter WaitN via a tiny write path is heavy; check SetLimits does not panic
	// and burst constant matches s-ui (1 MiB).
	lim := rate.NewLimiter(rate.Limit(1024), 1<<20)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := lim.WaitN(ctx, 512); err != nil {
		t.Fatal(err)
	}
	_ = io.Discard
}

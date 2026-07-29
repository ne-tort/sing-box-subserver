//go:build with_traffic

package tracker_test

import (
	"testing"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/tracker"
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

func TestDiscardUserKeysPreventsFlushResurrection(t *testing.T) {
	t.Parallel()
	st := tracker.NewStatsTracker()
	st.AddUserTraffic("alice", 100, 200)
	st.DiscardUserKeys([]string{"alice"})
	_, _, users := st.SwapDeltas()
	if len(users) != 0 {
		t.Fatalf("expected no deltas after discard, got %v", users)
	}
}

func TestRateLimitTrackerSetLimitsReplace(t *testing.T) {
	t.Parallel()
	rt := tracker.NewRateLimitTracker()
	rt.SetLimits(map[string]domain.SpeedLimit{
		"bob": {UpBytesPerSec: 1024, DownBytesPerSec: 1024},
	})
	got := rt.Limits()
	if got["bob"].UpBytesPerSec != 1024 {
		t.Fatalf("%+v", got)
	}
	rt.SetLimits(nil)
	if len(rt.Limits()) != 0 {
		t.Fatalf("expected empty after nil, got %v", rt.Limits())
	}
}

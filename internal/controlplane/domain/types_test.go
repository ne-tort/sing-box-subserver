//go:build with_controlplane

package domain

import "testing"
import "time"

func TestUserEligible(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	lim := uint64(100)
	u := User{Enabled: true}
	if !u.Eligible(now) {
		t.Fatal("default eligible")
	}
	u.Enabled = false
	if u.Eligible(now) {
		t.Fatal("disabled")
	}
	u.Enabled = true
	u.ExpiresAt = &past
	if u.Eligible(now) {
		t.Fatal("expired")
	}
	u.ExpiresAt = nil
	u.TrafficLimitBytes = &lim
	u.TrafficUsedBytes = 100
	if u.Eligible(now) {
		t.Fatal("over limit")
	}
	u.TrafficUsedBytes = 99
	if !u.Eligible(now) {
		t.Fatal("under limit")
	}
}

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/testutil"
)

// Concurrent apply storm: only one should mutate; others get ErrConflict or succeed serially.
func TestStressConcurrentApply(t *testing.T) {
	t.Parallel()
	eng := &testutil.FakeEngine{}
	sup := newTestSupervisor(t, eng)

	const n = 32
	var okCount, conflictCount, other atomic.Int32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			raw := []byte(fmt.Sprintf(`{"n":%d}`, i))
			_, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw, Source: configstore.SourcePush})
			switch {
			case err == nil:
				okCount.Add(1)
			case errors.Is(err, ErrConflict):
				conflictCount.Add(1)
			default:
				other.Add(1)
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()
	if okCount.Load() < 1 {
		t.Fatal("expected at least one successful apply")
	}
	if other.Load() != 0 {
		t.Fatalf("unexpected errors: %d", other.Load())
	}
	st := sup.Status()
	if st.State != StateRunning && st.State != StateRolledBack {
		t.Fatalf("state=%s", st.State)
	}
	if st.Revision < 1 {
		t.Fatalf("revision=%d", st.Revision)
	}
}

// Crash storm: close instance repeatedly; supervisor must recover to Running.
func TestStressCrashRestartStorm(t *testing.T) {
	t.Parallel()
	eng := &testutil.FakeEngine{}
	sup := newTestSupervisor(t, eng)
	if _, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(`{"storm":1}`)}); err != nil {
		t.Fatal(err)
	}

	const crashes = 8
	for i := 0; i < crashes; i++ {
		waitRunning(t, sup, 3*time.Second)
		sup.mu.Lock()
		inst := sup.inst
		startsBefore := eng.Starts.Load()
		sup.mu.Unlock()
		if inst == nil {
			t.Fatalf("no instance before crash %d", i)
		}
		_ = inst.Close()
		waitDown(t, sup, 2*time.Second)
		waitRunning(t, sup, 5*time.Second)
		if eng.Starts.Load() <= startsBefore {
			t.Fatalf("crash %d: Start not called again (starts=%d)", i, eng.Starts.Load())
		}
	}
	st := sup.Status()
	if !st.BoxUp || st.State != StateRunning {
		t.Fatalf("after storm: %+v", st)
	}
}

func waitDown(t *testing.T, sup *Supervisor, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		st := sup.Status()
		if !st.BoxUp || st.State == StateDegraded {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting down; status=%+v", sup.Status())
}

func waitRunning(t *testing.T, sup *Supervisor, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		st := sup.Status()
		if st.State == StateRunning && st.BoxUp {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timeout waiting Running; status=%+v", sup.Status())
}

// Bad configs never leave process without last-good when one existed.
func TestStressAlternatingBadGood(t *testing.T) {
	t.Parallel()
	eng := &testutil.FakeEngine{}
	sup := newTestSupervisor(t, eng)
	good := []byte(`{"ok":true}`)
	if _, err := sup.Apply(context.Background(), ApplyRequest{Raw: good}); err != nil {
		t.Fatal(err)
	}
	sha := configstore.Hash(good)

	for i := 0; i < 20; i++ {
		eng.SetFailNextStart(true)
		_, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(fmt.Sprintf(`{"bad":%d}`, i))})
		if err == nil {
			t.Fatalf("iteration %d: expected fail", i)
		}
		st := sup.Status()
		if st.ContentSHA256 != sha {
			t.Fatalf("iteration %d: lost last-good sha", i)
		}
		if st.Revision != 1 {
			t.Fatalf("iteration %d: revision bumped on fail: %d", i, st.Revision)
		}
	}
}

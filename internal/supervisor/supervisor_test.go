package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/testutil"
)

func newTestSupervisor(t *testing.T, eng BoxEngine) *Supervisor {
	t.Helper()
	store, err := configstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewWithOptions(store, eng, obs.Setup("error").Logger, &obs.Metrics{}, Options{Probe: 0})
}

func TestApplyValidateFailDoesNotStart(t *testing.T) {
	t.Parallel()
	eng := &testutil.FakeEngine{ValidateErr: errors.New("bad")}
	sup := newTestSupervisor(t, eng)
	_, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(`{}`), Source: configstore.SourcePush})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
	if eng.Starts.Load() != 0 {
		t.Fatalf("start should not be called, got %d", eng.Starts.Load())
	}
	if st := sup.Status().State; st != StateStopped {
		t.Fatalf("state=%s", st)
	}
}

func TestApplyStartFailRestoresLastGood(t *testing.T) {
	t.Parallel()
	eng := &testutil.FakeEngine{}
	sup := newTestSupervisor(t, eng)

	raw1 := []byte(`{"v":1}`)
	if _, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw1, Source: configstore.SourcePush}); err != nil {
		t.Fatal(err)
	}

	eng.SetFailNextStart(true)

	raw2 := []byte(`{"v":2}`)
	_, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw2, Source: configstore.SourcePush})
	if err == nil {
		t.Fatal("expected start failure")
	}
	st := sup.Status()
	if st.State != StateRolledBack {
		t.Fatalf("state=%s", st.State)
	}
	if st.ContentSHA256 != configstore.Hash(raw1) {
		t.Fatalf("should keep last-good sha, got %s", st.ContentSHA256)
	}
	if st.Revision != 1 {
		t.Fatalf("revision should stay 1, got %d", st.Revision)
	}
}

func TestApplyIdempotentSameHash(t *testing.T) {
	t.Parallel()
	eng := &testutil.FakeEngine{}
	sup := newTestSupervisor(t, eng)
	raw := []byte(`{"v":1}`)
	if _, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw}); err != nil {
		t.Fatal(err)
	}
	starts := eng.Starts.Load()
	res, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Noop {
		t.Fatal("expected noop")
	}
	if eng.Starts.Load() != starts {
		t.Fatalf("starts should not increase on noop")
	}
}

func TestApplyConflict(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	release := make(chan struct{})
	eng := &blockingEngine{block: block, release: release}
	sup := newTestSupervisor(t, eng)

	errCh := make(chan error, 1)
	go func() {
		_, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(`{"a":1}`)})
		errCh <- err
	}()
	<-block
	_, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(`{"a":2}`)})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

type blockingEngine struct {
	block   chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *blockingEngine) Validate(ctx context.Context, raw []byte) error { return nil }

func (e *blockingEngine) Start(ctx context.Context, raw []byte) (box.Instance, error) {
	e.once.Do(func() { close(e.block) })
	<-e.release
	return testutil.NewFakeInst(), nil
}

func TestBootLastGoodNoRevisionBump(t *testing.T) {
	t.Parallel()
	eng := &testutil.FakeEngine{}
	sup := newTestSupervisor(t, eng)
	raw := []byte(`{"boot":true}`)
	if _, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw}); err != nil {
		t.Fatal(err)
	}
	sup.Shutdown()
	time.Sleep(10 * time.Millisecond)
	if err := sup.BootLastGood(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sup.Status().Revision != 1 {
		t.Fatalf("boot should not bump revision, got %d", sup.Status().Revision)
	}
	if sup.Status().State != StateRunning {
		t.Fatalf("state=%s", sup.Status().State)
	}
}

func TestUnexpectedDoneRestarts(t *testing.T) {
	t.Parallel()
	eng := &testutil.FakeEngine{}
	sup := newTestSupervisor(t, eng)
	if _, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(`{"v":1}`)}); err != nil {
		t.Fatal(err)
	}
	startsBefore := eng.Starts.Load()

	sup.mu.Lock()
	inst := sup.inst
	sup.mu.Unlock()
	if inst == nil {
		t.Fatal("no instance")
	}
	_ = inst.Close() // fires Done → watch → restart

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := sup.Status()
		if st.State == StateRunning && eng.Starts.Load() > startsBefore {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected restart; state=%s starts=%d", sup.Status().State, eng.Starts.Load())
}

func TestStopStartBox(t *testing.T) {
	t.Parallel()
	eng := &testutil.FakeEngine{}
	sup := newTestSupervisor(t, eng)
	if _, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(`{"v":1}`)}); err != nil {
		t.Fatal(err)
	}
	if err := sup.StopBox(); err != nil {
		t.Fatal(err)
	}
	if sup.Status().State != StateStopped || sup.Status().BoxUp {
		t.Fatalf("status=%+v", sup.Status())
	}
	if err := sup.StartBox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sup.Status().State != StateRunning || !sup.Status().BoxUp {
		t.Fatalf("status=%+v", sup.Status())
	}
}

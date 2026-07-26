package supervisor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
)

type fakeInst struct {
	closed chan struct{}
	once   sync.Once
}

func newFakeInst() *fakeInst {
	return &fakeInst{closed: make(chan struct{})}
}

func (f *fakeInst) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

func (f *fakeInst) Done() <-chan struct{} { return f.closed }

type fakeEngine struct {
	mu            sync.Mutex
	validateErr   error
	startErr      error
	startErrOnce  atomic.Bool // fail first Start only
	starts        atomic.Int32
	validates     atomic.Int32
	failNextStart bool
}

func (e *fakeEngine) Validate(ctx context.Context, raw []byte) error {
	e.validates.Add(1)
	if e.validateErr != nil {
		return e.validateErr
	}
	if len(raw) == 0 {
		return errors.New("empty")
	}
	return nil
}

func (e *fakeEngine) Start(ctx context.Context, raw []byte) (box.Instance, error) {
	e.starts.Add(1)
	e.mu.Lock()
	fail := e.failNextStart
	if fail {
		e.failNextStart = false
	}
	permanent := e.startErr
	e.mu.Unlock()
	if fail {
		return nil, errors.New("start failed")
	}
	if permanent != nil {
		return nil, permanent
	}
	return newFakeInst(), nil
}

func newTestSupervisor(t *testing.T, eng BoxEngine) *Supervisor {
	t.Helper()
	store, err := configstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store, eng, obs.Setup("error").Logger, &obs.Metrics{})
}

func TestApplyValidateFailDoesNotStart(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{validateErr: errors.New("bad")}
	sup := newTestSupervisor(t, eng)
	_, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(`{}`), Source: configstore.SourcePush})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
	if eng.starts.Load() != 0 {
		t.Fatalf("start should not be called, got %d", eng.starts.Load())
	}
	if st := sup.Status().State; st != StateStopped {
		t.Fatalf("state=%s", st)
	}
}

func TestApplyStartFailRestoresLastGood(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
	sup := newTestSupervisor(t, eng)

	raw1 := []byte(`{"v":1}`)
	if _, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw1, Source: configstore.SourcePush}); err != nil {
		t.Fatal(err)
	}
	if sup.Status().Revision != 1 {
		t.Fatalf("rev=%d", sup.Status().Revision)
	}

	eng.mu.Lock()
	eng.failNextStart = true
	eng.mu.Unlock()

	raw2 := []byte(`{"v":2}`)
	_, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw2, Source: configstore.SourcePush})
	if err == nil {
		t.Fatal("expected start failure")
	}
	st := sup.Status()
	if st.State != StateRolledBack && st.State != StateRunning {
		// RolledBack after restore
		if st.State != StateRolledBack {
			t.Fatalf("state=%s", st.State)
		}
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
	eng := &fakeEngine{}
	sup := newTestSupervisor(t, eng)
	raw := []byte(`{"v":1}`)
	if _, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw}); err != nil {
		t.Fatal(err)
	}
	starts := eng.starts.Load()
	res, err := sup.Apply(context.Background(), ApplyRequest{Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Noop {
		t.Fatal("expected noop")
	}
	if eng.starts.Load() != starts {
		t.Fatalf("starts should not increase on noop")
	}
}

func TestApplyConflict(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
	// block Start
	block := make(chan struct{})
	release := make(chan struct{})
	eng2 := &blockingEngine{block: block, release: release}
	sup := newTestSupervisor(t, eng2)

	errCh := make(chan error, 1)
	go func() {
		_, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(`{"a":1}`)})
		errCh <- err
	}()
	<-block // first apply entered Start
	_, err := sup.Apply(context.Background(), ApplyRequest{Raw: []byte(`{"a":2}`)})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	_ = eng
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
	return newFakeInst(), nil
}

func TestBootLastGoodNoRevisionBump(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
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

package testutil

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ne-tort/sing-box-subserver/internal/box"
)

// FakeInst is a closable box.Instance for tests.
type FakeInst struct {
	closed chan struct{}
	once   sync.Once
}

func NewFakeInst() *FakeInst {
	return &FakeInst{closed: make(chan struct{})}
}

func (f *FakeInst) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

func (f *FakeInst) Done() <-chan struct{} { return f.closed }

// FakeEngine records Validate/Start calls.
type FakeEngine struct {
	mu            sync.Mutex
	ValidateErr   error
	StartErr      error
	FailNextStart bool
	Starts        atomic.Int32
	Validates     atomic.Int32
}

func (e *FakeEngine) SetFailNextStart(v bool) {
	e.mu.Lock()
	e.FailNextStart = v
	e.mu.Unlock()
}

func (e *FakeEngine) Validate(ctx context.Context, raw []byte) error {
	e.Validates.Add(1)
	if e.ValidateErr != nil {
		return e.ValidateErr
	}
	if len(raw) == 0 {
		return errors.New("empty")
	}
	return nil
}

func (e *FakeEngine) Start(ctx context.Context, raw []byte) (box.Instance, error) {
	e.Starts.Add(1)
	e.mu.Lock()
	fail := e.FailNextStart
	if fail {
		e.FailNextStart = false
	}
	permanent := e.StartErr
	e.mu.Unlock()
	if fail {
		return nil, errors.New("start failed")
	}
	if permanent != nil {
		return nil, permanent
	}
	return NewFakeInst(), nil
}

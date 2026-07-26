package box

import (
	"context"
	"fmt"

	singbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
)

// Engine creates and validates sing-box instances with server registries.
type Engine struct {
	base context.Context
}

// NewEngine builds a reusable engine with registries installed on a base context.
func NewEngine(parent context.Context) *Engine {
	if parent == nil {
		parent = context.Background()
	}
	ctx := singbox.Context(
		parent,
		InboundRegistry(),
		OutboundRegistry(),
		EndpointRegistry(),
		DNSTransportRegistry(),
		ServiceRegistry(),
		CertificateProviderRegistry(),
	)
	ctx = registerDemuxInjectFeeds(ctx)
	return &Engine{base: ctx}
}

// Validate unmarshals options without starting listeners.
func (e *Engine) Validate(ctx context.Context, raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty config")
	}
	if ctx == nil {
		ctx = e.base
	} else {
		ctx = mergeBase(ctx, e.base)
	}
	var opt option.Options
	if err := opt.UnmarshalJSONContext(ctx, raw); err != nil {
		return fmt.Errorf("config invalid: %w", err)
	}
	return nil
}

// Instance is a running sing-box dataplane.
type Instance interface {
	Close() error
	Done() <-chan struct{}
}

type instance struct {
	box  *singbox.Box
	done chan struct{}
}

func (i *instance) Close() error {
	if i == nil || i.box == nil {
		return nil
	}
	err := i.box.Close()
	select {
	case <-i.done:
	default:
		close(i.done)
	}
	return err
}

func (i *instance) Done() <-chan struct{} {
	if i == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return i.done
}

// Start validates, creates, and starts a box. On Start failure the instance is closed.
func (e *Engine) Start(ctx context.Context, raw []byte) (Instance, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty config")
	}
	if ctx == nil {
		ctx = e.base
	} else {
		ctx = mergeBase(ctx, e.base)
	}
	// Fresh demux feeds per start so UDP/TCP inject state is not shared across boxes.
	ctx = registerDemuxInjectFeedsFresh(ctx)

	var opt option.Options
	if err := opt.UnmarshalJSONContext(ctx, raw); err != nil {
		return nil, fmt.Errorf("config invalid: %w", err)
	}
	b, err := singbox.New(singbox.Options{
		Context: ctx,
		Options: opt,
	})
	if err != nil {
		return nil, fmt.Errorf("create box: %w", err)
	}
	if err := b.Start(); err != nil {
		_ = b.Close()
		return nil, fmt.Errorf("start box: %w", err)
	}
	done := make(chan struct{})
	inst := &instance{box: b, done: done}
	return inst, nil
}

func mergeBase(ctx, base context.Context) context.Context {
	if ctx == base {
		return ctx
	}
	return &mergeCtx{Context: ctx, base: base}
}

type mergeCtx struct {
	context.Context
	base context.Context
}

func (m *mergeCtx) Value(key any) any {
	if v := m.Context.Value(key); v != nil {
		return v
	}
	return m.base.Value(key)
}

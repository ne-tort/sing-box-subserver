package box

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	singbox "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// ErrUnsupported is returned when the config requires a feature this agent rejects.
var ErrUnsupported = errors.New("unsupported feature")

// Engine creates and validates sing-box instances with server registries.
type Engine struct {
	base              context.Context
	hook              TrafficHook
	platformLogWriter log.PlatformWriter
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

// SetTrafficHook registers optional traffic trackers (nil clears).
func (e *Engine) SetTrafficHook(h TrafficHook) {
	if e == nil {
		return
	}
	e.hook = h
}

// SetPlatformLogWriter captures sing-box core logs (nil clears).
func (e *Engine) SetPlatformLogWriter(w log.PlatformWriter) {
	if e == nil {
		return
	}
	e.platformLogWriter = w
}

// Validate unmarshals options without starting listeners and enforces agent policy.
func (e *Engine) Validate(ctx context.Context, raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty config")
	}
	if err := rejectClashAPI(raw); err != nil {
		return err
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

func rejectClashAPI(raw []byte) error {
	var probe struct {
		Experimental *struct {
			ClashAPI *struct {
				ExternalController string `json:"external_controller"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil // let UnmarshalJSONContext report parse errors
	}
	if probe.Experimental != nil && probe.Experimental.ClashAPI != nil {
		if probe.Experimental.ClashAPI.ExternalController != "" {
			return fmt.Errorf("%w: experimental.clash_api is not supported on sing-box-subserver (ADR 0006)", ErrUnsupported)
		}
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
	hook TrafficHook
}

func (i *instance) Close() error {
	if i == nil {
		return nil
	}
	if i.hook != nil {
		i.hook.OnBoxStopped()
	}
	var err error
	if i.box != nil {
		err = i.box.Close()
	}
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
	if err := rejectClashAPI(raw); err != nil {
		return nil, err
	}
	// Detach from caller cancel (HTTP request context) so the dataplane outlives Apply.
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx := mergeBase(context.WithoutCancel(ctx), e.base)
	runCtx = registerDemuxInjectFeedsFresh(runCtx)

	var opt option.Options
	if err := opt.UnmarshalJSONContext(runCtx, raw); err != nil {
		return nil, fmt.Errorf("config invalid: %w", err)
	}
	b, err := singbox.New(singbox.Options{
		Context:           runCtx,
		Options:           opt,
		PlatformLogWriter: e.platformLogWriter,
	})
	if err != nil {
		return nil, fmt.Errorf("create box: %w", err)
	}
	if e.hook != nil {
		if router := b.Router(); router != nil {
			for _, tr := range e.hook.Trackers() {
				if tr != nil {
					router.AppendTracker(tr)
				}
			}
		}
	}
	if err := b.Start(); err != nil {
		_ = b.Close()
		return nil, fmt.Errorf("start box: %w", err)
	}
	done := make(chan struct{})
	inst := &instance{box: b, done: done, hook: e.hook}
	if e.hook != nil {
		e.hook.OnBoxStarted(runCtx)
	}
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


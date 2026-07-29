//go:build with_traffic

package traffic

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/sagernet/sing-box/adapter"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/portal"
	"github.com/ne-tort/sing-box-subserver/internal/traffic/service"
)

// Deps for constructing the module.
type Deps struct {
	DataDir       string
	FlushInterval time.Duration
	RetentionDays int
	AllowInject   bool
	Logger        *slog.Logger
}

// Module is the optional traffic accounting/shaping subsystem.
type Module struct {
	svc *service.Service
}

// New returns a traffic module or nil on error (caller should log).
func New(d Deps) *Module {
	svc, err := service.New(service.Config{
		DataDir:       d.DataDir,
		FlushInterval: d.FlushInterval,
		RetentionDays: d.RetentionDays,
		AllowInject:   d.AllowInject,
		Logger:        d.Logger,
	})
	if err != nil {
		if d.Logger != nil {
			d.Logger.Warn("traffic module init failed", "err", err)
		}
		return nil
	}
	return &Module{svc: svc}
}

// Enabled reports whether the module compiled in and constructed.
func Enabled() bool { return true }

func (m *Module) Service() *service.Service {
	if m == nil {
		return nil
	}
	return m.svc
}

// Trackers implements box.TrafficHook.
func (m *Module) Trackers() []adapter.ConnectionTracker {
	if m == nil || m.svc == nil {
		return nil
	}
	return m.svc.Trackers()
}

func (m *Module) OnBoxStarted(ctx context.Context) {
	if m != nil && m.svc != nil {
		m.svc.OnBoxStarted(ctx)
	}
}

func (m *Module) OnBoxStopped() {
	if m != nil && m.svc != nil {
		m.svc.OnBoxStopped()
	}
}

func (m *Module) Run(ctx context.Context) {
	if m != nil && m.svc != nil {
		m.svc.Run(ctx)
	}
}

func (m *Module) Register(mux *http.ServeMux, requireAuth func(func(http.ResponseWriter, *http.Request)) http.HandlerFunc) {
	if m == nil || m.svc == nil {
		return
	}
	portal.Register(mux, m.svc, requireAuth)
}

// Convenience pass-throughs for consumers.
func (m *Module) RegisterManifest(consumer string, subjects []domain.Subject) error {
	if m == nil || m.svc == nil {
		return nil
	}
	return m.svc.RegisterManifest(consumer, subjects)
}

func (m *Module) SetLimits(limits map[string]domain.SpeedLimit) {
	if m != nil && m.svc != nil {
		m.svc.SetManualLimits(limits)
	}
}

func (m *Module) SetCPLimits(limits map[string]domain.SpeedLimit) {
	if m != nil && m.svc != nil {
		m.svc.SetCPLimits(limits)
	}
}

func (m *Module) PollSubjectUsage(subjectID string) domain.Usage {
	if m == nil || m.svc == nil {
		return domain.Usage{SubjectID: subjectID}
	}
	return m.svc.PollSubjectUsage(subjectID)
}

func (m *Module) ZeroSubject(subjectID string) error {
	if m == nil || m.svc == nil {
		return nil
	}
	return m.svc.ZeroSubject(subjectID)
}

func (m *Module) SetSubjectUsage(subjectID string, total uint64) error {
	if m == nil || m.svc == nil {
		return nil
	}
	return m.svc.SetSubjectUsage(subjectID, total)
}

func (m *Module) Flush() error {
	if m == nil || m.svc == nil {
		return nil
	}
	return m.svc.Flush()
}

func (m *Module) CloseConnByUsers(keys []string) int {
	if m == nil || m.svc == nil {
		return 0
	}
	return m.svc.CloseConnByUsers(keys)
}

func (m *Module) DiscoverObservedUsers() []string {
	if m == nil || m.svc == nil {
		return nil
	}
	return m.svc.DiscoverObservedUsers()
}

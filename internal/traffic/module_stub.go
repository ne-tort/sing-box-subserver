//go:build !with_traffic

package traffic

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/sagernet/sing-box/adapter"

	"github.com/ne-tort/sing-box-subserver/internal/traffic/domain"
)

// Deps for constructing the module (ignored without with_traffic).
type Deps struct {
	DataDir       string
	FlushInterval time.Duration
	RetentionDays int
	Logger        *slog.Logger
}

// Module is a no-op stub without with_traffic.
type Module struct{}

func New(Deps) *Module { return nil }

func Enabled() bool { return false }

func (m *Module) Trackers() []adapter.ConnectionTracker { return nil }
func (m *Module) OnBoxStarted(context.Context)          {}
func (m *Module) OnBoxStopped()                         {}
func (m *Module) Run(context.Context)                   {}
func (m *Module) Register(mux *http.ServeMux, requireAuth func(func(http.ResponseWriter, *http.Request)) http.HandlerFunc) {
}
func (m *Module) RegisterManifest(string, []domain.Subject) error { return nil }
func (m *Module) SetLimits(map[string]domain.SpeedLimit)          {}
func (m *Module) PollSubjectUsage(subjectID string) domain.Usage {
	return domain.Usage{SubjectID: subjectID}
}
func (m *Module) ZeroSubject(string) error          { return nil }
func (m *Module) DiscoverObservedUsers() []string { return nil }
func (m *Module) Service() any                      { return nil }

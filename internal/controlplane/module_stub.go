//go:build !with_controlplane

package controlplane

import (
	"context"
	"net/http"
)

// New returns nil when the binary is built without with_controlplane.
func New(Deps) *Service { return nil }

// Service is a stub type so callers compile without the feature tag.
type Service struct{}

func (s *Service) Register(mux *http.ServeMux, requireAuth func(func(http.ResponseWriter, *http.Request)) http.HandlerFunc) {
}

func (s *Service) Run(context.Context) {}

func (s *Service) Bootstrap(context.Context) {}

func (s *Service) OnLeaveOwnership() {}

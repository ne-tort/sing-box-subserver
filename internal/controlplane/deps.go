package controlplane

import (
	"log/slog"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

// Deps are wiring inputs for New (both build tags).
type Deps struct {
	Cfg        *agentcfg.Config
	DataDir    string
	Supervisor *supervisor.Supervisor
	Owner      *configowner.Registry
	Logger     *slog.Logger
}

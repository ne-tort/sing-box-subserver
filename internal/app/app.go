package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/api"
	"github.com/ne-tort/sing-box-subserver/internal/auth"
	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configowner"
	"github.com/ne-tort/sing-box-subserver/internal/configstats"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/controlplane"
	"github.com/ne-tort/sing-box-subserver/internal/heartbeat"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/subscribe"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

// Options for Run.
type Options struct {
	ConfigPath        string
	ExitOnBootFailure bool
	// Context cancels the agent (tests). If nil, Interrupt/SIGTERM is used.
	Context context.Context
}

// Run loads config, boots supervisor, serves API until signal.
func Run(opts Options) error {
	cfg, err := agentcfg.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	o := obs.Setup(cfg.Log.Level)
	if cfg.InsecurePublicBind && !cfg.IsLoopbackListen() && !cfg.HasTLS() {
		o.Logger.Warn("insecure_public_bind enabled: management API without TLS on non-loopback", "listen", cfg.Listen)
	}

	store, err := configstore.New(cfg.DataDir)
	if err != nil {
		return err
	}
	engine := box.NewEngine(context.Background())
	sup := supervisor.NewWithOptions(store, engine, o.Logger, o.Metrics, supervisor.Options{
		Probe: cfg.ProbeDuration(),
	})
	sup.SetPullStatus(supervisor.PullStatus{
		Enabled:     cfg.Pull.Enabled,
		IntervalSec: cfg.Pull.IntervalSec,
	})

	parent := opts.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := sup.BootLastGood(ctx); err != nil && !errors.Is(err, supervisor.ErrNotFound) {
		o.Logger.Error("boot last-good failed", "err", err)
		if opts.ExitOnBootFailure {
			sup.Shutdown()
			return fmt.Errorf("boot failure: %w", err)
		}
	}

	srv := api.New(cfg, sup, o)
	creds, err := auth.Open(cfg.DataDir, cfg.Token)
	if err != nil {
		sup.Shutdown()
		return fmt.Errorf("auth store: %w", err)
	}
	srv.Auth = creds

	owner, err := configowner.Open(cfg.DataDir)
	if err != nil {
		sup.Shutdown()
		return fmt.Errorf("config owner: %w", err)
	}
	srv.Owner = owner

	subMgr := subscribe.New(cfg.DataDir, cfg.Token, sup)
	if err := subMgr.BootstrapFromYAML(cfg); err != nil {
		o.Logger.Warn("subscribe bootstrap failed", "err", err)
	}
	srv.Subscribe = subMgr

	cpSvc := controlplane.New(controlplane.Deps{
		Cfg:        cfg,
		DataDir:    cfg.DataDir,
		Supervisor: sup,
		Owner:      owner,
		Logger:     o.Logger,
	})
	if cpSvc != nil {
		srv.SetControlplane(cpSvc)
	}

	owner.SetHooks(configowner.Hooks{
		OnLeaveSubscribe: func() {
			subMgr.CancelOnDirectConfig()
		},
		OnLeaveControlplane: func() {
			if cpSvc != nil {
				cpSvc.OnLeaveOwnership()
			}
		},
	})

	hb := heartbeat.New(cfg.DataDir, cfg.NodeID, cfg.Listen, cfg.Token, sup)
	hb.SetInboundsCounter(func() int { return configstats.CountInbounds(sup) })
	hb.SetSubscribeStatus(func() any { return subMgr.Status() })
	hb.SetConfigMode(func() string { return string(owner.Owner()) })
	if err := hb.BootstrapFromYAML(cfg); err != nil {
		o.Logger.Warn("heartbeat bootstrap failed", "err", err)
	}
	srv.Heartbeat = hb

	// Subscribe/heartbeat may idle when disabled — that must not tear down the API.
	// Runtime state lives in data_dir; agent.yaml / install env are seed-only.
	go subMgr.Run(ctx)
	go hb.Run(ctx)
	if cpSvc != nil {
		go cpSvc.Run(ctx)
		cpSvc.Bootstrap(ctx)
	}

	apiErr := make(chan error, 1)
	go func() {
		o.Logger.Info("management API listening", "addr", cfg.Listen, "node_id", cfg.NodeID)
		apiErr <- srv.ListenAndServe(ctx)
	}()

	select {
	case <-ctx.Done():
		o.Logger.Info("shutting down")
		sup.Shutdown()
		return nil
	case err := <-apiErr:
		sup.Shutdown()
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
}

// Logger exposes slog for main if needed.
func Logger() *slog.Logger { return slog.Default() }

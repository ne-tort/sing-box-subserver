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
	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
	"github.com/ne-tort/sing-box-subserver/internal/pull"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

// Options for Run.
type Options struct {
	ConfigPath         string
	ExitOnBootFailure  bool
}

// Run loads config, boots supervisor, serves API until signal.
func Run(opts Options) error {
	cfg, err := agentcfg.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	o := obs.Setup(cfg.Log.Level)
	store, err := configstore.New(cfg.DataDir)
	if err != nil {
		return err
	}
	engine := box.NewEngine(context.Background())
	sup := supervisor.New(store, engine, o.Logger, o.Metrics)
	sup.SetPullStatus(supervisor.PullStatus{
		Enabled:     cfg.Pull.Enabled,
		IntervalSec: cfg.Pull.IntervalSec,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := sup.BootLastGood(ctx); err != nil && !errors.Is(err, supervisor.ErrNotFound) {
		o.Logger.Error("boot last-good failed", "err", err)
		if opts.ExitOnBootFailure {
			sup.Shutdown()
			return fmt.Errorf("boot failure: %w", err)
		}
	}

	srv := api.New(cfg, sup, o)
	puller := pull.New(cfg, sup)

	errCh := make(chan error, 2)
	go func() {
		o.Logger.Info("management API listening", "addr", cfg.Listen, "node_id", cfg.NodeID)
		errCh <- srv.ListenAndServe(ctx)
	}()
	go func() {
		puller.Run(ctx)
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		o.Logger.Info("shutting down")
		sup.Shutdown()
		return nil
	case err := <-errCh:
		sup.Shutdown()
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
}

// Logger exposes slog for main if needed.
func Logger() *slog.Logger { return slog.Default() }

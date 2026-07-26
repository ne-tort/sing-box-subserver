package pull

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/agentcfg"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/supervisor"
)

// Scheduler periodically GETs desired config and applies it.
type Scheduler struct {
	Cfg    *agentcfg.Config
	Sup    *supervisor.Supervisor
	Client *http.Client
}

func New(cfg *agentcfg.Config, sup *supervisor.Supervisor) *Scheduler {
	return &Scheduler{
		Cfg: cfg,
		Sup: sup,
		Client: &http.Client{
			Timeout: cfg.PullTimeout(),
		},
	}
}

// Run blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if !s.Cfg.Pull.Enabled {
		return
	}
	s.Sup.SetPullStatus(supervisor.PullStatus{
		Enabled:     true,
		IntervalSec: s.Cfg.Pull.IntervalSec,
	})

	// initial delay with jitter
	s.sleep(ctx, s.jittered())
	for {
		if err := s.tick(ctx); err != nil {
			s.Sup.ReportPullError(err)
		} else {
			s.Sup.ReportPullSuccess()
		}
		if !s.sleep(ctx, s.jittered()) {
			return
		}
	}
}

func (s *Scheduler) jittered() time.Duration {
	base := s.Cfg.PullInterval()
	j := s.Cfg.PullJitter()
	if j <= 0 {
		return base
	}
	return base + time.Duration(rand.Int63n(int64(j)))
}

func (s *Scheduler) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (s *Scheduler) tick(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.Cfg.Pull.URL, nil)
	if err != nil {
		return err
	}
	for k, v := range s.Cfg.Pull.Headers {
		req.Header.Set(k, v)
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull status %d: %s", resp.StatusCode, truncate(body, 200))
	}
	_, err = s.Sup.Apply(ctx, supervisor.ApplyRequest{
		Raw:    body,
		Source: configstore.SourcePull,
	})
	return err
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

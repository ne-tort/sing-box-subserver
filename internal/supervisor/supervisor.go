package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ne-tort/sing-box-subserver/internal/box"
	"github.com/ne-tort/sing-box-subserver/internal/configstore"
	"github.com/ne-tort/sing-box-subserver/internal/obs"
)

// State is the supervisor state machine value.
type State string

const (
	StateStopped    State = "Stopped"
	StateStarting   State = "Starting"
	StateRunning    State = "Running"
	StateApplying   State = "Applying"
	StateDegraded   State = "Degraded"
	StateRolledBack State = "RolledBack"
)

var (
	ErrConflict   = errors.New("apply in progress")
	ErrPrecondition = errors.New("if-match precondition failed")
	ErrNotFound   = errors.New("no last-good config")
	ErrInvalid    = errors.New("config invalid")
)

// MatchMode for If-Match.
type MatchMode int

const (
	MatchNone MatchMode = iota
	MatchRevision
	MatchSHA256
)

// ApplyRequest is one config apply attempt.
type ApplyRequest struct {
	Raw       []byte
	Source    configstore.Source
	MatchMode MatchMode
	MatchRev  uint64
	MatchSHA  string
}

// ApplyResult is returned to API.
type ApplyResult struct {
	Noop     bool
	Revision uint64
	SHA256   string
	State    State
}

// LastApply records the last apply attempt.
type LastApply struct {
	At     time.Time `json:"at"`
	Source string    `json:"source"`
	Result string    `json:"result"` // ok|fail|noop
	Error  *string   `json:"error"`
}

// PullStatus is mirrored into GET /v1/status.
type PullStatus struct {
	Enabled       bool       `json:"enabled"`
	IntervalSec   int        `json:"interval_sec"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     *string    `json:"last_error"`
}

// StatusSnapshot is the supervisor view for REST.
type StatusSnapshot struct {
	State           State     `json:"state"`
	Revision        uint64    `json:"revision"`
	ContentSHA256   string    `json:"content_sha256"`
	BoxStartedAt    *time.Time `json:"box_started_at"`
	LastApply       *LastApply `json:"last_apply"`
	LastError       *string   `json:"last_error"`
	Pull            PullStatus `json:"pull"`
}

// BoxEngine is the dataplane dependency (real or fake).
type BoxEngine interface {
	Validate(ctx context.Context, raw []byte) error
	Start(ctx context.Context, raw []byte) (box.Instance, error)
}

// Supervisor owns box lifecycle and last-good apply.
type Supervisor struct {
	store   *configstore.Store
	engine  BoxEngine
	log     *slog.Logger
	metrics *obs.Metrics

	mu            sync.Mutex
	state         State
	inst          box.Instance
	boxStartedAt  time.Time
	lastApply     *LastApply
	lastError     *string
	contentSHA    string
	revision      uint64
	applying      bool
	watchCancel   context.CancelFunc
	pull          PullStatus
	processStart  time.Time
	backoff       time.Duration
	stopWatch     chan struct{}
}

func New(store *configstore.Store, engine BoxEngine, log *slog.Logger, metrics *obs.Metrics) *Supervisor {
	if log == nil {
		log = slog.Default()
	}
	if metrics == nil {
		metrics = &obs.Metrics{}
	}
	s := &Supervisor{
		store:        store,
		engine:       engine,
		log:          log,
		metrics:      metrics,
		state:        StateStopped,
		processStart: time.Now().UTC(),
		backoff:      time.Second,
		stopWatch:    make(chan struct{}),
	}
	if rev, err := store.CurrentRevision(); err == nil {
		s.revision = rev
	}
	if raw, meta, err := store.ReadLastGood(); err == nil {
		s.contentSHA = meta.ContentSHA256
		s.revision = meta.Revision
		_ = raw
	}
	return s
}

func (s *Supervisor) ProcessStartedAt() time.Time { return s.processStart }

func (s *Supervisor) SetPullStatus(p PullStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pull = p
}

func (s *Supervisor) ReportPullSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.pull.LastSuccessAt = &now
	s.pull.LastError = nil
}

func (s *Supervisor) ReportPullError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := err.Error()
	s.pull.LastError = &msg
}

// Status returns a consistent snapshot.
func (s *Supervisor) Status() StatusSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var started *time.Time
	if !s.boxStartedAt.IsZero() && s.inst != nil {
		t := s.boxStartedAt
		started = &t
	}
	return StatusSnapshot{
		State:         s.state,
		Revision:      s.revision,
		ContentSHA256: s.contentSHA,
		BoxStartedAt:  started,
		LastApply:     s.lastApply,
		LastError:     s.lastError,
		Pull:          s.pull,
	}
}

// Validate only.
func (s *Supervisor) Validate(ctx context.Context, raw []byte) error {
	if err := s.engine.Validate(ctx, raw); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

// Apply runs the normative last-good pipeline.
func (s *Supervisor) Apply(ctx context.Context, req ApplyRequest) (ApplyResult, error) {
	s.mu.Lock()
	if s.applying {
		s.mu.Unlock()
		return ApplyResult{}, ErrConflict
	}
	s.applying = true
	prevState := s.state
	if s.state == StateRunning || s.state == StateRolledBack {
		s.state = StateApplying
	} else {
		s.state = StateStarting
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.applying = false
		s.mu.Unlock()
	}()

	res, err := s.applyLocked(ctx, req, prevState)
	s.recordApply(req.Source, res, err)
	return res, err
}

func (s *Supervisor) recordApply(source configstore.Source, res ApplyResult, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	la := &LastApply{At: now, Source: string(source)}
	if err != nil {
		la.Result = "fail"
		msg := err.Error()
		la.Error = &msg
		s.lastError = &msg
		s.metrics.IncApply(false)
	} else if res.Noop {
		la.Result = "noop"
		la.Error = nil
		s.metrics.IncApply(true)
	} else {
		la.Result = "ok"
		la.Error = nil
		s.lastError = nil
		s.metrics.IncApply(true)
	}
	s.lastApply = la
}

func (s *Supervisor) applyLocked(ctx context.Context, req ApplyRequest, prevState State) (ApplyResult, error) {
	if len(req.Raw) == 0 {
		s.setState(prevState)
		return ApplyResult{}, fmt.Errorf("%w: empty body", ErrInvalid)
	}
	if req.Source == "" {
		req.Source = configstore.SourcePush
	}

	s.mu.Lock()
	curRev := s.revision
	curSHA := s.contentSHA
	s.mu.Unlock()

	switch req.MatchMode {
	case MatchRevision:
		if req.MatchRev != curRev {
			s.setState(prevState)
			return ApplyResult{}, fmt.Errorf("%w: revision %d != %d", ErrPrecondition, req.MatchRev, curRev)
		}
	case MatchSHA256:
		if req.MatchSHA != curSHA {
			s.setState(prevState)
			return ApplyResult{}, fmt.Errorf("%w: sha mismatch", ErrPrecondition)
		}
	}

	sha := configstore.Hash(req.Raw)
	s.mu.Lock()
	same := sha == s.contentSHA && s.inst != nil && (prevState == StateRunning || prevState == StateRolledBack)
	s.mu.Unlock()
	if same {
		s.setState(StateRunning)
		return ApplyResult{Noop: true, Revision: curRev, SHA256: sha, State: StateRunning}, nil
	}

	if err := s.engine.Validate(ctx, req.Raw); err != nil {
		s.setState(prevState)
		return ApplyResult{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	if _, err := s.store.WriteStaged(req.Raw, req.Source); err != nil {
		s.setState(prevState)
		return ApplyResult{}, err
	}

	// Quiesce old
	old := s.takeInstance()
	if old != nil {
		_ = old.Close()
	}

	inst, err := s.engine.Start(ctx, req.Raw)
	if err != nil {
		s.metrics.RollbackTotal.Add(1)
		if restoreErr := s.restoreLastGood(ctx); restoreErr != nil {
			s.setState(StateStopped)
			return ApplyResult{}, fmt.Errorf("start failed: %v; restore failed: %w", err, restoreErr)
		}
		s.setState(StateRolledBack)
		return ApplyResult{}, fmt.Errorf("start failed, restored last-good: %w", err)
	}

	meta, err := s.store.PromoteStaged()
	if err != nil {
		_ = inst.Close()
		s.metrics.RollbackTotal.Add(1)
		if restoreErr := s.restoreLastGood(ctx); restoreErr != nil {
			s.setState(StateStopped)
			return ApplyResult{}, fmt.Errorf("promote failed: %v; restore failed: %w", err, restoreErr)
		}
		s.setState(StateRolledBack)
		return ApplyResult{}, fmt.Errorf("promote failed, restored last-good: %w", err)
	}

	s.installInstance(inst, meta.ContentSHA256, meta.Revision)
	s.setState(StateRunning)
	s.backoff = time.Second
	return ApplyResult{Revision: meta.Revision, SHA256: meta.ContentSHA256, State: StateRunning}, nil
}

func (s *Supervisor) restoreLastGood(ctx context.Context) error {
	raw, meta, err := s.store.ReadLastGood()
	if err != nil {
		return err
	}
	inst, err := s.engine.Start(ctx, raw)
	if err != nil {
		return err
	}
	s.installInstance(inst, meta.ContentSHA256, meta.Revision)
	return nil
}

func (s *Supervisor) takeInstance() box.Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.watchCancel != nil {
		s.watchCancel()
		s.watchCancel = nil
	}
	inst := s.inst
	s.inst = nil
	return inst
}

func (s *Supervisor) installInstance(inst box.Instance, sha string, rev uint64) {
	s.mu.Lock()
	if s.watchCancel != nil {
		s.watchCancel()
	}
	s.inst = inst
	s.contentSHA = sha
	s.revision = rev
	s.boxStartedAt = time.Now().UTC()
	watchCtx, cancel := context.WithCancel(context.Background())
	s.watchCancel = cancel
	s.mu.Unlock()
	go s.watch(watchCtx, inst)
}

func (s *Supervisor) setState(st State) {
	s.mu.Lock()
	s.state = st
	s.mu.Unlock()
}

func (s *Supervisor) watch(ctx context.Context, inst box.Instance) {
	select {
	case <-ctx.Done():
		return
	case <-inst.Done():
	}
	s.mu.Lock()
	if s.inst != inst {
		s.mu.Unlock()
		return
	}
	s.inst = nil
	s.state = StateDegraded
	s.metrics.BoxRestarts.Add(1)
	backoff := s.backoff
	s.mu.Unlock()

	s.log.Warn("box stopped unexpectedly; restarting last-good", "backoff", backoff.String())
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	if err := s.restoreLastGood(context.Background()); err != nil {
		s.log.Error("restart last-good failed", "err", err)
		s.setState(StateDegraded)
		s.mu.Lock()
		if s.backoff < 60*time.Second {
			s.backoff *= 2
			if s.backoff > 60*time.Second {
				s.backoff = 60 * time.Second
			}
		}
		s.mu.Unlock()
		return
	}
	s.setState(StateRunning)
}

// BootLastGood starts last-good if present without bumping revision.
func (s *Supervisor) BootLastGood(ctx context.Context) error {
	if !s.store.HasLastGood() {
		return ErrNotFound
	}
	s.mu.Lock()
	if s.applying {
		s.mu.Unlock()
		return ErrConflict
	}
	s.applying = true
	s.state = StateStarting
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.applying = false
		s.mu.Unlock()
	}()

	if err := s.restoreLastGood(ctx); err != nil {
		s.setState(StateStopped)
		msg := err.Error()
		s.mu.Lock()
		s.lastError = &msg
		s.mu.Unlock()
		return err
	}
	s.setState(StateRunning)
	s.backoff = time.Second
	return nil
}

// Shutdown closes the running box.
func (s *Supervisor) Shutdown() {
	inst := s.takeInstance()
	if inst != nil {
		_ = inst.Close()
	}
	s.setState(StateStopped)
}

// LastGoodConfig returns last-good bytes and meta.
func (s *Supervisor) LastGoodConfig() ([]byte, configstore.Meta, error) {
	return s.store.ReadLastGood()
}

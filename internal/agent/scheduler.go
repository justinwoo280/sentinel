package agent

import (
	"context"
	"log/slog"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/ctrl"
)

// moduleRunner is the subset of Agent methods the scheduler needs.
type moduleRunner interface {
	ModGoogle(ctx context.Context) ctrl.Result
	ModTrust(ctx context.Context) ctrl.Result
}

// Scheduler drives periodic keepalive cycles. It replaces the original
// project's cron/systemd-timer approach with an in-process ticker + jitter,
// and uses a mutex to prevent overlapping cycles.
type Scheduler struct {
	interval time.Duration
	jitter   time.Duration
	cfg      config.ModulesConfig
	log      *slog.Logger
	mu       sync.Mutex
	running  bool
	agent    moduleRunner
}

func NewScheduler(cfg config.AgentConfig, a *Agent, log *slog.Logger) *Scheduler {
	return &Scheduler{
		interval: time.Duration(cfg.Schedule.Interval),
		jitter:   time.Duration(cfg.Schedule.Jitter),
		cfg:      cfg.Modules,
		log:      log,
		agent:    a,
	}
}

// Run starts the periodic loop. Blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		result := s.RunOne(ctx)
		s.log.Info("keepalive cycle completed",
			"ok", result.OK, "msg", result.Msg)

		wait := s.interval
		if s.jitter > 0 && !isTerminal() {
			wait += time.Duration(rand.Intn(int(s.jitter)))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// RunOne executes a single keepalive cycle, picking a module via the
// probability wheel (70% google / 30% trust when both enabled). Returns
// the result of the chosen module.
func (s *Scheduler) RunOne(ctx context.Context) ctrl.Result {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ctrl.Result{OK: false, Msg: "previous cycle still running"}
	}
	s.running = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	if s.cfg.Google && s.cfg.Trust {
		if rand.Intn(100) < 70 {
			return s.agent.ModGoogle(ctx)
		}
		return s.agent.ModTrust(ctx)
	} else if s.cfg.Google {
		return s.agent.ModGoogle(ctx)
	} else if s.cfg.Trust {
		return s.agent.ModTrust(ctx)
	}
	return ctrl.Result{OK: false, Msg: "no modules enabled"}
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

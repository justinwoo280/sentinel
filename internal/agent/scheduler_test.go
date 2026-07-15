package agent

import (
	"context"
	"testing"

	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/ctrl"
)

func TestSchedulerProbabilityWheel(t *testing.T) {
	cfg := config.DefaultAgent()
	cfg.Node.Name = "test"
	cfg.Region.Code = "US"
	cfg.Modules = config.ModulesConfig{Google: true, Trust: true}
	cfg.Schedule.Interval = config.Duration(0)
	cfg.Schedule.Jitter = config.Duration(0)

	// We can't create a full Agent without geo/net deps, so test
	// the scheduler's RunOne directly with a minimal mock agent.
	s := &Scheduler{
		interval: 0,
		jitter:   0,
		cfg:      cfg.Modules,
		agent:    &mockRunner{},
	}

	// With both modules enabled, RunOne should pick one.
	// We can't test exact probabilities, but we can verify it
	// doesn't panic and returns a valid result.
	result := s.RunOne(context.Background())
	if result.Msg == "" {
		t.Fatal("expected non-empty result message")
	}
}

func TestSchedulerNoModules(t *testing.T) {
	s := &Scheduler{
		cfg:   config.ModulesConfig{Google: false, Trust: false},
		agent: &mockRunner{},
	}
	result := s.RunOne(context.Background())
	if result.OK {
		t.Fatal("expected OK=false with no modules")
	}
}

func TestSchedulerOnlyGoogle(t *testing.T) {
	s := &Scheduler{
		cfg:   config.ModulesConfig{Google: true, Trust: false},
		agent: &mockRunner{},
	}
	result := s.RunOne(context.Background())
	if result.Msg != "google" {
		t.Fatalf("expected google, got %q", result.Msg)
	}
}

func TestSchedulerOnlyTrust(t *testing.T) {
	s := &Scheduler{
		cfg:   config.ModulesConfig{Google: false, Trust: true},
		agent: &mockRunner{},
	}
	result := s.RunOne(context.Background())
	if result.Msg != "trust" {
		t.Fatalf("expected trust, got %q", result.Msg)
	}
}

// mockRunner is a minimal moduleRunner for testing the scheduler
// without geo/net dependencies.
type mockRunner struct{}

func (m *mockRunner) ModGoogle(ctx context.Context) ctrl.Result {
	return ctrl.Result{OK: true, Msg: "google"}
}
func (m *mockRunner) ModTrust(ctx context.Context) ctrl.Result {
	return ctrl.Result{OK: true, Msg: "trust"}
}

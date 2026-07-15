package agent

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/justinwoo280/sentinel/internal/config"
)

// TestNewAgentCapturesLogs is an end-to-end regression test for the bug
// where the Master's `log` command always returned "(no logs captured
// yet)" because module loggers (google/trust/scheduler/ctrl) never fed
// the agentlog ring buffer — only Report() pushed a single hardcoded
// entry. After New() runs (which itself logs "region data loaded" and
// other bootstrap messages through the wrapped logger), Log() must
// return real content.
func TestNewAgentCapturesLogs(t *testing.T) {
	cfg := config.DefaultAgent()
	cfg.Node.Name = "test-node"
	cfg.Region.Code = "JP"
	cfg.Region.State = "Default"
	cfg.Region.City = "Tokyo"
	cfg.Master.Enabled = false
	cfg.GeoIP.Enabled = false

	log := slog.New(slog.NewTextHandler(discardWriter{}, nil))
	a, err := New(cfg, "", log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	result := a.Log(context.Background())
	if !result.OK {
		t.Fatalf("Log() returned OK=false: %s", result.Msg)
	}
	if strings.Contains(result.Msg, "no logs captured yet") {
		t.Fatalf("Log() still reports no captured logs after New(); "+
			"the agentlog handler is not wired up:\n%s", result.Msg)
	}
	if !strings.Contains(result.Msg, "region data loaded") {
		t.Fatalf("Log() output missing the bootstrap 'region data loaded' entry logged by New():\n%s", result.Msg)
	}
}

// TestReportGoesIntoLog verifies Report() no longer needs a hardcoded
// buffer push — its own log.Info call must be captured automatically.
func TestReportGoesIntoLog(t *testing.T) {
	cfg := config.DefaultAgent()
	cfg.Node.Name = "test-node"
	cfg.Node.Alias = "Test"
	cfg.Region.Code = "JP"
	cfg.Region.State = "Default"
	cfg.Region.City = "Tokyo"
	cfg.Master.Enabled = false
	cfg.GeoIP.Enabled = false

	log := slog.New(slog.NewTextHandler(discardWriter{}, nil))
	a, err := New(cfg, "", log)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_ = a.Report(context.Background())

	result := a.Log(context.Background())
	if !strings.Contains(result.Msg, "report generated") {
		t.Fatalf("Log() missing report-generated entry after Report():\n%s", result.Msg)
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

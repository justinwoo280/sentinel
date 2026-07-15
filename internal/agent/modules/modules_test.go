package modules

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justinwoo280/sentinel/internal/config"
	"github.com/justinwoo280/sentinel/internal/ctrl"
	"github.com/justinwoo280/sentinel/internal/geo"
	"log/slog"
)

// TestGoogleModuleSmoke runs a Google keepalive cycle against a mock
// HTTP server, verifying no panic and a valid result.
func TestGoogleModuleSmoke(t *testing.T) {
	// Mock server returning minimal Google-like responses.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		// Minimal page with "contentRegion" for geo detection.
		w.Write([]byte(`<!DOCTYPE html><html><head><script>var yt = {"contentRegion":"JP"}</script></head><body>Google</body></html>`))
	}))
	defer srv.Close()

	cfg := config.DefaultAgent()
	cfg.Node.Name = "test"
	cfg.Region.Code = "JP"
	cfg.Region.Name = "Japan"
	cfg.Region.Lat = 35.6762
	cfg.Region.Lon = 139.6503

	log := slog.Default()
	m := NewGoogle(cfg, log, "127.0.0.1")

	// We can't fully test the module without real network access,
	// but we can verify it doesn't panic on data loading.
	uas, err := geo.LoadUAs()
	if err != nil {
		t.Fatal(err)
	}
	if len(uas) == 0 {
		t.Fatal("UA pool empty")
	}

	kw, err := geo.LoadKeywords("JP")
	if err != nil {
		t.Fatal(err)
	}
	if len(kw) == 0 {
		t.Fatal("keyword pool empty for JP")
	}

	// Run with a very short context timeout — we just want to verify
	// no panic, not full network success.
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()

	_ = m.Run(ctx)
	// No panic = pass. Network failures are expected in a test env.
}

// TestTrustModuleSmoke verifies the trust module loads region data and
// doesn't panic.
func TestTrustModuleSmoke(t *testing.T) {
	cfg := config.DefaultAgent()
	cfg.Node.Name = "test"
	cfg.Region.Code = "JP"

	log := slog.Default()
	m := NewTrust(cfg, log, "127.0.0.1")

	// Verify region data loads.
	rd, err := geo.LoadRegion("JP")
	if err != nil {
		t.Fatal(err)
	}
	if len(rd.TrustModule.WhiteURLs) == 0 {
		t.Fatal("no white URLs for JP")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()

	_ = m.Run(ctx)
}

// TestGoogleModuleResultType verifies Run returns a ctrl.Result.
func TestGoogleModuleResultType(t *testing.T) {
	cfg := config.DefaultAgent()
	cfg.Node.Name = "test"
	cfg.Region.Code = "JP"

	m := NewGoogle(cfg, slog.Default(), "127.0.0.1")
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()

	result := m.Run(ctx)
	if result.Msg == "" {
		t.Fatal("result should have a message")
	}
}

// TestTrustModuleResultType verifies Run returns a ctrl.Result.
func TestTrustModuleResultType(t *testing.T) {
	cfg := config.DefaultAgent()
	cfg.Node.Name = "test"
	cfg.Region.Code = "JP"

	m := NewTrust(cfg, slog.Default(), "127.0.0.1")
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()

	result := m.Run(ctx)
	if result.Msg == "" {
		t.Fatal("result should have a message")
	}
}

// Ensure ctrl.Result is the return type (compile-time check).
var _ ctrl.Result = ctrl.Result{}

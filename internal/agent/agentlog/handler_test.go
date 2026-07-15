package agentlog

import (
	"log/slog"
	"strings"
	"testing"
)

// TestHandlerCapturesIntoBuffer is a regression test for the bug where
// module logs (google/trust/quality/scheduler/ctrl) never reached the
// ring buffer, so the Master's `log` command always reported "no logs
// captured yet". A logger built on this Handler must push every record
// into the Buffer.
func TestHandlerCapturesIntoBuffer(t *testing.T) {
	buf := New(50)
	var sb strings.Builder
	next := slog.NewTextHandler(&sb, nil)
	h := NewHandler(next, buf)
	log := slog.New(h)

	log.Info("google session starting", "platform", "ios", "ip", "1.2.3.4")
	log.Warn("action failed", "i", 3)

	entries := buf.All()
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Level != "INFO" {
		t.Errorf("entry[0] level = %q, want INFO", entries[0].Level)
	}
	if !strings.Contains(entries[0].Msg, "google session starting") {
		t.Errorf("entry[0] msg = %q, missing base message", entries[0].Msg)
	}
	if !strings.Contains(entries[0].Msg, "platform=ios") {
		t.Errorf("entry[0] msg = %q, missing attrs", entries[0].Msg)
	}
	if entries[1].Level != "WARN" {
		t.Errorf("entry[1] level = %q, want WARN", entries[1].Level)
	}

	// The underlying handler must still receive the record (normal
	// stdout/journalctl output is not lost).
	if !strings.Contains(sb.String(), "google session starting") {
		t.Errorf("underlying handler did not receive the record: %q", sb.String())
	}
}

// TestHandlerModuleTag verifies log.With("module", "google") propagates
// the module tag into captured entries (and isn't duplicated as a
// regular attr in the message).
func TestHandlerModuleTag(t *testing.T) {
	buf := New(50)
	next := slog.NewTextHandler(&strings.Builder{}, nil)
	h := NewHandler(next, buf)
	log := slog.New(h).With("module", "google")

	log.Info("session starting", "ip", "1.2.3.4")

	entries := buf.All()
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Module != "google" {
		t.Errorf("module = %q, want google", entries[0].Module)
	}
	if strings.Contains(entries[0].Msg, "module=") {
		t.Errorf("module leaked into message: %q", entries[0].Msg)
	}
	if !strings.Contains(entries[0].Msg, "ip=1.2.3.4") {
		t.Errorf("missing ip attr: %q", entries[0].Msg)
	}
}

// TestHandlerWithGroup verifies WithGroup doesn't panic and still
// forwards to the underlying handler.
func TestHandlerWithGroup(t *testing.T) {
	buf := New(50)
	next := slog.NewTextHandler(&strings.Builder{}, nil)
	h := NewHandler(next, buf)
	log := slog.New(h).WithGroup("g")
	log.Info("grouped message")

	if len(buf.All()) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(buf.All()))
	}
}

// TestHandlerEnabledDelegates verifies level filtering is delegated to
// the underlying handler (e.g. a Debug-filtered handler still filters
// through this wrapper).
func TestHandlerEnabledDelegates(t *testing.T) {
	buf := New(50)
	next := slog.NewTextHandler(&strings.Builder{}, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := NewHandler(next, buf)
	log := slog.New(h)

	log.Info("should be filtered out by the underlying handler's level")
	log.Warn("should pass")

	entries := buf.All()
	// slog.Logger checks Enabled() before calling Handle, so Info here
	// should not even reach our Handle() / the buffer.
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (only the Warn)", len(entries))
	}
	if entries[0].Level != "WARN" {
		t.Errorf("entry level = %q, want WARN", entries[0].Level)
	}
}

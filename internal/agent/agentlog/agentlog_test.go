package agentlog

import (
	"testing"
)

func TestBufferPushAndRecent(t *testing.T) {
	buf := New(5)

	for i := 0; i < 7; i++ {
		buf.Push("INFO", "test", "message "+string(rune('a'+i)))
	}

	// Should only have the last 5.
	recent := buf.Recent(10)
	if len(recent) != 5 {
		t.Fatalf("got %d entries, want 5", len(recent))
	}

	// Should be oldest first.
	if recent[0].Msg != "message c" {
		t.Fatalf("first entry: got %q, want 'message c'", recent[0].Msg)
	}
	if recent[4].Msg != "message g" {
		t.Fatalf("last entry: got %q, want 'message g'", recent[4].Msg)
	}
}

func TestBufferRecentN(t *testing.T) {
	buf := New(100)
	for i := 0; i < 10; i++ {
		buf.Push("INFO", "mod", "msg")
	}
	recent := buf.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("got %d, want 3", len(recent))
	}
}

func TestBufferEmpty(t *testing.T) {
	buf := New(5)
	recent := buf.Recent(10)
	if len(recent) != 0 {
		t.Fatalf("empty buffer should return 0 entries, got %d", len(recent))
	}
}

func TestBufferFormat(t *testing.T) {
	buf := New(10)
	buf.Push("INFO", "google", "session started")
	buf.Push("WARN", "trust", "visit failed")

	formatted := buf.Format(10)
	if formatted == "" {
		t.Fatal("formatted output should not be empty")
	}
	if !contains(formatted, "google") {
		t.Fatal("formatted output should contain 'google'")
	}
	if !contains(formatted, "trust") {
		t.Fatal("formatted output should contain 'trust'")
	}
	if !contains(formatted, "WARN") {
		t.Fatal("formatted output should contain 'WARN'")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

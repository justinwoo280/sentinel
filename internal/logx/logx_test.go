package logx

import (
	"log/slog"
	"testing"
)

func TestNew(t *testing.T) {
	l := New()
	if l == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNewText(t *testing.T) {
	l := NewText()
	if l == nil {
		t.Fatal("NewText() returned nil")
	}
}

func TestWithModule(t *testing.T) {
	l := New()
	child := WithModule(l, "google")
	if child == nil {
		t.Fatal("WithModule returned nil")
	}
}

func TestWithModuleRegion(t *testing.T) {
	l := New()
	child := WithModuleRegion(l, "google", "JP")
	if child == nil {
		t.Fatal("WithModuleRegion returned nil")
	}
}

func TestLevelFromEnv(t *testing.T) {
	// Default is info.
	if got := levelFromEnv(); got != slog.LevelInfo {
		t.Fatalf("default level: got %v, want info", got)
	}
}

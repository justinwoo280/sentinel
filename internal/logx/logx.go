// Package logx provides unified structured logging for the Sentinel agent
// and master (DESIGN.md §2.2, §附录A: time(UTC) [LEVEL] [module] [region] msg).
//
// It wraps log/slog with project-specific defaults: UTC timestamps, a
// consistent handler, and helper context fields for module/region tagging.
package logx

import (
	"log/slog"
	"os"
	"strings"
)

// Level wraps slog.Level for convenience.
type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// New creates a slog.Logger with project defaults: JSON handler, UTC time,
// level from env or INFO default.
func New() *slog.Logger {
	return NewWithLevel(levelFromEnv())
}

// NewWithLevel creates a slog.Logger at the given level with UTC timestamps.
func NewWithLevel(level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level:     level,
		AddSource: false,
	})
	return slog.New(h)
}

// NewText creates a text-format logger for human-readable console output
// (useful during development or interactive install).
func NewText() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: levelFromEnv(),
	}))
}

// WithModule returns a child logger tagged with the module name.
func WithModule(log *slog.Logger, module string) *slog.Logger {
	return log.With("module", module)
}

// WithModuleRegion returns a child logger tagged with both module and region,
// matching the DESIGN.md log format: time(UTC) [LEVEL] [module] [region] msg.
func WithModuleRegion(log *slog.Logger, module, region string) *slog.Logger {
	return log.With("module", module, "region", region)
}

func levelFromEnv() slog.Level {
	v := strings.ToLower(os.Getenv("SENTINEL_LOG_LEVEL"))
	switch v {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

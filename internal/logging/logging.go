package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Setup configures the global slog.Default() logger with the given format and level.
// format: "text" (human-readable) or "json" (structured, for Datadog/Grafana Alloy).
// level: "debug", "info", "warn", "error".
// Returns the configured *slog.Logger.
func Setup(format, level string) *slog.Logger {
	lvl := ParseLevel(level)
	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, opts)
	default:
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// ParseLevel converts a level string to slog.Level.
// Defaults to slog.LevelInfo for unrecognized values.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Discard returns a *slog.Logger that discards all output.
// Useful for tests that don't need log output.
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

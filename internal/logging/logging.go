// Package logging configures structured diagnostic logging.
//
// Nexus keeps user-facing output (internal/ui) and diagnostics strictly apart:
// diagnostics are structured slog records written to stderr only when asked
// for with --verbose or NEXUS_LOG, so they never pollute pipelines.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// Setup installs the default slog logger. verbose forces debug level;
// otherwise NEXUS_LOG (debug|info|warn|error) decides, defaulting to silence.
func Setup(w io.Writer, verbose bool) {
	level, enabled := levelFromEnv(os.Getenv("NEXUS_LOG"))
	if verbose {
		level, enabled = slog.LevelDebug, true
	}
	if !enabled {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return
	}
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h).With("app", "nexus"))
}

func levelFromEnv(v string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}

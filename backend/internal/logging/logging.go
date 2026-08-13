// Package logging configures the process-wide slog handler.
//
// Person C owns request-scoped structured logging in platform/middleware; this
// only establishes a sane default handler so domain code can call slog directly
// instead of dropping errors on the floor.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Setup installs the default slog handler. level accepts debug|info|warn|error
// (default info) and format accepts json|text (default text, which is easier to
// read in local development).
func Setup(level, format string) {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.EqualFold(strings.TrimSpace(format), "json") {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

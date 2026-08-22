package logging

import (
	"io"
	"log/slog"
	"strings"

	"github.com/tktsync/tktsync/backend/internal/platform/config"
)

func New(out io.Writer, service string, cfg config.Config) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(strings.ToUpper(cfg.Logging.Level)))
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Logging.Format == "text" {
		handler = slog.NewTextHandler(out, opts)
	} else {
		handler = slog.NewJSONHandler(out, opts)
	}
	return slog.New(handler).With("service", service, "environment", cfg.Environment)
}

// Sensitive configuration values must never be attached to log records. Log only
// stable identifiers, operation names, durations, and redacted error context.

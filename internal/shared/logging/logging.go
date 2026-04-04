// Package logging wraps digital.vasic.observability/pkg/logging with
// HelixLLM-specific constructors.
package logging

import (
	"io"
	"os"

	obslog "digital.vasic.observability/pkg/logging"
)

// Logger is the structured logger interface from the observability module.
type Logger = obslog.Logger

// levelFor converts a string level name to an obslog.Level.
func levelFor(level string) obslog.Level {
	switch level {
	case "debug":
		return obslog.DebugLevel
	case "warn", "warning":
		return obslog.WarnLevel
	case "error":
		return obslog.ErrorLevel
	default:
		return obslog.InfoLevel
	}
}

// New returns a Logger that writes to stderr at the given level and format.
func New(level, format string) Logger {
	return NewWithOutput(level, format, os.Stderr)
}

// NewWithOutput returns a Logger that writes to the supplied writer.
func NewWithOutput(level, format string, output io.Writer) Logger {
	cfg := &obslog.Config{
		Level:  levelFor(level),
		Format: format,
		Output: output,
	}
	return obslog.NewLogrusAdapter(cfg)
}

// Package log provides structured logging contracts for Karpo services.
package log

import (
	"context"
)

// Level defines logging severity.
type Level int

const (
	LevelDebug Level = -4
	LevelInfo  Level = 0
	LevelWarn  Level = 4
	LevelError Level = 8
)

// Logger is the domain contract for structured logging.
// Implementations can wrap slog, zap, log4net equivalent, or in-memory capture for tests.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	With(args ...any) Logger
	WithContext(ctx context.Context) Logger
}

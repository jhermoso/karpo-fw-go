package log

import (
	"context"
	"log/slog"
)

// Default returns a Logger over slog.Default(), the logger of the process. Framework pieces
// that must never be silent (the outbox relay) fall back to it when no logger is given. It adds
// nothing from the context: use log/vanilla for a logger that does.
func Default() Logger { return std{} }

// Discard returns a Logger that writes nothing, to silence a piece on purpose.
func Discard() Logger { return discard{} }

type std struct {
	args []any
}

func (s std) log(level slog.Level, msg string, args []any) {
	slog.Default().Log(context.Background(), level, msg, append(append([]any(nil), s.args...), args...)...)
}

func (s std) Debug(msg string, args ...any)      { s.log(slog.LevelDebug, msg, args) }
func (s std) Info(msg string, args ...any)       { s.log(slog.LevelInfo, msg, args) }
func (s std) Warn(msg string, args ...any)       { s.log(slog.LevelWarn, msg, args) }
func (s std) Error(msg string, args ...any)      { s.log(slog.LevelError, msg, args) }
func (s std) WithContext(context.Context) Logger { return s }
func (s std) With(args ...any) Logger {
	return std{args: append(append([]any(nil), s.args...), args...)}
}

type discard struct{}

func (discard) Debug(string, ...any)                 {}
func (discard) Info(string, ...any)                  {}
func (discard) Warn(string, ...any)                  {}
func (discard) Error(string, ...any)                 {}
func (d discard) With(...any) Logger                 { return d }
func (d discard) WithContext(context.Context) Logger { return d }

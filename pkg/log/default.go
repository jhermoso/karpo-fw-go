package log

import (
	"context"
	"log/slog"
)

// Default returns a Logger over slog.Default(), the logger of the process. Framework pieces
// that must never be silent (the outbox relay) fall back to it when no logger is given. It hands
// the context of WithContext to slog, so a default handler that reads it (log/vanilla installed
// with slog.SetDefault) adds the correlation and the trace; the plain slog handler ignores it.
func Default() Logger { return std{ctx: context.Background()} }

// Discard returns a Logger that writes nothing, to silence a piece on purpose.
func Discard() Logger { return discard{} }

type std struct {
	args []any
	ctx  context.Context
}

func (s std) log(level slog.Level, msg string, args []any) {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	slog.Default().Log(ctx, level, msg, append(append([]any(nil), s.args...), args...)...)
}

func (s std) Debug(msg string, args ...any) { s.log(slog.LevelDebug, msg, args) }
func (s std) Info(msg string, args ...any)  { s.log(slog.LevelInfo, msg, args) }
func (s std) Warn(msg string, args ...any)  { s.log(slog.LevelWarn, msg, args) }
func (s std) Error(msg string, args ...any) { s.log(slog.LevelError, msg, args) }
func (s std) WithContext(ctx context.Context) Logger {
	return std{args: s.args, ctx: ctx}
}
func (s std) With(args ...any) Logger {
	return std{args: append(append([]any(nil), s.args...), args...), ctx: s.ctx}
}

type discard struct{}

func (discard) Debug(string, ...any)                 {}
func (discard) Info(string, ...any)                  {}
func (discard) Warn(string, ...any)                  {}
func (discard) Error(string, ...any)                 {}
func (d discard) With(...any) Logger                 { return d }
func (d discard) WithContext(context.Context) Logger { return d }

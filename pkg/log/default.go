package log

import (
	"context"
	"log/slog"
)

// Default returns a Logger over the process-wide slog logger (slog.Default, resolved on every
// call, so slog.SetDefault made later still applies). Components that were given no logger use
// it instead of staying silent: a failure must leave a trace even when nobody wired a logger.
func Default() Logger { return slogLogger{ctx: context.Background()} }

type slogLogger struct {
	args []any
	ctx  context.Context
}

func (l slogLogger) log(level slog.Level, msg string, args []any) {
	logger := slog.Default()
	if len(l.args) > 0 {
		logger = logger.With(l.args...)
	}
	logger.Log(l.ctx, level, msg, args...)
}

func (l slogLogger) Debug(msg string, args ...any) { l.log(slog.LevelDebug, msg, args) }
func (l slogLogger) Info(msg string, args ...any)  { l.log(slog.LevelInfo, msg, args) }
func (l slogLogger) Warn(msg string, args ...any)  { l.log(slog.LevelWarn, msg, args) }
func (l slogLogger) Error(msg string, args ...any) { l.log(slog.LevelError, msg, args) }

func (l slogLogger) With(args ...any) Logger {
	return slogLogger{args: append(append([]any(nil), l.args...), args...), ctx: l.ctx}
}

func (l slogLogger) WithContext(ctx context.Context) Logger {
	return slogLogger{args: l.args, ctx: ctx}
}

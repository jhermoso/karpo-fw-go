package log

import (
	"context"
	"log/slog"
)

// Default devuelve un Logger sobre slog.Default(), el logger del proceso. Las piezas del framework
// que nunca deben quedar en silencio (el relay del outbox) recurren a él cuando no se les da logger.
// Pasa a slog el contexto de WithContext, de modo que un handler por defecto que lo lea (log/vanilla
// instalado con slog.SetDefault) añade la correlación y la traza; el handler simple de slog lo ignora.
func Default() Logger { return std{ctx: context.Background()} }

// Discard devuelve un Logger que no escribe nada, para silenciar una pieza a propósito.
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

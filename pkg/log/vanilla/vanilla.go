// Package vanilla provides an implementation of log.Logger using the Go standard library log/slog.
package vanilla

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

// VanillaLogger wraps slog.Logger to satisfy log.Logger contract.
type VanillaLogger struct {
	logger *slog.Logger
	ctx    context.Context
}

// NewJSON creates a VanillaLogger writing JSON structured logs to w.
func NewJSON(w io.Writer, minLevel log.Level) *VanillaLogger {
	opts := &slog.HandlerOptions{
		Level: slog.Level(minLevel),
	}
	handler := slog.NewJSONHandler(w, opts)
	return &VanillaLogger{
		logger: slog.New(handler),
		ctx:    context.Background(),
	}
}

// NewText creates a VanillaLogger writing human-readable text logs to w.
func NewText(w io.Writer, minLevel log.Level) *VanillaLogger {
	opts := &slog.HandlerOptions{
		Level: slog.Level(minLevel),
	}
	handler := slog.NewTextHandler(w, opts)
	return &VanillaLogger{
		logger: slog.New(handler),
		ctx:    context.Background(),
	}
}

// Default creates a VanillaLogger writing text to stdout with Info level.
func Default() *VanillaLogger {
	return NewText(os.Stdout, log.LevelInfo)
}

func (l *VanillaLogger) Debug(msg string, args ...any) {
	l.logger.DebugContext(l.ctx, msg, args...)
}

func (l *VanillaLogger) Info(msg string, args ...any) {
	l.logger.InfoContext(l.ctx, msg, args...)
}

func (l *VanillaLogger) Warn(msg string, args ...any) {
	l.logger.WarnContext(l.ctx, msg, args...)
}

func (l *VanillaLogger) Error(msg string, args ...any) {
	l.logger.ErrorContext(l.ctx, msg, args...)
}

func (l *VanillaLogger) With(args ...any) log.Logger {
	return &VanillaLogger{
		logger: l.logger.With(args...),
		ctx:    l.ctx,
	}
}

func (l *VanillaLogger) WithContext(ctx context.Context) log.Logger {
	return &VanillaLogger{
		logger: l.logger,
		ctx:    ctx,
	}
}

var _ log.Logger = (*VanillaLogger)(nil)

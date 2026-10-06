// Package vanilla provides an implementation of log.Logger using the Go standard library log/slog.
//
// Every record logged with a context (Logger.WithContext) is stamped with the correlation of the
// flow and the current span: correlation_id, causation_id, trace_id and span_id, the field names
// shared with the C# framework (see observability/names.go).
package vanilla

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/observability"
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
	handler := Enrich(slog.NewJSONHandler(w, opts))
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
	handler := Enrich(slog.NewTextHandler(w, opts))
	return &VanillaLogger{
		logger: slog.New(handler),
		ctx:    context.Background(),
	}
}

// Default creates a VanillaLogger writing text to stdout with Info level.
func Default() *VanillaLogger {
	return NewText(os.Stdout, log.LevelInfo)
}

// Environment variables read by FromEnv.
const (
	EnvLogFormat = "KARPO_LOG_FORMAT" // json (default) | text
	EnvLogLevel  = "KARPO_LOG_LEVEL"  // debug | info (default) | warn | error
)

// FromEnv creates the logger of a service process: one JSON object per line on stdout (what a
// container log driver expects), at the level of KARPO_LOG_LEVEL, every line carrying
// service.name. KARPO_LOG_FORMAT=text switches to text for a developer console.
func FromEnv(service string) *VanillaLogger {
	return fromEnv(os.Stdout, os.Getenv, service)
}

func fromEnv(w io.Writer, getenv func(string) string, service string) *VanillaLogger {
	level := ParseLevel(getenv(EnvLogLevel))
	var l *VanillaLogger
	if strings.EqualFold(strings.TrimSpace(getenv(EnvLogFormat)), "text") {
		l = NewText(w, level)
	} else {
		l = NewJSON(w, level)
	}
	if service != "" {
		l.logger = l.logger.With(observability.FieldService, service)
	}
	return l
}

// ParseLevel parses debug, info, warn(ing) or error (any case); anything else is info.
func ParseLevel(s string) log.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return log.LevelDebug
	case "warn", "warning":
		return log.LevelWarn
	case "error":
		return log.LevelError
	}
	return log.LevelInfo
}

// Slog returns the underlying slog logger (e.g. to install it with slog.SetDefault, so that
// log.Default and the standard library log package write through it).
func (l *VanillaLogger) Slog() *slog.Logger { return l.logger }

// Enrich wraps h so that every record handled with a context gets correlation_id, causation_id,
// trace_id and span_id from it. A field the record (or a With on the logger) already sets is
// not added again, so a JSON line never carries a key twice.
func Enrich(h slog.Handler) slog.Handler { return &contextHandler{inner: h} }

type contextHandler struct {
	inner slog.Handler
	own   map[string]bool // top-level keys already set with WithAttrs
	group bool
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if ctx == nil || h.group {
		return h.inner.Handle(ctx, r)
	}
	present := map[string]bool{}
	r.Attrs(func(a slog.Attr) bool { present[a.Key] = true; return true })
	add := func(key, value string) {
		if value != "" && !present[key] && !h.own[key] {
			r.AddAttrs(slog.String(key, value))
		}
	}
	add(observability.FieldCorrelationID, application.CorrelationID(ctx))
	add(observability.FieldCausationID, application.CausationID(ctx))
	traceID, spanID := observability.TraceIDs(ctx)
	add(observability.FieldTraceID, traceID)
	add(observability.FieldSpanID, spanID)
	return h.inner.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	own := make(map[string]bool, len(h.own)+len(attrs))
	for k := range h.own {
		own[k] = true
	}
	if !h.group {
		for _, a := range attrs {
			own[a.Key] = true
		}
	}
	return &contextHandler{inner: h.inner.WithAttrs(attrs), own: own, group: h.group}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	// Inside a group the fields would be nested and no longer found by name: stop enriching.
	return &contextHandler{inner: h.inner.WithGroup(name), own: h.own, group: true}
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

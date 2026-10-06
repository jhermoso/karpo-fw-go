// Package vanilla provides an implementation of log.Logger using the Go standard library log/slog.
//
// Every line is enriched from the context given to WithContext: the correlation and causation
// ids of the business flow (application.WithCorrelationID / WithCausationID), the current span
// (trace.FromContext) and the id of the actor (application.WithActor). Callers do not pass them
// by hand; an attribute passed explicitly with the same key wins.
package vanilla

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

// Attribute keys added from the context.
const (
	CorrelationIDKey = "correlation_id"
	CausationIDKey   = "causation_id"
	ActorKey         = "actor"
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
	return New(slog.NewJSONHandler(w, opts))
}

// NewText creates a VanillaLogger writing human-readable text logs to w.
func NewText(w io.Writer, minLevel log.Level) *VanillaLogger {
	opts := &slog.HandlerOptions{
		Level: slog.Level(minLevel),
	}
	return New(slog.NewTextHandler(w, opts))
}

// New creates a VanillaLogger over any slog.Handler, adding the context enrichment.
func New(handler slog.Handler) *VanillaLogger {
	return &VanillaLogger{
		logger: slog.New(contextHandler{handler}),
		ctx:    context.Background(),
	}
}

// Default creates a VanillaLogger writing text to stdout with Info level.
func Default() *VanillaLogger {
	return NewText(os.Stdout, log.LevelInfo)
}

// ForService creates the logger of a deployed process: JSON on stdout (what a container runtime
// collects), Info level, with the fixed attributes "service", "version" and "env" on every line.
// Empty version or env are omitted.
func ForService(service, version, env string) log.Logger {
	args := []any{"service", service}
	if version != "" {
		args = append(args, "version", version)
	}
	if env != "" {
		args = append(args, "env", env)
	}
	return NewJSON(os.Stdout, log.LevelInfo).With(args...)
}

// Slog exposes the underlying slog.Logger (e.g. for slog.SetDefault, so that log.Default writes
// to the same destination).
func (l *VanillaLogger) Slog() *slog.Logger { return l.logger }

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
	if ctx == nil {
		ctx = context.Background()
	}
	return &VanillaLogger{
		logger: l.logger,
		ctx:    ctx,
	}
}

var _ log.Logger = (*VanillaLogger)(nil)

// contextHandler adds the context attributes to every record. Only the actor id is written:
// names are personal data.
type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	var add []slog.Attr
	if id := application.CorrelationID(ctx); id != "" {
		add = append(add, slog.String(CorrelationIDKey, id))
	}
	if id := application.CausationID(ctx); id != "" {
		add = append(add, slog.String(CausationIDKey, id))
	}
	if sc := trace.FromContext(ctx).Context(); sc.IsValid() {
		add = append(add, slog.String(trace.TraceIDKey, sc.TraceIDString()), slog.String(trace.SpanIDKey, sc.SpanIDString()))
	}
	if a := application.ActorFrom(ctx); !a.IsSystem() {
		add = append(add, slog.String(ActorKey, a.PartyID.String()))
	}
	if len(add) > 0 {
		r.Attrs(func(a slog.Attr) bool {
			for i := range add {
				if add[i].Key == a.Key {
					add = append(add[:i], add[i+1:]...)
					break
				}
			}
			return len(add) > 0
		})
		r = r.Clone()
		r.AddAttrs(add...)
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}

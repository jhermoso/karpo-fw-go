// Package vanilla ofrece una implementación de log.Logger con log/slog de la biblioteca estándar de Go.
//
// Cada línea se enriquece a partir del contexto dado a WithContext: los ids de correlación y de
// causalidad del flujo de negocio (application.WithCorrelationID / WithCausationID), el span en curso
// (trace.FromContext) y el id del actor (application.WithActor). Quien llama no los pasa a mano; un
// atributo pasado explícitamente con la misma clave gana.
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

// Claves de los atributos que se añaden a partir del contexto.
const (
	CorrelationIDKey = "correlation_id"
	CausationIDKey   = "causation_id"
	ActorKey         = "actor"
)

// VanillaLogger envuelve slog.Logger para cumplir el contrato log.Logger.
type VanillaLogger struct {
	logger *slog.Logger
	ctx    context.Context
}

// NewJSON crea un VanillaLogger que escribe en w logs estructurados en JSON.
func NewJSON(w io.Writer, minLevel log.Level) *VanillaLogger {
	opts := &slog.HandlerOptions{
		Level: slog.Level(minLevel),
	}
	return New(slog.NewJSONHandler(w, opts))
}

// NewText crea un VanillaLogger que escribe en w logs de texto legibles.
func NewText(w io.Writer, minLevel log.Level) *VanillaLogger {
	opts := &slog.HandlerOptions{
		Level: slog.Level(minLevel),
	}
	return New(slog.NewTextHandler(w, opts))
}

// New crea un VanillaLogger sobre cualquier slog.Handler y le añade el enriquecimiento desde el contexto.
func New(handler slog.Handler) *VanillaLogger {
	return &VanillaLogger{
		logger: slog.New(contextHandler{handler}),
		ctx:    context.Background(),
	}
}

// Default crea un VanillaLogger que escribe texto en stdout con nivel Info.
func Default() *VanillaLogger {
	return NewText(os.Stdout, log.LevelInfo)
}

// ForService crea el logger de un proceso desplegado: JSON en stdout (lo que recoge un entorno de
// contenedores), nivel Info, con los atributos fijos "service", "version" y "env" en cada línea.
// version o env vacíos se omiten.
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

// Slog expone el slog.Logger subyacente (p. ej. para slog.SetDefault, de modo que log.Default
// escriba en el mismo destino).
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

// contextHandler añade los atributos del contexto a cada registro. Sólo se escribe el id del actor:
// los nombres son datos personales.
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

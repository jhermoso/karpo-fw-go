package application

import (
	"context"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

type ctxKey int

const (
	correlationKey ctxKey = iota
	causationKey
	actorKey
	channelKey
	importKey
)

// Canales de operación (OperationChannel de C#).
const (
	ChannelWeb    = "web"
	ChannelMobile = "mobile"
	ChannelDevice = "device"
	ChannelAPI    = "api"
)

// WithChannel guarda el canal del que vino la operación; los valores desconocidos se ignoran.
func WithChannel(ctx context.Context, channel string) context.Context {
	switch c := strings.ToLower(strings.TrimSpace(channel)); c {
	case ChannelWeb, ChannelMobile, ChannelDevice, ChannelAPI:
		return context.WithValue(ctx, channelKey, c)
	}
	return ctx
}

// Channel devuelve el canal guardado en ctx, o "".
func Channel(ctx context.Context) string {
	c, _ := ctx.Value(channelKey).(string)
	return c
}

// ImportProvenance identifica una ejecución de importación de datos (ImportProvenance de C#): todo
// cambio hecho mientras está en el contexto se atribuye a esa ejecución en el log de auditoría.
type ImportProvenance struct {
	SourceKey  string      `json:"sourceKey"`
	RunID      domain.UUID `json:"runId"`
	SourceFile string      `json:"sourceFile,omitempty"`
}

// WithImportProvenance marca ctx como parte de una ejecución de importación (ImportContext.BeginRun de C#).
func WithImportProvenance(ctx context.Context, p ImportProvenance) context.Context {
	if p.RunID.IsZero() {
		p.RunID = domain.NewUUID()
	}
	return context.WithValue(ctx, importKey, p)
}

// ImportProvenanceFrom devuelve la ejecución de importación de ctx, si la hay.
func ImportProvenanceFrom(ctx context.Context) (ImportProvenance, bool) {
	p, ok := ctx.Value(importKey).(ImportProvenance)
	return p, ok
}

// WithActor guarda el actor que hace la petición en curso (la contrapartida en Go del
// ActorContext.Begin de C#, que usaba estado ambiente AsyncLocal).
func WithActor(ctx context.Context, a vocab.Actor) context.Context {
	return context.WithValue(ctx, actorKey, a)
}

// ActorFrom devuelve el actor guardado en ctx; si no hay ninguno, el del contexto de
// autorización (una llamada directa al caso de uso, sin HTTP, como un trabajo o la integración);
// y si tampoco, vocab.SystemActor (el modismo de C# "ActorContext.Current ?? Actor.System").
func ActorFrom(ctx context.Context) vocab.Actor {
	if a, ok := ctx.Value(actorKey).(vocab.Actor); ok && !a.IsZero() {
		return a
	}
	// Sólo un humano: los principales de servicio y el sistema siguen siendo System (no son una party).
	if c, ok := authz.FromContext(ctx); ok && c.Kind != authz.Service && c.Kind != authz.System {
		if a := c.Actor(); !a.IsZero() {
			return a
		}
	}
	return vocab.SystemActor
}

// WithCorrelationID guarda en ctx el id de correlación del flujo de negocio en curso.
// La capa de distribución lo fija a partir de la cabecera X-Correlation-ID; el outbox lo persiste.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey, id)
}

// CorrelationID devuelve el id de correlación guardado en ctx, o "".
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey).(string)
	return id
}

// WithCausationID guarda el id del mensaje que causó el procesamiento en curso
// (p. ej. el evento que está manejando un suscriptor).
func WithCausationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, causationKey, id)
}

// CausationID devuelve el id de causalidad guardado en ctx, o "".
func CausationID(ctx context.Context) string {
	id, _ := ctx.Value(causationKey).(string)
	return id
}

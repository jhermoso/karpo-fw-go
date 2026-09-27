package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

type ctxKey int

const (
	correlationKey ctxKey = iota
	causationKey
	actorKey
)

// WithActor stores the actor performing the current request (the Go counterpart of the C#
// ActorContext.Begin, which used ambient AsyncLocal state).
func WithActor(ctx context.Context, a vocab.Actor) context.Context {
	return context.WithValue(ctx, actorKey, a)
}

// ActorFrom returns the actor stored in ctx, or vocab.SystemActor when there is none
// (the C# idiom "ActorContext.Current ?? Actor.System").
func ActorFrom(ctx context.Context) vocab.Actor {
	if a, ok := ctx.Value(actorKey).(vocab.Actor); ok && !a.IsZero() {
		return a
	}
	return vocab.SystemActor
}

// WithCorrelationID stores the correlation id of the current business flow in ctx.
// The distribution layer sets it from the X-Correlation-ID header; the outbox persists it.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey, id)
}

// CorrelationID returns the correlation id stored in ctx, or "".
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey).(string)
	return id
}

// WithCausationID stores the id of the message that caused the current processing
// (e.g. the event being handled by a subscriber).
func WithCausationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, causationKey, id)
}

// CausationID returns the causation id stored in ctx, or "".
func CausationID(ctx context.Context) string {
	id, _ := ctx.Value(causationKey).(string)
	return id
}

package application

import (
	"context"
	"strings"

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

// Operation channels (C# OperationChannel).
const (
	ChannelWeb    = "web"
	ChannelMobile = "mobile"
	ChannelDevice = "device"
	ChannelAPI    = "api"
)

// WithChannel stores the channel the operation came from; unknown values are ignored.
func WithChannel(ctx context.Context, channel string) context.Context {
	switch c := strings.ToLower(strings.TrimSpace(channel)); c {
	case ChannelWeb, ChannelMobile, ChannelDevice, ChannelAPI:
		return context.WithValue(ctx, channelKey, c)
	}
	return ctx
}

// Channel returns the channel stored in ctx, or "".
func Channel(ctx context.Context) string {
	c, _ := ctx.Value(channelKey).(string)
	return c
}

// ImportProvenance identifies a data import run (C# ImportProvenance): every change made while
// it is in the context is attributed to that run in the audit log.
type ImportProvenance struct {
	SourceKey  string      `json:"sourceKey"`
	RunID      domain.UUID `json:"runId"`
	SourceFile string      `json:"sourceFile,omitempty"`
}

// WithImportProvenance marks ctx as part of an import run (C# ImportContext.BeginRun).
func WithImportProvenance(ctx context.Context, p ImportProvenance) context.Context {
	if p.RunID.IsZero() {
		p.RunID = domain.NewUUID()
	}
	return context.WithValue(ctx, importKey, p)
}

// ImportProvenanceFrom returns the import run in ctx, if any.
func ImportProvenanceFrom(ctx context.Context) (ImportProvenance, bool) {
	p, ok := ctx.Value(importKey).(ImportProvenance)
	return p, ok
}

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

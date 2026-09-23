// Package events provides domain event publishing and subscription primitives.
package events

import (
	"context"
	"time"
)

// Event is the contract for domain events in Karpo.
type Event interface {
	// ID returns a unique identifier for this event instance.
	ID() string

	// Type returns the event name/type identifier (e.g. "party.created").
	Type() string

	// OccurredAt returns the timestamp when the event was generated.
	OccurredAt() time.Time

	// Payload returns the data payload carried by this event.
	Payload() any
}

// Handler handles a domain event.
type Handler interface {
	Handle(ctx context.Context, evt Event) error
}

// HandlerFunc allows using a plain function as an event Handler.
type HandlerFunc func(ctx context.Context, evt Event) error

// Handle calls fn(ctx, evt).
func (fn HandlerFunc) Handle(ctx context.Context, evt Event) error {
	return fn(ctx, evt)
}

// Dispatcher coordinates event publishing and subscriptions.
type Dispatcher interface {
	// Publish dispatches an event to all subscribers registered for its type.
	Publish(ctx context.Context, evt Event) error

	// Subscribe registers a handler for a given event type. Returns an unsubscribe function.
	Subscribe(eventType string, handler Handler) func()
}

// BaseEvent is a convenience struct to embed in concrete domain events.
type BaseEvent struct {
	EventID        string
	EventType      string
	EventTimestamp time.Time
	EventPayload   any
}

func (e BaseEvent) ID() string             { return e.EventID }
func (e BaseEvent) Type() string           { return e.EventType }
func (e BaseEvent) OccurredAt() time.Time  { return e.EventTimestamp }
func (e BaseEvent) Payload() any           { return e.EventPayload }

package application

import (
	"context"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Validatable is implemented by commands/queries that can check their own input.
// Return a *domain.ValidationError (see domain.Validation) to get field-level details.
type Validatable interface {
	Validate() error
}

// ---------------------------------------------------------------------------------------------
// Domain events
// ---------------------------------------------------------------------------------------------

// WildcardEventType subscribes a handler to every event type.
const WildcardEventType = "*"

// Publisher delivers a domain event to its subscribers.
type Publisher interface {
	Publish(ctx context.Context, evt domain.Event) error
}

// EventHandler reacts to a domain event.
type EventHandler interface {
	Handle(ctx context.Context, evt domain.Event) error
}

// EventHandlerFunc adapts a function into an EventHandler.
type EventHandlerFunc func(ctx context.Context, evt domain.Event) error

// Handle calls fn.
func (fn EventHandlerFunc) Handle(ctx context.Context, evt domain.Event) error { return fn(ctx, evt) }

// Dispatcher publishes events and manages subscriptions (implementations: events/inprocess,
// message-broker adapters).
type Dispatcher interface {
	Publisher
	// Subscribe registers a handler for an event type (or WildcardEventType) and returns an
	// unsubscribe function.
	Subscribe(eventType string, h EventHandler) (unsubscribe func())
}

// EventRecorder durably records domain events inside the current unit of work
// (typically the transactional outbox).
type EventRecorder interface {
	Record(ctx context.Context, events []domain.Event) error
}

// EventDecoder rebuilds events from their serialized form (implementation: events.Registry).
type EventDecoder interface {
	Decode(eventType string, payload []byte) (domain.Event, error)
}

// ---------------------------------------------------------------------------------------------
// Transactional outbox
// ---------------------------------------------------------------------------------------------

// OutboxMessage is a serialized domain event waiting to be relayed.
type OutboxMessage struct {
	ID               string
	EventType        string
	AggregateType    string
	AggregateID      string
	AggregateVersion int64
	Payload          []byte
	OccurredAt       time.Time
	CorrelationID    string
	CausationID      string
	Attempts         int
	LastError        string
}

// OutboxStore persists outbox messages. Append must join the unit of work in ctx so messages
// commit atomically with the aggregate state (implementations: persistence/sqlrepo,
// persistence/memory, persistence/hotswap).
type OutboxStore interface {
	Append(ctx context.Context, msgs ...OutboxMessage) error
	// Pending returns unprocessed messages with fewer than maxAttempts attempts, oldest first.
	Pending(ctx context.Context, limit, maxAttempts int) ([]OutboxMessage, error)
	MarkProcessed(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, cause error) error
}

// ---------------------------------------------------------------------------------------------
// Idempotency
// ---------------------------------------------------------------------------------------------

// IdempotencyStore remembers the result of already-processed requests
// (equivalent to the C# IIdempotencyStore).
type IdempotencyStore interface {
	Get(ctx context.Context, key string) (value []byte, found bool, err error)
	Put(ctx context.Context, key string, value []byte, ttl time.Duration) error
}

// IdempotencyKeyed is implemented by commands carrying a client-supplied idempotency key
// (equivalent to the C# IIdempotentCommand).
type IdempotencyKeyed interface {
	IdempotencyKey() string
}

// ---------------------------------------------------------------------------------------------
// Audit log
// ---------------------------------------------------------------------------------------------

// Audit operations.
const (
	AuditCreated = "created"
	AuditUpdated = "updated"
	AuditDeleted = "deleted"
)

// AuditRecord is one entry of the audit log: who changed which aggregate, how, when, through
// which channel or import run, which fields changed and which events it raised.
type AuditRecord struct {
	ID               string               `json:"id"`
	AggregateType    string               `json:"aggregateType"`
	AggregateID      string               `json:"aggregateId"`
	AggregateVersion int64                `json:"aggregateVersion"`
	Operation        string               `json:"operation"`
	Actor            vocab.Actor          `json:"actor"`
	Channel          string               `json:"channel,omitempty"`
	Import           *ImportProvenance    `json:"import,omitempty"`
	CorrelationID    string               `json:"correlationId,omitempty"`
	At               time.Time            `json:"at"`
	Changes          []traits.FieldChange `json:"changes,omitempty"`
	Events           []string             `json:"events,omitempty"`
}

// AuditLog stores audit records. Append must join the unit of work in ctx, so the record
// commits atomically with the state change (implementations: persistence/sqlrepo,
// persistence/memory).
type AuditLog interface {
	Append(ctx context.Context, records ...AuditRecord) error
	// Trail returns the records of one aggregate, oldest first.
	Trail(ctx context.Context, aggregateType, aggregateID string) ([]AuditRecord, error)
}

package application

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// ---------------------------------------------------------------------------------------------
// Integration events (strategic: communication between bounded contexts)
// ---------------------------------------------------------------------------------------------
//
// Domain events stay inside their bounded context. What crosses the boundary is an integration
// event: an explicit, versioned contract of the upstream context (its Published Language),
// produced by translating domain events, recorded in an integration outbox in the same unit of
// work, relayed to a transport and consumed downstream through an inbox that discards
// duplicates. Renaming a field of a domain event never breaks another context.

// IntegrationEvent is a fact a bounded context publishes for others. The type name is part of
// the contract and carries its version, "{context}.{fact}.v{n}" (e.g. "parties.party-registered.v1").
// Implementations are plain structs with JSON tags, owned by the publishing context.
type IntegrationEvent interface {
	IntegrationEventType() string
}

// Envelope is an integration event on the wire (CloudEvents-like attributes plus the JSON data).
type Envelope struct {
	ID            string          `json:"id"`      // unique message id: the inbox key
	Type          string          `json:"type"`    // IntegrationEventType
	Source        string          `json:"source"`  // publishing bounded context
	Subject       string          `json:"subject"` // aggregate id the fact is about (ordering key)
	OccurredAt    time.Time       `json:"time"`
	CorrelationID string          `json:"correlationId,omitempty"`
	CausationID   string          `json:"causationId,omitempty"` // id of the domain event that caused it
	Data          json.RawMessage `json:"data"`
}

// Translator maps a domain event of its bounded context to the integration events it publishes
// (none when the fact is internal). It is the outbound side of the anti-corruption layer.
type Translator interface {
	Translate(ctx context.Context, evt domain.Event) ([]IntegrationEvent, error)
}

// TranslatorFunc adapts a function into a Translator.
type TranslatorFunc func(ctx context.Context, evt domain.Event) ([]IntegrationEvent, error)

// Translate calls fn.
func (fn TranslatorFunc) Translate(ctx context.Context, evt domain.Event) ([]IntegrationEvent, error) {
	return fn(ctx, evt)
}

// MessageSender hands envelopes to a transport (message broker adapter, in-process broker,
// HTTP webhook...). It returns only when the transport accepted them.
type MessageSender interface {
	Send(ctx context.Context, envelopes ...Envelope) error
}

// MessageHandler consumes an envelope in a downstream bounded context.
type MessageHandler interface {
	HandleMessage(ctx context.Context, env Envelope) error
}

// MessageHandlerFunc adapts a function into a MessageHandler.
type MessageHandlerFunc func(ctx context.Context, env Envelope) error

// HandleMessage calls fn.
func (fn MessageHandlerFunc) HandleMessage(ctx context.Context, env Envelope) error {
	return fn(ctx, env)
}

// InboxStore remembers the messages a consumer already processed. Claim must join the unit of
// work in ctx: the claim commits with the consumer's changes or rolls back with them, which
// turns at-least-once delivery into an exactly-once effect (implementations:
// persistence/memory, persistence/sqlrepo, persistence/hotswap).
type InboxStore interface {
	// Claim records (consumer, messageID) and reports whether it is the first time.
	Claim(ctx context.Context, consumer, messageID string) (first bool, err error)
}

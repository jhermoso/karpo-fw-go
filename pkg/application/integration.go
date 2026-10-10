package application

import (
	"context"
	"encoding/json"
	"errors"
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
// HTTP webhook...). It returns only when the transport accepted them, in the order given.
type MessageSender interface {
	Send(ctx context.Context, envelopes ...Envelope) error
}

// ErrTransportUnavailable is returned (wrapped) by a MessageSender when the transport cannot be
// reached. It says nothing about the message, so a relay stops its pass and tries again later
// without counting an attempt against it.
var ErrTransportUnavailable = errors.New("message transport unavailable")

// MessageHandler consumes an envelope in a downstream bounded context.
type MessageHandler interface {
	HandleMessage(ctx context.Context, env Envelope) error
}

// MessageSubscriber attaches consumers to a transport (in-process broker, message broker
// adapter). The composition of a bounded context depends on this port, never on a transport.
type MessageSubscriber interface {
	// Attach delivers to h the envelopes of the given types (every type when none) until detach
	// is called. consumer names a logical consumer, usually a bounded context: attachments that
	// share the name are replicas of it, and each message takes effect once among them. A
	// message is settled only when h returns nil; otherwise it is delivered again.
	Attach(ctx context.Context, consumer string, h MessageHandler, types ...string) (detach func() error, err error)
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

// InboxPurger is implemented by the inbox stores that can forget old messages, so that the
// inbox does not grow for ever.
type InboxPurger interface {
	// Purge forgets the messages claimed before olderThan and returns how many. A forgotten
	// message that is delivered again takes effect again: only purge what the transport can no
	// longer redeliver (older than what it retains).
	Purge(ctx context.Context, olderThan time.Time) (int64, error)
}

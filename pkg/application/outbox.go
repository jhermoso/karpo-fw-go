package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

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
// commit atomically with the aggregate state (implementations: sqlrepo, memory).
type OutboxStore interface {
	Append(ctx context.Context, msgs ...OutboxMessage) error
	// Pending returns unprocessed messages with fewer than maxAttempts attempts, oldest first.
	Pending(ctx context.Context, limit, maxAttempts int) ([]OutboxMessage, error)
	MarkProcessed(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, cause error) error
}

// Outbox is the EventRecorder that serializes events into an OutboxStore.
type Outbox struct {
	store OutboxStore
}

// NewOutbox creates an Outbox over store.
func NewOutbox(store OutboxStore) *Outbox { return &Outbox{store: store} }

// Record serializes evts (JSON) and appends them to the store in the current unit of work.
func (o *Outbox) Record(ctx context.Context, evts []domain.Event) error {
	msgs := make([]OutboxMessage, 0, len(evts))
	for _, evt := range evts {
		payload, err := json.Marshal(evt)
		if err != nil {
			return fmt.Errorf("outbox: encoding %s: %w", evt.EventType(), err)
		}
		m := evt.Meta()
		id := m.EventID
		if id == "" {
			id = domain.NewUUID().String()
		}
		occurred := m.OccurredAt
		if occurred.IsZero() {
			occurred = domain.Now()
		}
		msgs = append(msgs, OutboxMessage{
			ID:               id,
			EventType:        evt.EventType(),
			AggregateType:    m.AggregateType,
			AggregateID:      m.AggregateID,
			AggregateVersion: m.AggregateVersion,
			Payload:          payload,
			OccurredAt:       occurred.UTC(),
			CorrelationID:    CorrelationID(ctx),
			CausationID:      CausationID(ctx),
		})
	}
	return o.store.Append(ctx, msgs...)
}

// EventDecoder rebuilds events from their serialized form. events.Registry satisfies it.
type EventDecoder interface {
	Decode(eventType string, payload []byte) (domain.Event, error)
}

// OutboxRelay moves outbox messages to a Publisher (in-process bus, message broker adapter...).
// Delivery is at-least-once: subscribers must be idempotent (use the event id).
type OutboxRelay struct {
	store       OutboxStore
	decoder     EventDecoder
	publisher   Publisher
	batchSize   int
	maxAttempts int
	logger      log.Logger
}

// RelayOption configures an OutboxRelay.
type RelayOption func(*OutboxRelay)

// WithBatchSize sets how many messages are fetched per iteration (default 100).
func WithBatchSize(n int) RelayOption { return func(r *OutboxRelay) { r.batchSize = n } }

// WithMaxAttempts sets after how many failures a message is parked (default 10).
func WithMaxAttempts(n int) RelayOption { return func(r *OutboxRelay) { r.maxAttempts = n } }

// WithRelayLogger sets a logger for delivery failures.
func WithRelayLogger(l log.Logger) RelayOption { return func(r *OutboxRelay) { r.logger = l } }

// NewOutboxRelay creates a relay.
func NewOutboxRelay(store OutboxStore, decoder EventDecoder, publisher Publisher, opts ...RelayOption) *OutboxRelay {
	r := &OutboxRelay{store: store, decoder: decoder, publisher: publisher, batchSize: 100, maxAttempts: 10}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RelayOnce delivers one batch of pending messages and returns how many were delivered.
func (r *OutboxRelay) RelayOnce(ctx context.Context) (int, error) {
	msgs, err := r.store.Pending(ctx, r.batchSize, r.maxAttempts)
	if err != nil {
		return 0, err
	}
	delivered := 0
	var errs []error
	for _, m := range msgs {
		if err := r.deliver(ctx, m); err != nil {
			if r.logger != nil {
				r.logger.Warn("outbox delivery failed", "message_id", m.ID, "event_type", m.EventType, "error", err)
			}
			if markErr := r.store.MarkFailed(ctx, m.ID, err); markErr != nil {
				errs = append(errs, markErr)
			}
			continue
		}
		if err := r.store.MarkProcessed(ctx, m.ID); err != nil {
			errs = append(errs, err)
			continue
		}
		delivered++
	}
	return delivered, errors.Join(errs...)
}

func (r *OutboxRelay) deliver(ctx context.Context, m OutboxMessage) error {
	evt, err := r.decoder.Decode(m.EventType, m.Payload)
	if err != nil {
		return err
	}
	ctx = WithCausationID(WithCorrelationID(ctx, m.CorrelationID), m.ID)
	return r.publisher.Publish(ctx, evt)
}

// Run relays continuously every interval until ctx is cancelled.
func (r *OutboxRelay) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for {
			n, err := r.RelayOnce(ctx)
			if err != nil && r.logger != nil {
				r.logger.Error("outbox relay iteration failed", "error", err)
			}
			if n < r.batchSize || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

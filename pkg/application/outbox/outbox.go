// Package outbox implements the transactional outbox on top of the application.OutboxStore port:
// Recorder serializes domain events in the aggregate's unit of work, and Relay delivers them
// to a Publisher (at-least-once).
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

// Recorder is the application.EventRecorder that serializes events into an OutboxStore.
type Recorder struct {
	store application.OutboxStore
}

// NewRecorder creates a Recorder over store.
func NewRecorder(store application.OutboxStore) *Recorder { return &Recorder{store: store} }

// Record serializes evts (JSON) and appends them to the store in the current unit of work.
func (o *Recorder) Record(ctx context.Context, evts []domain.Event) error {
	msgs := make([]application.OutboxMessage, 0, len(evts))
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
		msgs = append(msgs, application.OutboxMessage{
			ID:               id,
			EventType:        evt.EventType(),
			AggregateType:    m.AggregateType,
			AggregateID:      m.AggregateID,
			AggregateVersion: m.AggregateVersion,
			Payload:          payload,
			OccurredAt:       occurred.UTC(),
			CorrelationID:    application.CorrelationID(ctx),
			CausationID:      application.CausationID(ctx),
		})
	}
	return o.store.Append(ctx, msgs...)
}

// Relay moves outbox messages to a Publisher (in-process bus, message broker adapter...).
// Delivery is at-least-once: subscribers must be idempotent (use the event id).
type Relay struct {
	store       application.OutboxStore
	decoder     application.EventDecoder
	publisher   application.Publisher
	batchSize   int
	maxAttempts int
	logger      log.Logger
}

// RelayOption configures a Relay.
type RelayOption func(*Relay)

// WithBatchSize sets how many messages are fetched per iteration (default 100).
func WithBatchSize(n int) RelayOption { return func(r *Relay) { r.batchSize = n } }

// WithMaxAttempts sets after how many failures a message is parked (default 10).
func WithMaxAttempts(n int) RelayOption { return func(r *Relay) { r.maxAttempts = n } }

// WithRelayLogger sets a logger for delivery failures.
func WithRelayLogger(l log.Logger) RelayOption { return func(r *Relay) { r.logger = l } }

// NewRelay creates a relay.
func NewRelay(store application.OutboxStore, decoder application.EventDecoder, publisher application.Publisher, opts ...RelayOption) *Relay {
	r := &Relay{store: store, decoder: decoder, publisher: publisher, batchSize: 100, maxAttempts: 10}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RelayOnce delivers one batch of pending messages and returns how many were delivered.
func (r *Relay) RelayOnce(ctx context.Context) (int, error) {
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

func (r *Relay) deliver(ctx context.Context, m application.OutboxMessage) error {
	evt, err := r.decoder.Decode(m.EventType, m.Payload)
	if err != nil {
		return err
	}
	ctx = application.WithCausationID(application.WithCorrelationID(ctx, m.CorrelationID), m.ID)
	return r.publisher.Publish(ctx, evt)
}

// Run relays continuously every interval until ctx is cancelled.
func (r *Relay) Run(ctx context.Context, interval time.Duration) error {
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

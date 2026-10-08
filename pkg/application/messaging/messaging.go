// Package messaging implements the integration event contracts of the application layer:
//
//   - Recorder translates domain events into integration events (Published Language) and
//     records them in the integration outbox, in the same unit of work as the aggregate;
//   - NewRelay forwards the integration outbox to a transport (application.MessageSender),
//     at-least-once;
//   - Consumer decodes envelopes for a downstream bounded context and runs each handler inside
//     a unit of work together with the inbox claim, so a redelivered message has no effect.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

var typePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+\.v[1-9][0-9]*$`)

// ValidType reports whether an integration event type follows "{context}.{fact}.v{n}".
func ValidType(t string) bool { return typePattern.MatchString(t) }

// TypeOf returns the integration event type declared by the value type E.
func TypeOf[E application.IntegrationEvent]() string {
	var zero E
	if reflect.TypeFor[E]().Kind() == reflect.Pointer {
		panic(fmt.Sprintf("messaging: %s must be a value type, not a pointer", reflect.TypeFor[E]()))
	}
	return zero.IntegrationEventType()
}

// ---------------------------------------------------------------------------------------------
// Publishing side
// ---------------------------------------------------------------------------------------------

// Recorder is the application.EventRecorder of the integration outbox: it runs the translators
// of a bounded context and appends the resulting integration events to store. Combine it with
// the domain outbox recorder through outbox.Recorders.
type Recorder struct {
	source      string
	store       application.OutboxStore
	mu          sync.RWMutex
	byType      map[string][]application.Translator
	translators []application.Translator
}

var _ application.EventRecorder = (*Recorder)(nil)

// NewRecorder creates a recorder for the bounded context source over the integration outbox.
func NewRecorder(source string, store application.OutboxStore) *Recorder {
	return &Recorder{source: source, store: store, byType: map[string][]application.Translator{}}
}

// Use registers a translator that sees every domain event.
func (r *Recorder) Use(t application.Translator) *Recorder {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.translators = append(r.translators, t)
	return r
}

// On registers a typed translation for the domain event E.
//
//	messaging.On(rec, func(ctx context.Context, e domain.PartyRegistered) ([]application.IntegrationEvent, error) {
//		return []application.IntegrationEvent{contracts.PartyRegistered{PartyID: e.PartyID.String(), ...}}, nil
//	})
func On[E domain.Event](r *Recorder, fn func(ctx context.Context, evt E) ([]application.IntegrationEvent, error)) {
	var zero E
	name := zero.EventType()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byType[name] = append(r.byType[name], application.TranslatorFunc(func(ctx context.Context, evt domain.Event) ([]application.IntegrationEvent, error) {
		typed, ok := evt.(E)
		if !ok {
			return nil, fmt.Errorf("messaging: translator for %s received %T", name, evt)
		}
		return fn(ctx, typed)
	}))
}

// Record implements application.EventRecorder.
func (r *Recorder) Record(ctx context.Context, evts []domain.Event) error {
	var msgs []application.OutboxMessage
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, evt := range evts {
		ts := append(append([]application.Translator(nil), r.byType[evt.EventType()]...), r.translators...)
		for _, t := range ts {
			out, err := t.Translate(ctx, evt)
			if err != nil {
				return fmt.Errorf("messaging: translating %s: %w", evt.EventType(), err)
			}
			for _, ie := range out {
				m, err := r.message(ctx, evt, ie)
				if err != nil {
					return err
				}
				msgs = append(msgs, m)
			}
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return r.store.Append(ctx, msgs...)
}

func (r *Recorder) message(ctx context.Context, evt domain.Event, ie application.IntegrationEvent) (application.OutboxMessage, error) {
	t := ie.IntegrationEventType()
	if !ValidType(t) {
		return application.OutboxMessage{}, fmt.Errorf("%w: integration event type %q must be {context}.{fact}.v{n}", domain.ErrValidation, t)
	}
	data, err := json.Marshal(ie)
	if err != nil {
		return application.OutboxMessage{}, fmt.Errorf("messaging: encoding %s: %w", t, err)
	}
	meta := evt.Meta()
	occurred := meta.OccurredAt
	if occurred.IsZero() {
		occurred = domain.Now()
	}
	return application.OutboxMessage{
		ID: domain.NewUUID().String(), EventType: t,
		AggregateType: meta.AggregateType, AggregateID: meta.AggregateID, AggregateVersion: meta.AggregateVersion,
		Payload: data, OccurredAt: occurred.UTC(),
		CorrelationID: application.CorrelationID(ctx), CausationID: meta.EventID,
	}, nil
}

// Envelope builds the wire envelope of an integration outbox message.
func Envelope(source string, m application.OutboxMessage) application.Envelope {
	return application.Envelope{
		ID: m.ID, Type: m.EventType, Source: source, Subject: m.AggregateID, OccurredAt: m.OccurredAt,
		CorrelationID: m.CorrelationID, CausationID: m.CausationID, Data: json.RawMessage(m.Payload),
	}
}

// NewRelay forwards the integration outbox of source to sender (at-least-once: consumers
// deduplicate with their inbox). Run it with Relay.Run or Relay.RelayOnce.
func NewRelay(source string, store application.OutboxStore, sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	opts = append([]outbox.RelayOption{outbox.WithRelayName(source)}, opts...)
	return outbox.NewForwarder(store, func(ctx context.Context, m application.OutboxMessage) error {
		return sender.Send(ctx, Envelope(source, m))
	}, opts...)
}

// ---------------------------------------------------------------------------------------------
// Consuming side
// ---------------------------------------------------------------------------------------------

// Consumer is the application.MessageHandler of a downstream bounded context. Each handled
// message runs in one unit of work with its inbox claim: duplicates are skipped and a failing
// handler rolls the claim back, so the redelivery is processed again.
type Consumer struct {
	name     string
	inbox    application.InboxStore
	uow      domain.UnitOfWork
	mu       sync.RWMutex
	handlers map[string]func(ctx context.Context, env application.Envelope) error
	tel      *consumerTelemetry
}

var _ application.MessageHandler = (*Consumer)(nil)

// NewConsumer creates the consumer name (the inbox partition, usually the bounded context).
func NewConsumer(name string, inbox application.InboxStore, uow domain.UnitOfWork) *Consumer {
	return &Consumer{name: name, inbox: inbox, uow: uow, handlers: map[string]func(context.Context, application.Envelope) error{}}
}

// Name returns the consumer name.
func (c *Consumer) Name() string { return c.name }

// Handle registers a typed handler for the integration event E. The consumer owns its copy of
// the upstream contract (a struct with the fields it needs): unknown fields are ignored, which
// lets the upstream add fields without breaking it.
func Handle[E application.IntegrationEvent](c *Consumer, fn func(ctx context.Context, evt E, env application.Envelope) error) {
	name := TypeOf[E]()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers[name] = func(ctx context.Context, env application.Envelope) error {
		var evt E
		if err := json.Unmarshal(env.Data, &evt); err != nil {
			return fmt.Errorf("messaging: decoding %s: %w", name, err)
		}
		return fn(ctx, evt, env)
	}
}

// Types returns the integration event types the consumer handles (to subscribe a transport).
func (c *Consumer) Types() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.handlers))
	for t := range c.handlers {
		out = append(out, t)
	}
	return out
}

// HandleMessage implements application.MessageHandler. Messages of types without a handler are
// acknowledged without effect.
func (c *Consumer) HandleMessage(ctx context.Context, env application.Envelope) error {
	c.mu.RLock()
	h, ok := c.handlers[env.Type]
	tel := c.tel
	c.mu.RUnlock()
	if !ok {
		tel.ignored(ctx, c.name)
		return nil
	}
	ctx = application.WithCorrelationID(ctx, env.CorrelationID)
	if env.ID != "" {
		ctx = application.WithCausationID(ctx, env.ID)
	}
	ctx, done := tel.start(ctx, c.name, env)
	if env.ID == "" {
		// Rejected, but still counted and logged: a producer sending envelopes without id is a
		// fault to see in the metrics, not a silent drop.
		err := fmt.Errorf("%w: envelope without id", domain.ErrValidation)
		done(false, err)
		return err
	}
	// A panicking handler rolls its unit of work back (uow.Do re-raises): the span is closed and
	// the failure counted before the panic goes on.
	defer func() {
		if p := recover(); p != nil {
			done(false, panicError(p))
			panic(p)
		}
	}()
	duplicate := false
	err := c.uow.Do(ctx, func(ctx context.Context) error {
		first, err := c.inbox.Claim(ctx, c.name, env.ID)
		if err != nil || !first {
			duplicate = err == nil
			return err
		}
		return h(ctx, env)
	})
	done(duplicate, err)
	return err
}

// panicError turns a recovered value into the error a panic left.
func panicError(r any) error {
	if err, ok := r.(error); ok {
		return fmt.Errorf("panic: %w", err)
	}
	return fmt.Errorf("panic: %v", r)
}

// Lag returns how long ago the message happened (for metrics and logs).
func Lag(env application.Envelope) time.Duration { return domain.Now().Sub(env.OccurredAt) }

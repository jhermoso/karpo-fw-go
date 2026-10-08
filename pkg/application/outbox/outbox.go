// Package outbox implements the transactional outbox on top of the application.OutboxStore port:
// Recorder serializes domain events in the aggregate's unit of work, and Relay delivers them
// to a Publisher (at-least-once).
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
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
	deliverFn   DeliverFunc
	batchSize   int
	maxAttempts int
	logger      log.Logger
	name        string
	tracer      trace.Tracer
	meter       metrics.Meter
	relayed     metrics.Counter
}

// RelayOption configures a Relay.
type RelayOption func(*Relay)

// WithBatchSize sets how many messages are fetched per iteration (default 100).
func WithBatchSize(n int) RelayOption { return func(r *Relay) { r.batchSize = n } }

// WithMaxAttempts sets after how many failures a message is parked (default 10).
func WithMaxAttempts(n int) RelayOption { return func(r *Relay) { r.maxAttempts = n } }

// WithRelayLogger sets the logger of delivery failures. Without it the relay writes to
// log.Default (the logger of the process): a failed delivery is never silent by omission. Pass
// log.Discard to silence it on purpose.
func WithRelayLogger(l log.Logger) RelayOption { return func(r *Relay) { r.logger = l } }

// WithRelayName names the outbox in metrics and logs (default "outbox"; the integration relay
// uses the name of its bounded context).
func WithRelayName(name string) RelayOption { return func(r *Relay) { r.name = name } }

// WithRelayTelemetry instruments the relay: a span per delivered message (a new trace that
// carries the correlation and causation ids of the message), the counter karpo.outbox.relayed
// by event type and outcome, and the gauge karpo.outbox.oldest_pending_age, which asks the
// store for the oldest pending message every time the metrics are read: it grows while the
// relay is stopped or stuck and returns to zero when the outbox is empty. Either may be nil.
func WithRelayTelemetry(tracer trace.Tracer, meter metrics.Meter) RelayOption {
	return func(r *Relay) { r.tracer, r.meter = tracer, meter }
}

// DeliverFunc delivers one outbox message; an error leaves it pending for a retry.
type DeliverFunc func(ctx context.Context, m application.OutboxMessage) error

// NewRelay creates a relay that decodes domain events and hands them to publisher.
func NewRelay(store application.OutboxStore, decoder application.EventDecoder, publisher application.Publisher, opts ...RelayOption) *Relay {
	return NewForwarder(store, func(ctx context.Context, m application.OutboxMessage) error {
		evt, err := decoder.Decode(m.EventType, m.Payload)
		if err != nil {
			return err
		}
		ctx = application.WithCausationID(application.WithCorrelationID(ctx, m.CorrelationID), m.ID)
		return publisher.Publish(ctx, evt)
	}, opts...)
}

// NewForwarder creates a relay with a custom delivery (e.g. the integration relay, which
// forwards the serialized message to a transport without decoding it).
func NewForwarder(store application.OutboxStore, deliver DeliverFunc, opts ...RelayOption) *Relay {
	r := &Relay{store: store, deliverFn: deliver, batchSize: 100, maxAttempts: 10, name: "outbox"}
	for _, opt := range opts {
		opt(r)
	}
	if r.logger == nil {
		r.logger = log.Default()
	}
	r.tracer = trace.OrNoop(r.tracer)
	meter := metrics.OrNoop(r.meter)
	r.relayed = meter.Counter(metrics.OutboxRelayed, metrics.UnitNone, "Outbox messages handed to the publisher, by outcome.")
	if r.meter != nil {
		registerPendingAge(r.meter, r)
	}
	return r
}

// The gauge karpo.outbox.oldest_pending_age is one series per outbox name. Relays that share a
// name in the same meter (several bounded contexts, each with its domain relay and the default
// name) share that series: it reports the oldest pending message among them, so any of them
// getting stuck shows. Registering one gauge per relay would let the last one replace the others
// in silence.
var pendingAges = struct {
	sync.Mutex
	groups map[pendingAgeKey]*pendingAgeGroup
}{groups: map[pendingAgeKey]*pendingAgeGroup{}}

type pendingAgeKey struct {
	meter metrics.Meter
	name  string
}

type pendingAgeGroup struct {
	mu     sync.Mutex
	relays []*Relay
}

func registerPendingAge(meter metrics.Meter, r *Relay) {
	const help = "Age of the oldest outbox message still pending delivery."
	if !reflect.TypeOf(meter).Comparable() {
		// Cannot be a map key: one gauge per relay, as best effort.
		meter.Gauge(metrics.OutboxOldestPendingAge, metrics.UnitSeconds, help, r.oldestPendingAge, metrics.LabelOutbox, r.name)
		return
	}
	key := pendingAgeKey{meter, r.name}
	pendingAges.Lock()
	g, ok := pendingAges.groups[key]
	if !ok {
		g = &pendingAgeGroup{}
		pendingAges.groups[key] = g
	}
	pendingAges.Unlock()
	g.mu.Lock()
	g.relays = append(g.relays, r)
	g.mu.Unlock()
	if !ok {
		meter.Gauge(metrics.OutboxOldestPendingAge, metrics.UnitSeconds, help, g.oldest, metrics.LabelOutbox, r.name)
	}
}

// oldest is the largest age among the relays of the group; -1 only when none could be read.
func (g *pendingAgeGroup) oldest() float64 {
	g.mu.Lock()
	relays := append([]*Relay(nil), g.relays...)
	g.mu.Unlock()
	age := -1.0
	for _, r := range relays {
		age = max(age, r.oldestPendingAge())
	}
	return age
}

// oldestPendingAge asks the store, so it also tells when the relay is not running at all.
// It returns -1 when the store cannot be read.
func (r *Relay) oldestPendingAge() float64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	msgs, err := r.store.Pending(ctx, 1, r.maxAttempts)
	if err != nil {
		return -1
	}
	if len(msgs) == 0 {
		return 0
	}
	return max(domain.Now().Sub(msgs[0].OccurredAt).Seconds(), 0)
}

// deliver hands one message to the publisher inside its own span.
func (r *Relay) deliver(ctx context.Context, m application.OutboxMessage) error {
	ctx, span := r.tracer.Start(ctx, "outbox relay "+m.EventType, trace.WithKind(trace.KindProducer), trace.WithNewRoot())
	span.SetAttributes("outbox", r.name, "event_type", m.EventType, "message_id", m.ID)
	if m.CorrelationID != "" {
		span.SetAttributes("correlation_id", m.CorrelationID)
	}
	if m.CausationID != "" {
		span.SetAttributes("causation_id", m.CausationID)
	}
	err := r.deliverFn(ctx, m)
	outcome := application.OutcomeOK
	if err != nil {
		outcome = application.OutcomeError
		span.RecordError(err)
	}
	span.End()
	r.relayed.Add(ctx, 1, metrics.LabelEventType, m.EventType, metrics.LabelOutcome, outcome)
	return err
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
			r.logger.Warn("outbox delivery failed", "outbox", r.name, "message_id", m.ID, "event_type", m.EventType,
				"attempt", m.Attempts+1, "correlation_id", m.CorrelationID, "causation_id", m.CausationID, "error", err.Error())
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

// Recorders combines several recorders (e.g. the domain outbox and the integration outbox)
// into one application.EventRecorder; all of them record in the same unit of work.
func Recorders(recorders ...application.EventRecorder) application.EventRecorder {
	return multi(recorders)
}

type multi []application.EventRecorder

func (m multi) Record(ctx context.Context, evts []domain.Event) error {
	for _, r := range m {
		if err := r.Record(ctx, evts); err != nil {
			return err
		}
	}
	return nil
}

// Run relays continuously every interval until ctx is cancelled.
func (r *Relay) Run(ctx context.Context, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for {
			n, err := r.RelayOnce(ctx)
			if err != nil && ctx.Err() == nil {
				r.logger.Error("outbox relay iteration failed", "outbox", r.name, "error", err.Error())
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

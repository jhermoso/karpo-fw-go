// Package outbox implementa el outbox transaccional sobre el puerto application.OutboxStore:
// Recorder serializa los eventos de dominio en la unidad de trabajo del agregado, y Relay los
// entrega a un Publisher (al menos una vez).
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

// Recorder es el application.EventRecorder que serializa los eventos en un OutboxStore.
type Recorder struct {
	store application.OutboxStore
}

// NewRecorder crea un Recorder sobre store.
func NewRecorder(store application.OutboxStore) *Recorder { return &Recorder{store: store} }

// Record serializa evts (JSON) y los añade al almacén en la unidad de trabajo en curso.
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

// Relay lleva los mensajes del outbox a un Publisher (bus en proceso, adaptador de un broker de mensajería...).
// La entrega es al menos una vez: los suscriptores deben ser idempotentes (con el id del evento).
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

// RelayOption configura un Relay.
type RelayOption func(*Relay)

// WithBatchSize fija cuántos mensajes se leen por iteración (por defecto 100).
func WithBatchSize(n int) RelayOption { return func(r *Relay) { r.batchSize = n } }

// WithMaxAttempts fija tras cuántos fallos se aparca un mensaje (por defecto 10).
func WithMaxAttempts(n int) RelayOption { return func(r *Relay) { r.maxAttempts = n } }

// WithRelayLogger fija el logger de los fallos de entrega. Sin él, el relay escribe en
// log.Default (el logger del proceso): una entrega fallida nunca queda en silencio por omisión.
// Se pasa log.Discard para silenciarlo a propósito.
func WithRelayLogger(l log.Logger) RelayOption { return func(r *Relay) { r.logger = l } }

// WithRelayName da nombre al outbox en métricas y logs (por defecto "outbox"; el relay de integración
// usa el nombre de su contexto delimitado).
func WithRelayName(name string) RelayOption { return func(r *Relay) { r.name = name } }

// WithRelayTelemetry instrumenta el relay: un span por mensaje entregado (una traza nueva que lleva
// los ids de correlación y de causalidad del mensaje), el contador karpo.outbox.relayed por tipo de
// evento y desenlace, y el indicador karpo.outbox.oldest_pending_age, que pregunta al almacén por el
// mensaje pendiente más antiguo cada vez que se leen las métricas: crece mientras el relay está parado
// o atascado y vuelve a cero cuando el outbox está vacío. Cualquiera de los dos puede ser nil.
func WithRelayTelemetry(tracer trace.Tracer, meter metrics.Meter) RelayOption {
	return func(r *Relay) { r.tracer, r.meter = tracer, meter }
}

// DeliverFunc entrega un mensaje del outbox; un error lo deja pendiente para un reintento.
type DeliverFunc func(ctx context.Context, m application.OutboxMessage) error

// NewRelay crea un relay que decodifica eventos de dominio y se los pasa a publisher.
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

// NewForwarder crea un relay con una entrega a medida (p. ej. el relay de integración, que reenvía
// el mensaje serializado a un transporte sin decodificarlo).
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

// El indicador karpo.outbox.oldest_pending_age es una serie por nombre de outbox. Los relays que
// comparten nombre en el mismo meter (varios contextos delimitados, cada uno con su relay de dominio y
// el nombre por defecto) comparten esa serie: informa del mensaje pendiente más antiguo entre ellos,
// así que se ve cuando cualquiera se atasca. Registrar un indicador por relay haría que el último
// sustituyera a los demás en silencio.
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
		// No puede ser clave de mapa: un indicador por relay, sin garantías.
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

// oldest es la mayor antigüedad entre los relays del grupo; -1 sólo si no se ha podido leer ninguno.
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

// oldestPendingAge pregunta al almacén, así que también avisa cuando el relay no se está ejecutando.
// Devuelve -1 cuando no se puede leer el almacén.
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

// deliver pasa un mensaje al publicador dentro de su propio span.
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

// RelayOnce entrega un lote de mensajes pendientes y devuelve cuántos se entregaron.
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

// Recorders combina varios registradores (p. ej. el outbox de dominio y el de integración)
// en un solo application.EventRecorder; todos registran en la misma unidad de trabajo.
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

// Run retransmite continuamente cada interval hasta que se cancela ctx.
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

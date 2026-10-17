// Package messaging implementa los contratos de eventos de integración de la capa de aplicación:
//
//   - Recorder traduce eventos de dominio a eventos de integración (Published Language) y los
//     registra en el outbox de integración, en la misma unidad de trabajo que el agregado;
//   - NewRelay reenvía el outbox de integración a un transporte (application.MessageSender),
//     al menos una vez;
//   - Consumer decodifica sobres para un contexto delimitado aguas abajo y ejecuta cada manejador
//     dentro de una unidad de trabajo junto con la reclamación del inbox, de modo que un mensaje
//     reentregado no tiene efecto.
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

// ValidType dice si un tipo de evento de integración sigue "{context}.{fact}.v{n}".
func ValidType(t string) bool { return typePattern.MatchString(t) }

// TypeOf devuelve el tipo de evento de integración que declara el tipo valor E.
func TypeOf[E application.IntegrationEvent]() string {
	var zero E
	if reflect.TypeFor[E]().Kind() == reflect.Pointer {
		panic(fmt.Sprintf("messaging: %s must be a value type, not a pointer", reflect.TypeFor[E]()))
	}
	return zero.IntegrationEventType()
}

// ---------------------------------------------------------------------------------------------
// Lado de publicación
// ---------------------------------------------------------------------------------------------

// Recorder es el application.EventRecorder del outbox de integración: ejecuta los traductores de
// un contexto delimitado y añade a store los eventos de integración resultantes. Se combina con el
// registrador del outbox de dominio mediante outbox.Recorders.
type Recorder struct {
	source      string
	store       application.OutboxStore
	mu          sync.RWMutex
	byType      map[string][]application.Translator
	translators []application.Translator
}

var _ application.EventRecorder = (*Recorder)(nil)

// NewRecorder crea un registrador para el contexto delimitado source sobre el outbox de integración.
func NewRecorder(source string, store application.OutboxStore) *Recorder {
	return &Recorder{source: source, store: store, byType: map[string][]application.Translator{}}
}

// Use registra un traductor que ve todos los eventos de dominio.
func (r *Recorder) Use(t application.Translator) *Recorder {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.translators = append(r.translators, t)
	return r
}

// On registra una traducción tipada para el evento de dominio E.
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

// Record implementa application.EventRecorder.
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

// Envelope construye el sobre de cable de un mensaje del outbox de integración.
func Envelope(source string, m application.OutboxMessage) application.Envelope {
	return application.Envelope{
		ID: m.ID, Type: m.EventType, Source: source, Subject: m.AggregateID, OccurredAt: m.OccurredAt,
		CorrelationID: m.CorrelationID, CausationID: m.CausationID, Data: json.RawMessage(m.Payload),
	}
}

// NewRelay reenvía el outbox de integración de source a sender (al menos una vez: los consumidores
// eliminan duplicados con su inbox). Se ejecuta con Relay.Run o Relay.RelayOnce.
func NewRelay(source string, store application.OutboxStore, sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	opts = append([]outbox.RelayOption{outbox.WithRelayName(source)}, opts...)
	return outbox.NewForwarder(store, func(ctx context.Context, m application.OutboxMessage) error {
		return sender.Send(ctx, Envelope(source, m))
	}, opts...)
}

// ---------------------------------------------------------------------------------------------
// Lado de consumo
// ---------------------------------------------------------------------------------------------

// Consumer es el application.MessageHandler de un contexto delimitado aguas abajo. Cada mensaje
// manejado se ejecuta en una unidad de trabajo con su reclamación del inbox: los duplicados se
// saltan y un manejador que falla deshace la reclamación, así que la reentrega se procesa de nuevo.
type Consumer struct {
	name     string
	inbox    application.InboxStore
	uow      domain.UnitOfWork
	mu       sync.RWMutex
	handlers map[string]func(ctx context.Context, env application.Envelope) error
	tel      *consumerTelemetry
}

var _ application.MessageHandler = (*Consumer)(nil)

// NewConsumer crea el consumidor name (la partición del inbox, normalmente el contexto delimitado).
func NewConsumer(name string, inbox application.InboxStore, uow domain.UnitOfWork) *Consumer {
	return &Consumer{name: name, inbox: inbox, uow: uow, handlers: map[string]func(context.Context, application.Envelope) error{}}
}

// Name devuelve el nombre del consumidor.
func (c *Consumer) Name() string { return c.name }

// Handle registra un manejador tipado para el evento de integración E. El consumidor es dueño de
// su copia del contrato del proveedor (un struct con los campos que necesita): los campos
// desconocidos se ignoran, lo que permite al proveedor añadir campos sin romperlo.
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

// Types devuelve los tipos de evento de integración que maneja el consumidor (para suscribir un transporte).
func (c *Consumer) Types() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.handlers))
	for t := range c.handlers {
		out = append(out, t)
	}
	return out
}

// HandleMessage implementa application.MessageHandler. Los mensajes de tipos sin manejador se
// confirman sin efecto.
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
		// Se rechaza, pero se cuenta y se registra igualmente: un productor que envía sobres sin id
		// es un fallo que hay que ver en las métricas, no un descarte silencioso.
		err := fmt.Errorf("%w: envelope without id", domain.ErrValidation)
		done(false, err)
		return err
	}
	// Un manejador que entra en pánico deshace su unidad de trabajo (uow.Do lo relanza): el span se
	// cierra y el fallo se cuenta antes de que el pánico siga su curso.
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

// panicError convierte un valor recuperado en el error que dejó un pánico.
func panicError(r any) error {
	if err, ok := r.(error); ok {
		return fmt.Errorf("panic: %w", err)
	}
	return fmt.Errorf("panic: %v", r)
}

// Lag devuelve cuánto hace que ocurrió el mensaje (para métricas y logs).
func Lag(env application.Envelope) time.Duration { return domain.Now().Sub(env.OccurredAt) }

// Package events ofrece suscripciones tipadas y el Registry de tipos de evento que se usa para
// decodificar eventos serializados (relay del outbox, brokers de mensajería). Implementa application.EventDecoder.
//
// Los contratos están en otra parte: domain.Event en la capa de dominio y application.Dispatcher /
// application.EventHandler en los contratos de aplicación. events/inprocess es un Dispatcher en
// memoria.
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Event es un alias de domain.Event por comodidad.
type Event = domain.Event

// Wildcard suscribe un manejador a todos los tipos de evento (application.WildcardEventType).
const Wildcard = application.WildcardEventType

// TypeOf devuelve el nombre del tipo de evento que declara el tipo valor E.
// Los eventos deben ser tipos valor cuyo método EventType funcione con el valor cero.
func TypeOf[E Event]() string {
	var zero E
	if reflect.TypeFor[E]().Kind() == reflect.Pointer {
		panic(fmt.Sprintf("events: %s must be a value type, not a pointer", reflect.TypeFor[E]()))
	}
	return zero.EventType()
}

// Subscribe registra un manejador tipado: fn sólo recibe eventos del tipo concreto E.
//
//	events.Subscribe(bus, func(ctx context.Context, e parties.PartyRegistered) error { ... })
func Subscribe[E Event](d application.Dispatcher, fn func(ctx context.Context, evt E) error) (unsubscribe func()) {
	return d.Subscribe(TypeOf[E](), application.EventHandlerFunc(func(ctx context.Context, evt Event) error {
		typed, ok := evt.(E)
		if !ok {
			return fmt.Errorf("events: handler for %s received %T", TypeOf[E](), evt)
		}
		return fn(ctx, typed)
	}))
}

// Registry asocia nombres de tipo de evento a tipos concretos de Go para poder decodificar los eventos serializados.
type Registry struct {
	mu       sync.RWMutex
	decoders map[string]func([]byte) (Event, error)
}

// NewRegistry crea un registro vacío.
func NewRegistry() *Registry {
	return &Registry{decoders: make(map[string]func([]byte) (Event, error))}
}

// Register hace E decodificable bajo su nombre EventType.
func Register[E Event](r *Registry) {
	name := TypeOf[E]()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decoders[name] = func(payload []byte) (Event, error) {
		var evt E
		if err := json.Unmarshal(payload, &evt); err != nil {
			return nil, fmt.Errorf("events: decoding %s: %w", name, err)
		}
		return evt, nil
	}
}

// Decode reconstruye un evento a partir de su nombre de tipo y su carga JSON.
func (r *Registry) Decode(eventType string, payload []byte) (Event, error) {
	r.mu.RLock()
	dec, ok := r.decoders[eventType]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: event type %q is not registered", domain.ErrUnsupported, eventType)
	}
	return dec(payload)
}

// Encode serializa como JSON la carga de un evento.
func Encode(evt Event) ([]byte, error) {
	return json.Marshal(evt)
}

var _ application.EventDecoder = (*Registry)(nil)

// Package events provides typed subscriptions and the event type Registry used to decode
// serialized events (outbox relay, message brokers). It implements application.EventDecoder.
//
// Contracts live elsewhere: domain.Event in the domain layer and application.Dispatcher /
// application.EventHandler in the application contracts. events/inprocess is an in-memory
// Dispatcher.
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

// Event aliases domain.Event for convenience.
type Event = domain.Event

// Wildcard subscribes a handler to every event type (application.WildcardEventType).
const Wildcard = application.WildcardEventType

// TypeOf returns the event type name declared by the value type E.
// Events must be value types whose EventType method works on the zero value.
func TypeOf[E Event]() string {
	var zero E
	if reflect.TypeFor[E]().Kind() == reflect.Pointer {
		panic(fmt.Sprintf("events: %s must be a value type, not a pointer", reflect.TypeFor[E]()))
	}
	return zero.EventType()
}

// Subscribe registers a typed handler: fn only receives events of concrete type E.
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

// Registry maps event type names to concrete Go types so serialized events can be decoded.
type Registry struct {
	mu       sync.RWMutex
	decoders map[string]func([]byte) (Event, error)
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{decoders: make(map[string]func([]byte) (Event, error))}
}

// Register makes E decodable under its EventType name.
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

// Decode rebuilds an event from its type name and JSON payload.
func (r *Registry) Decode(eventType string, payload []byte) (Event, error) {
	r.mu.RLock()
	dec, ok := r.decoders[eventType]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: event type %q is not registered", domain.ErrUnsupported, eventType)
	}
	return dec(payload)
}

// Encode serializes an event payload as JSON.
func Encode(evt Event) ([]byte, error) {
	return json.Marshal(evt)
}

var _ application.EventDecoder = (*Registry)(nil)

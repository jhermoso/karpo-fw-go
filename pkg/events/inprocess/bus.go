// Package inprocess provides an in-memory implementation of events.Dispatcher.
package inprocess

import (
	"context"
	"errors"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/events"
)

type subscription struct {
	id      uint64
	handler events.Handler
}

// Bus is a thread-safe in-memory event dispatcher. Handlers run synchronously in the
// publisher's goroutine; errors from several handlers are joined.
type Bus struct {
	mu          sync.RWMutex
	counter     uint64
	subscribers map[string][]subscription
}

// InProcessBus is kept as an alias for backwards compatibility.
type InProcessBus = Bus

// New creates a new Bus.
func New() *Bus {
	return &Bus{subscribers: make(map[string][]subscription)}
}

// Subscribe registers a handler for an event type (or events.Wildcard).
func (b *Bus) Subscribe(eventType string, handler events.Handler) func() {
	b.mu.Lock()
	b.counter++
	subID := b.counter
	b.subscribers[eventType] = append(b.subscribers[eventType], subscription{id: subID, handler: handler})
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subscribers[eventType]
		for i, s := range subs {
			if s.id == subID {
				b.subscribers[eventType] = append(subs[:i:i], subs[i+1:]...)
				break
			}
		}
	}
}

// Publish runs every handler subscribed to evt.EventType() and to the wildcard.
func (b *Bus) Publish(ctx context.Context, evt events.Event) error {
	if evt == nil {
		return errors.New("inprocess: cannot publish nil event")
	}

	b.mu.RLock()
	subs := append([]subscription(nil), b.subscribers[evt.EventType()]...)
	subs = append(subs, b.subscribers[events.Wildcard]...)
	b.mu.RUnlock()

	var errs []error
	for _, s := range subs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.handler.Handle(ctx, evt); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

var _ events.Dispatcher = (*Bus)(nil)

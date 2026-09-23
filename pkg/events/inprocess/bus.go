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

// InProcessBus is a thread-safe in-memory event dispatcher.
type InProcessBus struct {
	mu          sync.RWMutex
	counter     uint64
	subscribers map[string][]subscription
}

// New creates a new InProcessBus.
func New() *InProcessBus {
	return &InProcessBus{
		subscribers: make(map[string][]subscription),
	}
}

// Subscribe registers a handler for a specific event type. It returns an unsubscribe function.
func (b *InProcessBus) Subscribe(eventType string, handler events.Handler) func() {
	b.mu.Lock()
	b.counter++
	subID := b.counter
	b.subscribers[eventType] = append(b.subscribers[eventType], subscription{
		id:      subID,
		handler: handler,
	})
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		subs := b.subscribers[eventType]
		for i, s := range subs {
			if s.id == subID {
				b.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
}

// Publish executes all registered handlers for evt.Type(). If multiple handlers fail, errors are joined.
func (b *InProcessBus) Publish(ctx context.Context, evt events.Event) error {
	if evt == nil {
		return errors.New("cannot publish nil event")
	}

	b.mu.RLock()
	subs := append([]subscription(nil), b.subscribers[evt.Type()]...)
	// Also check wildcard subscribers "*"
	wildcards := append([]subscription(nil), b.subscribers["*"]...)
	b.mu.RUnlock()

	allSubs := append(subs, wildcards...)
	if len(allSubs) == 0 {
		return nil
	}

	var errs []error
	for _, s := range allSubs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := s.handler.Handle(ctx, evt); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

var _ events.Dispatcher = (*InProcessBus)(nil)

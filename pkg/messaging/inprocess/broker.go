// Package inprocess is an in-memory message transport: an application.MessageSender that fans
// every envelope out to the subscribed consumers of the same process. It serves the modular
// monolith (bounded contexts deployed together but coupled only through integration events)
// and tests; a broker adapter (NATS, RabbitMQ, Kafka...) replaces it without touching the
// publishing or consuming code.
package inprocess

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
)

// Broker delivers synchronously to every subscription whose types include the envelope type.
// Send fails when any consumer fails, so the relay retries the message; consumers that already
// processed it discard the duplicate through their inbox.
type Broker struct {
	mu   sync.RWMutex
	subs []subscription
	seq  uint64
}

type subscription struct {
	id       uint64
	consumer string
	types    []string // empty: every type
	handler  application.MessageHandler
}

var (
	_ application.MessageSender     = (*Broker)(nil)
	_ application.MessageSubscriber = (*Broker)(nil)
)

// NewBroker creates an empty broker.
func NewBroker() *Broker { return &Broker{} }

// Subscribe registers a consumer for the given types (all types when none) and returns an
// unsubscribe function.
func (b *Broker) Subscribe(consumer string, h application.MessageHandler, types ...string) (unsubscribe func()) {
	b.add(consumer, h, types)
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.subs = slices.DeleteFunc(b.subs, func(s subscription) bool { return s.consumer == consumer })
	}
}

// Attach implements application.MessageSubscriber. Replicas of a consumer all receive every
// message (there is nothing to share inside one process); their common inbox keeps the effect
// to one.
func (b *Broker) Attach(_ context.Context, consumer string, h application.MessageHandler, types ...string) (detach func() error, err error) {
	id := b.add(consumer, h, types)
	return func() error {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.subs = slices.DeleteFunc(b.subs, func(s subscription) bool { return s.id == id })
		return nil
	}, nil
}

func (b *Broker) add(consumer string, h application.MessageHandler, types []string) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	b.subs = append(b.subs, subscription{id: b.seq, consumer: consumer, types: slices.Clone(types), handler: h})
	return b.seq
}

// Send implements application.MessageSender.
func (b *Broker) Send(ctx context.Context, envelopes ...application.Envelope) error {
	b.mu.RLock()
	subs := slices.Clone(b.subs)
	b.mu.RUnlock()
	var errs []error
	for _, env := range envelopes {
		for _, s := range subs {
			if len(s.types) > 0 && !slices.Contains(s.types, env.Type) {
				continue
			}
			if err := s.handler.HandleMessage(ctx, env); err != nil {
				errs = append(errs, fmt.Errorf("consumer %s, message %s (%s): %w", s.consumer, env.ID, env.Type, err))
			}
		}
	}
	return errors.Join(errs...)
}

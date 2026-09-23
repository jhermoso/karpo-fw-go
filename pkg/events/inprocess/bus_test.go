package inprocess_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/events/inprocess"
)

func TestInProcessBus_PublishSubscribe(t *testing.T) {
	bus := inprocess.New()
	ctx := context.Background()

	var received []events.Event
	var mu sync.Mutex

	unsubscribe := bus.Subscribe("party.created", events.HandlerFunc(func(_ context.Context, evt events.Event) error {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, evt)
		return nil
	}))

	evt1 := events.BaseEvent{
		EventID:        "evt-1",
		EventType:      "party.created",
		EventTimestamp: time.Now(),
		EventPayload:   "Acme Corp",
	}

	if err := bus.Publish(ctx, evt1); err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	mu.Lock()
	if len(received) != 1 || received[0].ID() != "evt-1" {
		t.Fatalf("expected 1 event received, got: %v", received)
	}
	mu.Unlock()

	// Unsubscribe and verify no more events received
	unsubscribe()

	evt2 := events.BaseEvent{
		EventID:        "evt-2",
		EventType:      "party.created",
		EventTimestamp: time.Now(),
	}
	_ = bus.Publish(ctx, evt2)

	mu.Lock()
	if len(received) != 1 {
		t.Fatalf("expected handler not to be called after unsubscribe")
	}
	mu.Unlock()
}

func TestInProcessBus_WildcardAndErrors(t *testing.T) {
	bus := inprocess.New()
	ctx := context.Background()

	var wildcardCount int
	bus.Subscribe("*", events.HandlerFunc(func(_ context.Context, _ events.Event) error {
		wildcardCount++
		return nil
	}))

	expectedErr := errors.New("handler failed")
	bus.Subscribe("order.placed", events.HandlerFunc(func(_ context.Context, _ events.Event) error {
		return expectedErr
	}))

	err := bus.Publish(ctx, events.BaseEvent{
		EventType: "order.placed",
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected joined error to contain %v, got %v", expectedErr, err)
	}
	if wildcardCount != 1 {
		t.Fatalf("expected wildcard handler to execute once")
	}
}

func TestInProcessBus_ContextCancelled(t *testing.T) {
	bus := inprocess.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	bus.Subscribe("test.event", events.HandlerFunc(func(_ context.Context, _ events.Event) error {
		return nil
	}))

	err := bus.Publish(ctx, events.BaseEvent{EventType: "test.event"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}
}

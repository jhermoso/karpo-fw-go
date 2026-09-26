package inprocess_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/events/inprocess"
)

type partyCreated struct {
	domain.EventMeta
	Name string `json:"name"`
}

func (partyCreated) EventType() string { return "party.created" }

type orderPlaced struct{ domain.EventMeta }

func (orderPlaced) EventType() string { return "order.placed" }

func TestBus_TypedSubscribeAndUnsubscribe(t *testing.T) {
	bus := inprocess.New()
	ctx := context.Background()

	var received []string
	unsubscribe := events.Subscribe(bus, func(_ context.Context, e partyCreated) error {
		received = append(received, e.Name)
		return nil
	})

	if err := bus.Publish(ctx, partyCreated{EventMeta: domain.NewEventMeta(), Name: "Acme"}); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(ctx, orderPlaced{EventMeta: domain.NewEventMeta()}); err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 || received[0] != "Acme" {
		t.Fatalf("expected only the typed event, got %v", received)
	}

	unsubscribe()
	_ = bus.Publish(ctx, partyCreated{Name: "Other"})
	if len(received) != 1 {
		t.Fatal("handler called after unsubscribe")
	}
}

func TestBus_WildcardAndJoinedErrors(t *testing.T) {
	bus := inprocess.New()
	var wildcard int
	bus.Subscribe(events.Wildcard, application.EventHandlerFunc(func(context.Context, events.Event) error {
		wildcard++
		return nil
	}))
	boom := errors.New("handler failed")
	bus.Subscribe("order.placed", application.EventHandlerFunc(func(context.Context, events.Event) error { return boom }))

	err := bus.Publish(context.Background(), orderPlaced{})
	if !errors.Is(err, boom) || wildcard != 1 {
		t.Fatalf("err=%v wildcard=%d", err, wildcard)
	}
}

func TestBus_ContextCancelled(t *testing.T) {
	bus := inprocess.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bus.Subscribe("order.placed", application.EventHandlerFunc(func(context.Context, events.Event) error { return nil }))
	if err := bus.Publish(ctx, orderPlaced{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRegistry_RoundTrip(t *testing.T) {
	reg := events.NewRegistry()
	events.Register[partyCreated](reg)

	original := partyCreated{EventMeta: domain.NewEventMeta(), Name: "Acme"}
	payload, err := events.Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := reg.Decode("party.created", payload)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decoded.(partyCreated)
	if !ok || got.Name != "Acme" || got.Meta().EventID != original.EventID {
		t.Fatalf("round trip failed: %#v", decoded)
	}
	if _, err := reg.Decode("unknown", payload); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("unknown type must be ErrUnsupported, got %v", err)
	}
}

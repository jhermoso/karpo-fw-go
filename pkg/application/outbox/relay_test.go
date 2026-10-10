package outbox_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// fill appends one message per "aggregate/seq" label, one millisecond apart.
func fill(t *testing.T, store application.OutboxStore, labels ...string) {
	t.Helper()
	base := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	for i, l := range labels {
		m := application.OutboxMessage{ID: l, EventType: "things.thing-happened.v1", AggregateType: "thing",
			AggregateID: l[:1], Payload: []byte(`{}`), OccurredAt: base.Add(time.Duration(i) * time.Millisecond)}
		if err := store.Append(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
}

func attempts(t *testing.T, store application.OutboxStore) map[string]int {
	t.Helper()
	msgs, err := store.Pending(context.Background(), 100, 1000)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, m := range msgs {
		out[m.ID] = m.Attempts
	}
	return out
}

func TestRelay_KeepsTheOrderOfAnAggregateWhenADeliveryFails(t *testing.T) {
	ctx := context.Background()
	store := memory.NewOutbox(memory.NewStore("s"))
	fill(t, store, "a1", "b1", "a2", "a3")
	var sent []string
	fail := true
	relay := outbox.NewForwarder(store, func(_ context.Context, m application.OutboxMessage) error {
		if m.ID == "a1" && fail {
			fail = false
			return errors.New("consumer down")
		}
		sent = append(sent, m.ID)
		return nil
	})

	if n, err := relay.RelayOnce(ctx); n != 1 || err != nil {
		t.Fatalf("first pass: %d %v", n, err)
	}
	if !slices.Equal(sent, []string{"b1"}) {
		t.Fatalf("the later facts about a must wait behind the failed one: %v", sent)
	}
	if got := attempts(t, store); got["a1"] != 1 || got["a2"] != 0 || got["a3"] != 0 {
		t.Fatalf("only the failed message counts an attempt: %v", got)
	}
	if n, err := relay.RelayOnce(ctx); n != 3 || err != nil {
		t.Fatalf("second pass: %d %v", n, err)
	}
	if !slices.Equal(sent, []string{"b1", "a1", "a2", "a3"}) {
		t.Fatalf("order of a: %v", sent)
	}
}

func TestRelay_ATransportOutageDoesNotCountAttempts(t *testing.T) {
	ctx := context.Background()
	store := memory.NewOutbox(memory.NewStore("s"))
	fill(t, store, "a1", "b1", "a2")
	down, calls := true, 0
	relay := outbox.NewForwarder(store, func(context.Context, application.OutboxMessage) error {
		calls++
		if down {
			return fmt.Errorf("broker: %w", application.ErrTransportUnavailable)
		}
		return nil
	}, outbox.WithMaxAttempts(3))

	for range 20 {
		if n, err := relay.RelayOnce(ctx); n != 0 || !errors.Is(err, application.ErrTransportUnavailable) {
			t.Fatalf("outage: %d %v", n, err)
		}
	}
	if calls != 20 {
		t.Fatalf("a pass must stop at the first message the transport cannot take: %d sends in 20 passes", calls)
	}
	if got := attempts(t, store); len(got) != 3 || got["a1"] != 0 {
		t.Fatalf("an outage says nothing about the messages: %v", got)
	}
	down = false
	if n, err := relay.RelayOnce(ctx); n != 3 || err != nil {
		t.Fatalf("after the outage: %d %v", n, err)
	}
}

func TestRelay_WithClaims(t *testing.T) {
	ctx := context.Background()
	db := memory.NewStore("s")
	store := memory.NewOutbox(db)
	fill(t, store, "a1", "b1")
	var sent []string
	fail := true
	relay := outbox.NewForwarder(store, func(ctx context.Context, m application.OutboxMessage) error {
		// The delivery runs outside the unit of work that holds the messages.
		if _, err := store.Claim(ctx, 1, 1); err == nil {
			t.Error("the unit of work of the relay must not reach the delivery")
		}
		if m.ID == "b1" && fail {
			fail = false
			return errors.New("consumer down")
		}
		sent = append(sent, m.ID)
		return nil
	}, outbox.WithClaims(db))

	if n, err := relay.RelayOnce(ctx); n != 1 || err != nil {
		t.Fatalf("first pass: %d %v", n, err)
	}
	if got := attempts(t, store); len(got) != 1 || got["b1"] != 1 {
		t.Fatalf("the outcomes of a pass with failures are still recorded: %v", got)
	}
	if n, err := relay.RelayOnce(ctx); n != 1 || err != nil || !slices.Equal(sent, []string{"a1", "b1"}) {
		t.Fatalf("second pass: %d %v %v", n, err, sent)
	}

	plain := outbox.NewForwarder(pendingOnly{store}, func(context.Context, application.OutboxMessage) error { return nil }, outbox.WithClaims(db))
	if _, err := plain.RelayOnce(ctx); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("a store without claims cannot be shared: %v", err)
	}
}

// pendingOnly hides the claims of a store.
type pendingOnly struct{ application.OutboxStore }

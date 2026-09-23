package testkit_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/testkit"
)

func TestHarness(t *testing.T) {
	initial := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	h := testkit.NewHarness(initial)

	if !h.Clock.Now().Equal(initial) {
		t.Fatalf("expected clock to match initial time")
	}

	var count int
	h.Bus.Subscribe("test.event", events.HandlerFunc(func(_ context.Context, _ events.Event) error {
		count++
		return nil
	}))

	_ = h.Bus.Publish(context.Background(), events.BaseEvent{EventType: "test.event"})
	if count != 1 {
		t.Fatalf("expected bus in harness to deliver event")
	}
}

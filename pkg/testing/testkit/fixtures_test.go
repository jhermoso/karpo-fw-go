package testkit_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/testkit"
)

type pinged struct{ domain.EventMeta }

func (pinged) EventType() string { return "test.pinged" }

func TestHarness(t *testing.T) {
	initial := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	h := testkit.NewHarness(t, initial)

	if !h.Clock.Now().Equal(initial) || !domain.Now().Equal(initial) {
		t.Fatalf("expected harness and domain clocks to match initial time")
	}

	var count int
	events.Subscribe(h.Bus, func(_ context.Context, _ pinged) error {
		count++
		return nil
	})
	if err := h.Bus.Publish(context.Background(), pinged{EventMeta: domain.NewEventMeta()}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected bus in harness to deliver event")
	}
}

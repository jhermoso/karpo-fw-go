package outbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	metricsvanilla "github.com/jhermoso/karpo-fw-go/pkg/metrics/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// Two relays with the default name on the same meter (two bounded contexts, each with its domain
// relay): the oldest pending message of either one shows in the gauge, whichever was built last.
func TestRelay_PendingAgeOfRelaysSharingAName(t *testing.T) {
	reg := metricsvanilla.NewRegistry()
	deliver := func(context.Context, application.OutboxMessage) error { return nil }

	stuck := memory.NewOutbox(memory.NewStore("stuck"))
	if err := stuck.Append(context.Background(), application.OutboxMessage{ID: "m-1", EventType: "widget.renamed",
		Payload: []byte(`{}`), OccurredAt: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	outbox.NewForwarder(stuck, deliver, outbox.WithRelayTelemetry(nil, reg))
	outbox.NewForwarder(memory.NewOutbox(memory.NewStore("empty")), deliver, outbox.WithRelayTelemetry(nil, reg))

	age, ok := reg.GaugeValue(metrics.OutboxOldestPendingAge, "outbox", "outbox")
	if !ok || age < 3500 {
		t.Fatalf("the stuck outbox must show in the shared series: %v %v", age, ok)
	}
	if n := len(reg.SeriesLabels(metrics.OutboxOldestPendingAge)); n != 1 {
		t.Fatalf("one series per outbox name: %v", reg.SeriesLabels(metrics.OutboxOldestPendingAge))
	}

	// A named relay keeps its own series.
	outbox.NewForwarder(memory.NewOutbox(memory.NewStore("billing")), deliver, outbox.WithRelayName("billing"), outbox.WithRelayTelemetry(nil, reg))
	if age, ok := reg.GaugeValue(metrics.OutboxOldestPendingAge, "outbox", "billing"); !ok || age != 0 {
		t.Fatalf("an empty named outbox reports 0: %v %v", age, ok)
	}
}

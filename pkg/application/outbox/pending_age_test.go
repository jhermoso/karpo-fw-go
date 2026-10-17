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

// Dos relays con el nombre por defecto en el mismo meter (dos contextos delimitados, cada uno con su
// relay de dominio): el mensaje pendiente más antiguo de cualquiera de ellos se ve en el indicador, sea
// cual sea el último construido.
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

	// Un relay con nombre conserva su propia serie.
	outbox.NewForwarder(memory.NewOutbox(memory.NewStore("billing")), deliver, outbox.WithRelayName("billing"), outbox.WithRelayTelemetry(nil, reg))
	if age, ok := reg.GaugeValue(metrics.OutboxOldestPendingAge, "outbox", "billing"); !ok || age != 0 {
		t.Fatalf("an empty named outbox reports 0: %v %v", age, ok)
	}
}

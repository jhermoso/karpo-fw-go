package messaging_test

import (
	"context"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	metricsvanilla "github.com/jhermoso/karpo-fw-go/pkg/metrics/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	tracevanilla "github.com/jhermoso/karpo-fw-go/pkg/trace/vanilla"
)

// A panicking handler and an envelope without id leave their span and their count: neither is a
// silent drop.
func TestConsumer_PanicsAndEnvelopesWithoutIdAreCounted(t *testing.T) {
	store := memory.NewStore("s")
	spans := tracevanilla.NewRecorder(0)
	reg := metricsvanilla.NewRegistry()
	c := messaging.NewConsumer("crm", memory.NewInbox(store), store).
		WithTelemetry(log.Discard(), tracevanilla.New(tracevanilla.WithExporter(spans)), reg)
	messaging.Handle(c, func(context.Context, accountOpenedV1, application.Envelope) error { panic("handler bug") })
	const typ = "accounts.account-opened.v1"

	func() {
		defer func() {
			if p := recover(); p != "handler bug" {
				t.Fatalf("the panic must keep propagating, got %v", p)
			}
		}()
		_ = c.HandleMessage(context.Background(), application.Envelope{ID: "m-1", Type: typ, Data: []byte(`{}`)})
	}()
	if err := c.HandleMessage(context.Background(), application.Envelope{Type: typ, Data: []byte(`{}`)}); err == nil {
		t.Fatal("an envelope without id must be rejected")
	}

	if n := reg.CounterValue(metrics.MessagingHandled, "consumer", "crm", "event_type", typ, "outcome", application.OutcomeError); n != 2 {
		t.Fatalf("both failures must be counted: %v", reg.SeriesLabels(metrics.MessagingHandled))
	}
	if got := spans.Spans(); len(got) != 2 || got[0].Err == nil || got[1].Err == nil {
		t.Fatalf("both spans must be closed with their error: %+v", got)
	}
}

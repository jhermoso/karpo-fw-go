package outbox_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/observability"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

func seed(t *testing.T) *memory.Outbox {
	t.Helper()
	store := memory.NewOutbox(memory.NewStore("memory"))
	ctx := context.Background()
	for _, m := range []application.OutboxMessage{
		{ID: "m1", EventType: "party.registered", Payload: []byte(`{}`), CorrelationID: "corr-a"},
		{ID: "m2", EventType: "party.renamed", Payload: []byte(`{}`), CorrelationID: "corr-b"},
	} {
		if err := store.Append(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// A relay built without a logger no longer swallows failures: they go to slog.Default.
func TestRelay_IsNotSilentByDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	relay := outbox.NewForwarder(seed(t), func(_ context.Context, m application.OutboxMessage) error {
		if m.EventType == "party.renamed" {
			return errors.New("broker unreachable")
		}
		return nil
	})
	if n, err := relay.RelayOnce(context.Background()); n != 1 || err != nil {
		t.Fatalf("delivered %d, err %v", n, err)
	}
	out := buf.String()
	if !strings.Contains(out, `"msg":"outbox delivery failed"`) || !strings.Contains(out, `"error":"broker unreachable"`) ||
		!strings.Contains(out, `"event_type":"party.renamed"`) || !strings.Contains(out, `"attempt":1`) {
		t.Fatalf("failure not logged by default: %q", out)
	}
}

func TestRelay_CountsDeliveriesAndFailures(t *testing.T) {
	reg := inprocess.NewRegistry()
	relay := outbox.NewForwarder(seed(t), func(_ context.Context, m application.OutboxMessage) error {
		if m.EventType == "party.renamed" {
			return errors.New("broker unreachable")
		}
		return nil
	}, outbox.WithRelayTelemetry(observability.Telemetry{Meter: reg}), outbox.WithRelayLogger(discard{}))
	for i := 0; i < 2; i++ {
		if _, err := relay.RelayOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	d, _ := reg.Find(observability.MetricOutboxDelivered)
	f, _ := reg.Find(observability.MetricOutboxFailed)
	if len(d.Series) != 1 || d.Series[0].Value != 1 || d.Series[0].Attr(observability.AttrEventType) != "party.registered" {
		t.Fatalf("delivered: %+v", d)
	}
	if len(f.Series) != 1 || f.Series[0].Value != 2 || f.Series[0].Attr(observability.AttrEventType) != "party.renamed" {
		t.Fatalf("failed: %+v", f)
	}
}

type discard struct{}

func (discard) Debug(string, ...any)                     {}
func (discard) Info(string, ...any)                      {}
func (discard) Warn(string, ...any)                      {}
func (discard) Error(string, ...any)                     {}
func (d discard) With(...any) log.Logger                 { return d }
func (d discard) WithContext(context.Context) log.Logger { return d }

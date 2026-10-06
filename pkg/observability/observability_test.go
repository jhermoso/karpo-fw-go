package observability_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/observability"
)

func TestParseTraceparent(t *testing.T) {
	const valid = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	sc, ok := observability.ParseTraceparent(valid)
	if !ok || !sc.Sampled || !sc.Remote || sc.TraceID.String() != "4bf92f3577b34da6a3ce929d0e0e4736" || sc.SpanID.String() != "00f067aa0ba902b7" {
		t.Fatalf("valid traceparent: %+v %v", sc, ok)
	}
	if got := sc.Traceparent(); got != valid {
		t.Fatalf("round trip: %q", got)
	}
	for _, bad := range []string{
		"",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-0",             // short
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",            // upper case
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",            // zero trace id
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",            // zero span id
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",            // forbidden version
		"00_4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",            // separator
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01\nx-evil: 1", // injection
	} {
		if _, ok := observability.ParseTraceparent(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestNoopAndContext(t *testing.T) {
	var tel observability.Telemetry // zero value is usable
	ctx, span := tel.T().Start(context.Background(), "op")
	span.SetAttributes(observability.String("k", "v"))
	span.RecordError(errors.New("x"))
	span.SetStatus(observability.StatusError, "x")
	span.End()
	tel.M().Counter("c", "1", "").Add(ctx, 1)
	tel.M().Histogram("h", "s", "").Record(ctx, 1)
	if span.Context().IsValid() {
		t.Fatal("noop span has an identity")
	}
	if tr, sp := observability.TraceIDs(ctx); tr != "" || sp != "" {
		t.Fatalf("no trace ids expected: %q %q", tr, sp)
	}
	if _, ok := observability.ParentFromContext(ctx); ok {
		t.Fatal("no parent expected")
	}
	sc, _ := observability.ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00")
	ctx = observability.ContextWithRemoteSpanContext(ctx, sc)
	if p, ok := observability.ParentFromContext(ctx); !ok || p.TraceID != sc.TraceID || !p.Remote {
		t.Fatalf("remote parent: %+v %v", p, ok)
	}
	if observability.Int("n", 42).Value != "42" {
		t.Fatal("Int")
	}
}

package trace_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

const valid = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func TestParseTraceParent(t *testing.T) {
	cases := []struct {
		name, header string
		ok           bool
	}{
		{"valid sampled", valid, true},
		{"valid not sampled", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00", true},
		{"surrounding spaces", "  " + valid + " ", true},
		{"unknown version", "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", true},
		{"unknown version with extra field", "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra", true},
		{"version 00 with extra field", valid + "-extra", false},
		{"version ff", "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", false},
		{"all-zero trace id", "00-00000000000000000000000000000000-00f067aa0ba902b7-01", false},
		{"all-zero span id", "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01", false},
		{"uppercase", strings.ToUpper(valid), false},
		{"too short", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7", false},
		{"not hex", "00-4bf92f3577b34da6a3ce929d0e0e473z-00f067aa0ba902b7-01", false},
		{"wrong separators", strings.ReplaceAll(valid, "-", "_"), false},
		{"empty", "", false},
		{"long garbage", strings.Repeat("a", 200), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sc, ok := trace.ParseTraceParent(c.header)
			if ok != c.ok {
				t.Fatalf("ParseTraceParent(%q) ok = %v, want %v", c.header, ok, c.ok)
			}
			if !ok && sc != (trace.SpanContext{}) {
				t.Fatalf("a rejected header must return the zero context, got %+v", sc)
			}
		})
	}
}

func TestTraceParent_RoundTrip(t *testing.T) {
	sc, ok := trace.ParseTraceParent(valid)
	if !ok {
		t.Fatal("expected a valid header")
	}
	if sc.TraceIDString() != "4bf92f3577b34da6a3ce929d0e0e4736" || sc.SpanIDString() != "00f067aa0ba902b7" || !sc.Sampled {
		t.Fatalf("unexpected context: %+v", sc)
	}
	if got := sc.TraceParent(); got != valid {
		t.Fatalf("TraceParent() = %q, want %q", got, valid)
	}
	if got := (trace.SpanContext{}).TraceParent(); got != "" {
		t.Fatalf("an invalid context must render no header, got %q", got)
	}
}

func TestNoop_LeavesContextUntouched(t *testing.T) {
	ctx := context.Background()
	got, span := trace.Noop().Start(ctx, "anything", trace.WithKind(trace.KindServer))
	if got != ctx {
		t.Fatal("the no-op tracer must return the context it received")
	}
	span.SetName("x")
	span.SetAttributes("k", "v")
	span.RecordError(errors.New("boom"))
	span.End()
	if span.Context().IsValid() {
		t.Fatal("a no-op span has no identifiers")
	}
	if trace.FromContext(ctx) == nil {
		t.Fatal("FromContext never returns nil")
	}
	if attrs := trace.LogAttrs(ctx); attrs != nil {
		t.Fatalf("no span, no log attributes; got %v", attrs)
	}
	if trace.OrNoop(nil) == nil {
		t.Fatal("OrNoop(nil) must return a tracer")
	}
}

func TestStartConfig(t *testing.T) {
	parent, _ := trace.ParseTraceParent(valid)
	c := trace.NewStartConfig(trace.WithKind(trace.KindConsumer), trace.WithRemoteParent(parent), trace.WithNewRoot())
	if c.Kind != trace.KindConsumer || c.RemoteParent != parent || !c.NewRoot {
		t.Fatalf("unexpected config: %+v", c)
	}
	if trace.KindClient.String() != "client" || trace.Kind(99).String() != "internal" {
		t.Fatal("unexpected kind names")
	}
}

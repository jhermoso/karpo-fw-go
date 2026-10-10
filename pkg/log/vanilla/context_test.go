package vanilla_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
	tracevanilla "github.com/jhermoso/karpo-fw-go/pkg/trace/vanilla"
)

func line(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("not a JSON line: %v\n%s", err, buf.String())
	}
	return out
}

func TestVanillaLogger_ReadsTheContext(t *testing.T) {
	var buf bytes.Buffer
	logger := vanilla.NewJSON(&buf, log.LevelInfo)

	actorID := domain.NewUUID()
	actor, err := vocab.NewActor(actorID, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	ctx := application.WithCorrelationID(context.Background(), "corr-1")
	ctx = application.WithCausationID(ctx, "msg-9")
	ctx = application.WithActor(ctx, actor)
	ctx, span := tracevanilla.New().Start(ctx, "test")
	defer span.End()

	logger.WithContext(ctx).Info("order created", "order", 7)

	out := line(t, &buf)
	if out["correlation_id"] != "corr-1" || out["causation_id"] != "msg-9" {
		t.Fatalf("the line must carry the correlation and the causation: %v", out)
	}
	if out["trace_id"] != span.Context().TraceIDString() || out["span_id"] != span.Context().SpanIDString() {
		t.Fatalf("the line must carry the current span: %v", out)
	}
	if out["actor"] != actorID.String() {
		t.Fatalf("the line must carry the actor id: %v", out)
	}
	if strings.Contains(buf.String(), "Ada") {
		t.Fatalf("the actor name is personal data and must not be logged: %s", buf.String())
	}
	if out["order"] != float64(7) || out["msg"] != "order created" {
		t.Fatalf("the caller attributes must survive: %v", out)
	}
}

func TestVanillaLogger_WithoutContextAddsNothing(t *testing.T) {
	var buf bytes.Buffer
	vanilla.NewJSON(&buf, log.LevelInfo).With("service", "parties").Info("started")

	out := line(t, &buf)
	for _, k := range []string{"correlation_id", "causation_id", "trace_id", "span_id", "actor"} {
		if _, ok := out[k]; ok {
			t.Errorf("unexpected %q without a context: %v", k, out)
		}
	}
	if out["service"] != "parties" {
		t.Fatalf("With attributes must survive: %v", out)
	}
}

func TestVanillaLogger_ExplicitAttributeWins(t *testing.T) {
	var buf bytes.Buffer
	ctx := application.WithCorrelationID(context.Background(), "from-context")

	vanilla.NewJSON(&buf, log.LevelInfo).WithContext(ctx).With("service", "relay").
		Warn("delivery failed", "correlation_id", "from-message")

	if n := strings.Count(buf.String(), `"correlation_id"`); n != 1 {
		t.Fatalf("the key must appear once, got %d: %s", n, buf.String())
	}
	if out := line(t, &buf); out["correlation_id"] != "from-message" || out["service"] != "relay" {
		t.Fatalf("the explicit attribute must win: %v", out)
	}
}

func TestVanillaLogger_TextAlsoReadsTheContext(t *testing.T) {
	var buf bytes.Buffer
	ctx := application.WithCorrelationID(context.Background(), "corr-2")
	ctx = trace.ContextWithSpan(ctx, trace.FromContext(ctx)) // a no-op span adds nothing

	vanilla.NewText(&buf, log.LevelInfo).WithContext(ctx).Info("hello")

	if s := buf.String(); !strings.Contains(s, "correlation_id=corr-2") || strings.Contains(s, "trace_id") {
		t.Fatalf("unexpected text line: %s", s)
	}
}

package vanilla_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/log"
	logvanilla "github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
	"github.com/jhermoso/karpo-fw-go/pkg/trace/vanilla"
)

func TestTracer_ParentAndChildShareTheTrace(t *testing.T) {
	rec := vanilla.NewRecorder(0)
	tr := vanilla.New(vanilla.WithExporter(rec))

	ctx, parent := tr.Start(context.Background(), "parent", trace.WithKind(trace.KindServer))
	_, child := tr.Start(ctx, "child")
	child.SetAttributes("db.operation", "INSERT", "dangling")
	child.RecordError(errors.New("boom"))
	child.End()
	child.End() // a second End has no effect
	parent.SetName("GET /parties/{id}")
	parent.End()

	spans := rec.Spans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	c, p := spans[0], spans[1]
	if c.Name != "child" || p.Name != "GET /parties/{id}" {
		t.Fatalf("unexpected names: %q, %q", c.Name, p.Name)
	}
	if c.Context.TraceID != p.Context.TraceID {
		t.Fatal("the child must belong to the trace of its parent")
	}
	if c.ParentID != p.Context.SpanID || p.HasParent() {
		t.Fatal("the child must point at its parent, and the root at nobody")
	}
	if c.Context.SpanID == p.Context.SpanID {
		t.Fatal("every span has its own id")
	}
	if c.Attr("db.operation") != "INSERT" || len(c.Attributes) != 2 {
		t.Fatalf("unexpected attributes: %v", c.Attributes)
	}
	if c.Err == nil || p.Err != nil || p.Kind != trace.KindServer {
		t.Fatalf("unexpected error or kind: %+v / %+v", c, p)
	}
	if len(rec.Named("child")) != 1 {
		t.Fatal("Named must find the child")
	}
}

func TestTracer_ContinuesARemoteParent(t *testing.T) {
	rec := vanilla.NewRecorder(0)
	tr := vanilla.New(vanilla.WithExporter(rec))
	remote, _ := trace.ParseTraceParent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	ctx, s := tr.Start(context.Background(), "server", trace.WithRemoteParent(remote))
	if got := trace.FromContext(ctx).Context(); got != s.Context() {
		t.Fatal("the context must carry the started span")
	}
	s.End()

	got := rec.Spans()[0]
	if got.Context.TraceID != remote.TraceID || got.ParentID != remote.SpanID {
		t.Fatalf("the span must continue the remote trace: %+v", got)
	}

	_, root := tr.Start(ctx, "detached", trace.WithNewRoot())
	if root.Context().TraceID == remote.TraceID {
		t.Fatal("WithNewRoot must start another trace")
	}
}

func TestTracer_Sampling(t *testing.T) {
	rec := vanilla.NewRecorder(0)
	tr := vanilla.New(vanilla.WithExporter(rec), vanilla.WithSampleRatio(0))

	ctx, s := tr.Start(context.Background(), "unsampled")
	if !s.Context().IsValid() || s.Context().Sampled {
		t.Fatal("an unsampled span still has identifiers for the logs")
	}
	_, child := tr.Start(ctx, "child")
	child.End()
	s.End()
	if n := len(rec.Spans()); n != 0 {
		t.Fatalf("unsampled spans must not be exported, got %d", n)
	}

	// A remote parent keeps its own decision, whatever the local ratio is.
	remote, _ := trace.ParseTraceParent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	_, s2 := tr.Start(context.Background(), "remote", trace.WithRemoteParent(remote))
	s2.End()
	if n := len(rec.Spans()); n != 1 {
		t.Fatalf("a sampled remote parent must be honoured, got %d spans", n)
	}

	half := vanilla.New(vanilla.WithExporter(vanilla.NewRecorder(0)), vanilla.WithSampleRatio(0.5))
	sampled := 0
	for range 2000 {
		_, s := half.Start(context.Background(), "x")
		if s.Context().Sampled {
			sampled++
		}
	}
	if sampled < 800 || sampled > 1200 {
		t.Fatalf("a 0.5 ratio sampled %d of 2000", sampled)
	}
}

func TestRecorder_KeepsTheLastSpans(t *testing.T) {
	rec := vanilla.NewRecorder(2)
	tr := vanilla.New(vanilla.WithExporter(rec))
	for _, n := range []string{"a", "b", "c"} {
		_, s := tr.Start(context.Background(), n)
		s.End()
	}
	spans := rec.Spans()
	if len(spans) != 2 || spans[0].Name != "b" || spans[1].Name != "c" {
		t.Fatalf("unexpected spans: %+v", spans)
	}
	rec.Reset()
	if len(rec.Spans()) != 0 {
		t.Fatal("Reset must forget the spans")
	}
}

func TestLogExporter_WritesSpansAtDebug(t *testing.T) {
	var info, debug bytes.Buffer
	for _, c := range []struct {
		buf   *bytes.Buffer
		level log.Level
	}{{&info, log.LevelInfo}, {&debug, log.LevelDebug}} {
		tr := vanilla.New(vanilla.WithExporter(vanilla.LogExporter(logvanilla.NewJSON(c.buf, c.level))))
		ctx, parent := tr.Start(context.Background(), "parent")
		_, s := tr.Start(ctx, "db INSERT", trace.WithKind(trace.KindClient))
		s.SetAttributes("db.operation", "INSERT")
		s.RecordError(errors.New("boom"))
		s.End()
		parent.End()
	}
	if info.Len() != 0 {
		t.Fatalf("span lines must not reach the Info output: %s", info.String())
	}
	lines := strings.Split(strings.TrimSpace(debug.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 span lines, got %d", len(lines))
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"trace_id", "span_id", "parent_span_id", "duration_ms"} {
		if _, ok := line[k]; !ok {
			t.Errorf("the span line lacks %q: %v", k, line)
		}
	}
	if line["span"] != "db INSERT" || line["kind"] != "client" || line["error"] != "boom" || line["db.operation"] != "INSERT" {
		t.Fatalf("unexpected span line: %v", line)
	}
}

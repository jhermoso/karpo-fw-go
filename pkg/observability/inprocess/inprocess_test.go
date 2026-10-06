package inprocess_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/observability"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/inprocess"
)

func TestTracer_ParentChildAndRemote(t *testing.T) {
	tr := inprocess.NewTracer(2)
	ctx, root := tr.Start(context.Background(), "root", observability.WithKind(observability.SpanKindServer))
	_, child := tr.Start(ctx, "child", observability.WithAttributes(observability.String("k", "v")))
	if !root.Context().IsValid() || child.Context().TraceID != root.Context().TraceID || child.Context().SpanID == root.Context().SpanID {
		t.Fatalf("ids: root %+v child %+v", root.Context(), child.Context())
	}
	if tid, sid := observability.TraceIDs(ctx); tid != root.Context().TraceID.String() || sid != root.Context().SpanID.String() {
		t.Fatalf("TraceIDs: %s %s", tid, sid)
	}
	child.RecordError(errors.New("boom"))
	child.SetStatus(observability.StatusError, "boom")
	child.End()
	child.End() // second End is ignored
	root.End()
	fin := tr.Finished()
	if len(fin) != 2 || fin[0].Name != "child" || fin[0].Parent != root.Context() || fin[0].Attr("k") != "v" ||
		fin[0].Status != observability.StatusError || len(fin[0].Errors) != 1 || fin[1].Kind != observability.SpanKindServer {
		t.Fatalf("finished: %+v", fin)
	}

	remote, _ := observability.ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00")
	_, s := tr.Start(observability.ContextWithRemoteSpanContext(context.Background(), remote), "continued")
	if s.Context().TraceID != remote.TraceID || s.Context().Sampled {
		t.Fatalf("remote parent not honoured: %+v", s.Context())
	}
	s.End()
	if got := tr.Finished(); len(got) != 2 || got[1].Name != "continued" {
		t.Fatalf("bounded buffer: %+v", got)
	}
	if len(inprocess.NewTracer(0).Finished()) != 0 {
		t.Fatal("capacity 0 keeps nothing")
	}
}

func TestRegistry_CountersAndHistograms(t *testing.T) {
	reg := inprocess.NewRegistry()
	ctx := context.Background()
	c := reg.Counter("karpo.test", "1", "test")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Add(ctx, 1, observability.String("b", "2"), observability.String("a", "1"))
		}()
	}
	wg.Wait()
	c.Add(ctx, -5) // ignored: monotonic
	reg.Counter("karpo.test", "1", "again").Add(ctx, 1, observability.String("a", "1"), observability.String("b", "2"))

	h := reg.Histogram("karpo.duration", "s", "", 0.1, 1)
	for _, v := range []float64{0.05, 0.1, 0.5, 3} {
		h.Record(ctx, v)
	}

	m, ok := reg.Find("karpo.test")
	if !ok || len(m.Series) != 1 || m.Series[0].Value != 51 || m.Series[0].Attr("a") != "1" || m.Series[0].Attrs[0].Key != "a" {
		t.Fatalf("counter: %+v", m)
	}
	hs, _ := reg.Find("karpo.duration")
	s := hs.Series[0]
	if s.Count != 4 || s.Sum != 3.65 || s.Counts[0] != 2 || s.Counts[1] != 1 || s.Counts[2] != 1 {
		t.Fatalf("histogram: %+v", s)
	}
	if def := reg.Histogram("karpo.default", "s", ""); def == nil {
		t.Fatal("default buckets")
	}
	if d, _ := reg.Find("karpo.default"); len(d.Buckets) != len(observability.DurationBuckets) {
		t.Fatalf("default buckets: %v", d.Buckets)
	}
}

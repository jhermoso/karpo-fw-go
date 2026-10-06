package vanilla_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics/vanilla"
)

func scrape(t *testing.T, reg *vanilla.Registry) string {
	t.Helper()
	srv := httptest.NewServer(reg.Handler())
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("unexpected content type %q", ct)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestRegistry_ExposesACounterAndAHistogram(t *testing.T) {
	reg := vanilla.NewRegistry()
	ctx := context.Background()

	relayed := reg.Counter(metrics.OutboxRelayed, metrics.UnitNone, "Outbox messages relayed.")
	relayed.Add(ctx, 1, "event_type", "parties.party-registered.v1", "outcome", "ok")
	relayed.Add(ctx, 2, "event_type", "parties.party-registered.v1", "outcome", "ok")
	relayed.Add(ctx, -5, "event_type", "parties.party-registered.v1", "outcome", "ok") // counters never go down

	d := reg.Histogram(metrics.HTTPServerRequestDuration, metrics.UnitSeconds, "Duration of HTTP server requests.", 0.1, 1)
	d.Record(ctx, 0.05, "method", "GET", "route", "/parties/{id}", "status", "200")
	d.Record(ctx, 0.1, "method", "GET", "route", "/parties/{id}", "status", "200") // bounds are inclusive
	d.Record(ctx, 30, "method", "GET", "route", "/parties/{id}", "status", "200")

	reg.Gauge(metrics.OutboxOldestPendingAge, metrics.UnitSeconds, "Age of the oldest pending message.",
		func() float64 { return 12.5 }, "outbox", "integration")

	if v := reg.CounterValue(metrics.OutboxRelayed, "event_type", "parties.party-registered.v1", "outcome", "ok"); v != 3 {
		t.Fatalf("counter = %v, want 3", v)
	}
	if n := reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", "/parties/{id}", "status", "200"); n != 3 {
		t.Fatalf("histogram count = %d, want 3", n)
	}
	if n := reg.HistogramTotal(metrics.HTTPServerRequestDuration); n != 3 {
		t.Fatalf("histogram total = %d, want 3", n)
	}
	if v, ok := reg.GaugeValue(metrics.OutboxOldestPendingAge, "outbox", "integration"); !ok || v != 12.5 {
		t.Fatalf("gauge = %v, %v", v, ok)
	}

	text := scrape(t, reg)
	for _, want := range []string{
		"# TYPE karpo_outbox_relayed_total counter",
		`karpo_outbox_relayed_total{event_type="parties.party-registered.v1",outcome="ok"} 3`,
		"# HELP http_server_request_duration_seconds Duration of HTTP server requests.",
		"# TYPE http_server_request_duration_seconds histogram",
		`http_server_request_duration_seconds_bucket{method="GET",route="/parties/{id}",status="200",le="0.1"} 2`,
		`http_server_request_duration_seconds_bucket{method="GET",route="/parties/{id}",status="200",le="1"} 2`,
		`http_server_request_duration_seconds_bucket{method="GET",route="/parties/{id}",status="200",le="+Inf"} 3`,
		`http_server_request_duration_seconds_sum{method="GET",route="/parties/{id}",status="200"} 30.15`,
		`http_server_request_duration_seconds_count{method="GET",route="/parties/{id}",status="200"} 3`,
		"# TYPE karpo_outbox_oldest_pending_age_seconds gauge",
		`karpo_outbox_oldest_pending_age_seconds{outbox="integration"} 12.5`,
	} {
		if !strings.Contains(text, want+"\n") {
			t.Errorf("the exposition lacks %q\n%s", want, text)
		}
	}
}

func TestRegistry_SameNameSameInstrument(t *testing.T) {
	reg := vanilla.NewRegistry()
	reg.Counter("x.total", "", "").Add(context.Background(), 1)
	reg.Counter("x.total", "", "").Add(context.Background(), 1)
	if v := reg.CounterValue("x.total"); v != 2 {
		t.Fatalf("counter = %v, want 2", v)
	}
	if text := scrape(t, reg); !strings.Contains(text, "x_total_total 2\n") {
		t.Fatalf("unexpected exposition:\n%s", text)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("reusing a name for another kind of instrument must panic")
		}
	}()
	reg.Histogram("x.total", "", "")
}

func TestRegistry_EscapesLabelValues(t *testing.T) {
	reg := vanilla.NewRegistry()
	reg.Counter("c", "", "").Add(context.Background(), 1, "k", "a\"b\\c\nd")
	if text := scrape(t, reg); !strings.Contains(text, `c_total{k="a\"b\\c\nd"} 1`) {
		t.Fatalf("label values must be escaped:\n%s", text)
	}
}

func TestRegistry_ConcurrentUse(t *testing.T) {
	reg := vanilla.NewRegistry()
	c := reg.Counter("hits", "", "")
	h := reg.Histogram("d", metrics.UnitSeconds, "")
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for range 100 {
				c.Add(context.Background(), 1, "k", "v")
				h.Record(context.Background(), 0.01, "k", "v")
				_ = reg.WriteText(io.Discard)
			}
		})
	}
	wg.Wait()
	if v := reg.CounterValue("hits", "k", "v"); v != 5000 {
		t.Fatalf("counter = %v, want 5000", v)
	}
	if n := reg.HistogramCount("d", "k", "v"); n != 5000 {
		t.Fatalf("histogram = %d, want 5000", n)
	}
}

func TestRegisterRuntime(t *testing.T) {
	reg := vanilla.NewRegistry()
	vanilla.RegisterRuntime(reg)
	if v, ok := reg.GaugeValue("go.goroutines"); !ok || v < 1 {
		t.Fatalf("go.goroutines = %v, %v", v, ok)
	}
	text := scrape(t, reg)
	for _, want := range []string{"go_goroutines ", "go_memory_heap_bytes ", "go_gc_cycles "} {
		if !strings.Contains(text, want) {
			t.Errorf("the exposition lacks %q\n%s", want, text)
		}
	}
}

func TestNoop(t *testing.T) {
	m := metrics.OrNoop(nil)
	m.Counter("a", "", "").Add(context.Background(), 1)
	m.Histogram("b", "", "").Record(context.Background(), 1)
	m.Gauge("c", "", "", func() float64 { return 1 })
}

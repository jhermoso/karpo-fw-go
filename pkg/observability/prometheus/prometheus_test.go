package prometheus_test

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/observability"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/prometheus"
)

func TestHandler_TextExposition(t *testing.T) {
	reg := inprocess.NewRegistry()
	ctx := context.Background()
	reg.Histogram(observability.MetricHTTPServerDuration, "s", "Duration of HTTP server requests.", 0.1, 1).
		Record(ctx, 0.2, observability.String(observability.AttrHTTPRoute, `GET /api/x/{id}`), observability.Int(observability.AttrHTTPStatusCode, 200))
	reg.Counter(observability.MetricOutboxDelivered, "1", "").Add(ctx, 3, observability.String(observability.AttrEventType, "a\"b"))

	rec := httptest.NewRecorder()
	prometheus.Handler(reg).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body, _ := io.ReadAll(rec.Body)
	got := string(body)
	for _, want := range []string{
		"# TYPE http_server_request_duration_seconds histogram",
		`http_server_request_duration_seconds_bucket{http_response_status_code="200",http_route="GET /api/x/{id}",le="0.1"} 0`,
		`http_server_request_duration_seconds_bucket{http_response_status_code="200",http_route="GET /api/x/{id}",le="1"} 1`,
		`http_server_request_duration_seconds_bucket{http_response_status_code="200",http_route="GET /api/x/{id}",le="+Inf"} 1`,
		`http_server_request_duration_seconds_count{http_response_status_code="200",http_route="GET /api/x/{id}"} 1`,
		"# TYPE karpo_outbox_delivered_total counter",
		`karpo_outbox_delivered_total{karpo_event_type="a\"b"} 3`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if ct := rec.Header().Get("Content-Type"); ct != prometheus.ContentType {
		t.Fatalf("content type %q", ct)
	}
}

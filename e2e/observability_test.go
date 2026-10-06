// Package e2e holds end-to-end tests that compose real bounded contexts with the framework the
// way a service does. They live outside contexts/ so they never collide with work on a context.
package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	"github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/observability"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/prometheus"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// syncBuffer is the process stdout of the test service.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(b.buf.Bytes()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("stdout is not one JSON object per line: %q", sc.Text())
		}
		out = append(out, m)
	}
	return out
}

type service struct {
	srv    *httptest.Server
	stdout *syncBuffer
	tracer *inprocess.Tracer
	reg    *inprocess.Registry
	sw     *hotswap.Switch
	token  string
}

// compose builds the Geography context behind the standard chain of a service: recovery,
// correlation, telemetry and authentication, plus GET /metrics.
func compose(t *testing.T) *service {
	t.Helper()
	ctx := context.Background()
	s := &service{stdout: &syncBuffer{}, tracer: inprocess.NewTracer(64), reg: inprocess.NewRegistry()}
	logger := vanilla.NewJSON(s.stdout, log.LevelInfo).With(observability.FieldService, "geography")
	tel := observability.Telemetry{Tracer: s.tracer, Meter: s.reg}

	jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("e2e-observability")})
	dir := authorization.NewMemoryDirectory()
	reader := fw.NewUUID()
	dir.Put(reader, authz.Subject{Active: true, Permissions: []authz.Permission{gapp.PermBoundaryRead, gapp.PermReferenceRead}})
	tok, _ := jwt.Issue(jwtauth.Claims{Subject: reader.String(), Username: "reader", PartyID: fw.NewUUID().String(),
		ExpiresAt: fw.Now().Add(time.Hour).Unix()})
	s.token = "Bearer " + tok

	store := memory.NewStore("memory")
	if err := infrastructure.LoadMemory(ctx, store); err != nil {
		t.Fatal(err)
	}
	s.sw = hotswap.New(store)
	mod := geography.Compose(s.sw)

	api := http.NewServeMux()
	mod.HTTP.RegisterRoutes(api)
	root := http.NewServeMux()
	root.Handle("GET /metrics", prometheus.Handler(s.reg))
	root.Handle("/", distribution.Chain(api,
		distribution.Recovery(logger),
		distribution.Correlation(),
		distribution.Observe(logger, tel),
		distribution.Authorize(jwt, authorization.NewResolver(dir, authorization.Options{})),
	))
	s.srv = httptest.NewServer(root)
	t.Cleanup(s.srv.Close)
	t.Cleanup(func() { _ = s.sw.Close(ctx) })
	return s
}

func (s *service) get(t *testing.T, path, correlation string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, s.srv.URL+path, nil)
	req.Header.Set("Authorization", s.token)
	req.Header.Set(distribution.CorrelationHeader, correlation)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(body)
}

func requestLines(t *testing.T, s *service, correlation string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range s.stdout.lines(t) {
		if l["msg"] == "http request" && l[observability.FieldCorrelationID] == correlation {
			out = append(out, l)
		}
	}
	return out
}

// TestObservability_RequestLeavesLineMetricAndTraceWithOneCorrelation is the "done when" of the
// minimal observability (docs/OBSERVABILIDAD.md): an HTTP request to an existing context leaves a
// JSON line, a metric and a trace id sharing its correlation, and a 500 leaves its cause.
func TestObservability_RequestLeavesLineMetricAndTraceWithOneCorrelation(t *testing.T) {
	s := compose(t)

	// 1. A normal request.
	res, body := s.get(t, "/api/reference/countries/ES", "e2e-ok-1")
	if res.StatusCode != http.StatusOK || res.Header.Get(distribution.CorrelationHeader) != "e2e-ok-1" {
		t.Fatalf("GET country: %d %s", res.StatusCode, body)
	}
	lines := requestLines(t, s, "e2e-ok-1")
	if len(lines) != 1 {
		t.Fatalf("want exactly one request line with the correlation, got %v", s.stdout.lines(t))
	}
	line := lines[0]
	if line["level"] != "INFO" || line[observability.FieldService] != "geography" ||
		line[observability.AttrHTTPRoute] != "/api/reference/countries/{alpha2}" || line[observability.AttrHTTPStatusCode] != float64(200) {
		t.Fatalf("request line: %v", line)
	}
	traceID, _ := line[observability.FieldTraceID].(string)
	if len(traceID) != 32 {
		t.Fatalf("no trace id in the line: %v", line)
	}
	var server *inprocess.FinishedSpan
	for _, sp := range s.tracer.Finished() {
		if sp.Kind == observability.SpanKindServer && sp.Context.TraceID.String() == traceID {
			server = &sp
		}
	}
	if server == nil || server.Attr(observability.FieldCorrelationID) != "e2e-ok-1" || server.Name != "GET /api/reference/countries/{alpha2}" {
		t.Fatalf("no server span with the line's trace id and correlation: %+v", s.tracer.Finished())
	}

	// 2. A 500: the backend is swapped to a database without schema.
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if err := s.sw.Swap(context.Background(), sqlite.Open(raw, sqlrepo.WithName("sqlite"))); err != nil {
		t.Fatal(err)
	}
	res, body = s.get(t, "/api/reference/countries/ES", "e2e-500-1")
	if res.StatusCode != http.StatusInternalServerError || strings.Contains(body, "no such table") {
		t.Fatalf("expected an opaque 500, got %d %s", res.StatusCode, body)
	}
	lines = requestLines(t, s, "e2e-500-1")
	if len(lines) != 1 {
		t.Fatalf("want one request line for the 500, got %v", s.stdout.lines(t))
	}
	failed := lines[0]
	cause, _ := failed[observability.FieldError].(string)
	if failed["level"] != "ERROR" || !strings.Contains(cause, "no such table") || failed[observability.FieldErrorType] == nil ||
		failed[observability.FieldTraceID] == nil || failed[observability.AttrHTTPStatusCode] != float64(500) {
		t.Fatalf("the 500 line does not carry its cause: %v", failed)
	}

	for _, l := range s.stdout.lines(t) {
		t.Logf("%v", l)
	}

	// 3. The metric, by route and status, also exposed on /metrics.
	m, _ := s.reg.Find(observability.MetricHTTPServerDuration)
	counts := map[string]uint64{}
	for _, series := range m.Series {
		if series.Attr(observability.AttrHTTPRoute) == "/api/reference/countries/{alpha2}" {
			counts[series.Attr(observability.AttrHTTPStatusCode)] += series.Count
		}
	}
	if counts["200"] != 1 || counts["500"] != 1 {
		t.Fatalf("metric by route and status: %+v", m.Series)
	}
	mres, err := http.Get(s.srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	exposition, _ := io.ReadAll(mres.Body)
	mres.Body.Close()
	if !strings.Contains(string(exposition), `http_server_request_duration_seconds_count{http_request_method="GET",http_response_status_code="500",http_route="/api/reference/countries/{alpha2}"} 1`) {
		t.Fatalf("/metrics:\n%s", exposition)
	}
}

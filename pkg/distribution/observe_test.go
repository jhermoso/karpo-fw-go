package distribution_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/observability"
	"github.com/jhermoso/karpo-fw-go/pkg/observability/inprocess"
)

type harness struct {
	buf    bytes.Buffer
	tracer *inprocess.Tracer
	reg    *inprocess.Registry
	h      http.Handler
}

func newHarness(mux http.Handler) *harness {
	hs := &harness{tracer: inprocess.NewTracer(16), reg: inprocess.NewRegistry()}
	logger := vanilla.NewJSON(&hs.buf, log.LevelInfo)
	tel := observability.Telemetry{Tracer: hs.tracer, Meter: hs.reg}
	hs.h = distribution.Chain(mux, distribution.Recovery(nil), distribution.Correlation(), distribution.Observe(logger, tel))
	return hs
}

func (hs *harness) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	sc := bufio.NewScanner(&hs.buf)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("not one JSON object per line: %q", sc.Text())
		}
		out = append(out, m)
	}
	return out
}

func TestValidCorrelationID(t *testing.T) {
	for _, ok := range []string{"corr-1", "0192f3a4-7b1c-7def-8000-0123456789ab", "a.b_c:d", strings.Repeat("x", distribution.MaxCorrelationIDLength)} {
		if !distribution.ValidCorrelationID(ok) {
			t.Errorf("rejected %q", ok)
		}
	}
	for _, bad := range []string{"", "with space", "line\nbreak", `quo"te`, "ñandú", "a/b", strings.Repeat("x", distribution.MaxCorrelationIDLength+1)} {
		if distribution.ValidCorrelationID(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestCorrelation_InvalidClientValueIsReplacedNotEchoed(t *testing.T) {
	var seen string
	h := distribution.Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = application.CorrelationID(r.Context())
	}), distribution.Correlation())
	evil := "x\" level=ERROR msg=forged " + strings.Repeat("A", 500)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(distribution.CorrelationHeader, evil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seen == evil || !distribution.ValidCorrelationID(seen) || rec.Header().Get(distribution.CorrelationHeader) != seen {
		t.Fatalf("invalid correlation kept: ctx=%q header=%q", seen, rec.Header().Get(distribution.CorrelationHeader))
	}
	if _, err := domain.ParseUUID(seen); err != nil {
		t.Fatalf("replacement is not a fresh UUID: %q", seen)
	}
}

func TestObserve_OneLineSpanAndMetricPerRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/things/{id}", func(w http.ResponseWriter, r *http.Request) {
		distribution.Respond(w, r, map[string]string{"id": r.PathValue("id")}, nil, 0)
	})
	hs := newHarness(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/things/42?email=ana@example.com", nil)
	req.Header.Set(distribution.CorrelationHeader, "corr-ok")
	req.Header.Set(observability.TraceparentHeader, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	hs.h.ServeHTTP(rec, req)

	lines := hs.lines(t)
	if len(lines) != 1 {
		t.Fatalf("want one line, got %d", len(lines))
	}
	l := lines[0]
	if l["msg"] != "http request" || l["level"] != "INFO" || l[observability.AttrHTTPRoute] != "/api/things/{id}" ||
		l[observability.AttrHTTPMethod] != "GET" || l[observability.AttrHTTPStatusCode] != float64(200) ||
		l[observability.FieldCorrelationID] != "corr-ok" || l[observability.FieldTraceID] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("request line: %v", l)
	}
	if _, ok := l[observability.FieldDurationMs].(float64); !ok {
		t.Fatalf("duration_ms: %v", l)
	}
	raw := hs.buf.String()
	if strings.Contains(raw, "ana@example.com") || strings.Contains(raw, "/api/things/42") {
		t.Fatalf("raw path or query in the log: %s", raw)
	}

	spans := hs.tracer.Finished()
	if len(spans) != 1 || spans[0].Kind != observability.SpanKindServer || spans[0].Parent.SpanID.String() != "00f067aa0ba902b7" ||
		spans[0].Context.SpanID.String() != l[observability.FieldSpanID] || spans[0].Attr(observability.AttrHTTPRoute) != "/api/things/{id}" {
		t.Fatalf("span: %+v", spans)
	}

	m, ok := hs.reg.Find(observability.MetricHTTPServerDuration)
	if !ok || len(m.Series) != 1 || m.Series[0].Count != 1 || m.Series[0].Attr(observability.AttrHTTPRoute) != "/api/things/{id}" ||
		m.Series[0].Attr(observability.AttrHTTPStatusCode) != "200" {
		t.Fatalf("metric: %+v", m)
	}
}

func TestObserve_5xxLeavesItsCause(t *testing.T) {
	cause := fmt.Errorf("loading party: %w", errors.New("connection refused"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) { distribution.WriteError(w, r, cause) })
	mux.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) { panic("nil map") })
	mux.HandleFunc("GET /manual", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	mux.HandleFunc("GET /notfound", func(w http.ResponseWriter, r *http.Request) { distribution.WriteError(w, r, domain.ErrNotFound) })
	hs := newHarness(mux)

	for _, p := range []string{"/fail", "/panic", "/manual", "/notfound"} {
		rec := httptest.NewRecorder()
		hs.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		body, _ := io.ReadAll(rec.Body)
		if strings.Contains(string(body), "connection refused") {
			t.Fatalf("cause leaked to the client: %s", body)
		}
	}
	lines := hs.lines(t)
	if len(lines) != 4 {
		t.Fatalf("lines: %v", lines)
	}
	fail, pnc, manual, nf := lines[0], lines[1], lines[2], lines[3]
	if fail["level"] != "ERROR" || fail[observability.FieldError] != "loading party: connection refused" ||
		fail[observability.FieldErrorType] != "*errors.errorString" || fail[observability.FieldCorrelationID] == nil || fail[observability.AttrHTTPRoute] != "/fail" {
		t.Fatalf("5xx line: %v", fail)
	}
	if pnc["level"] != "ERROR" || pnc[observability.AttrHTTPStatusCode] != float64(500) || pnc[observability.FieldError] != "panic: nil map" {
		t.Fatalf("panic line: %v", pnc)
	}
	if manual[observability.AttrHTTPStatusCode] != float64(502) || manual[observability.FieldErrorType] != "502" || manual[observability.FieldError] == nil {
		t.Fatalf("manual 5xx line: %v", manual)
	}
	if nf["level"] != "INFO" || nf[observability.FieldError] != nil || nf[observability.AttrHTTPStatusCode] != float64(404) {
		t.Fatalf("4xx line: %v", nf)
	}
	spans := hs.tracer.Finished()
	if spans[0].Status != observability.StatusError || len(spans[0].Errors) != 1 || spans[3].Status == observability.StatusError {
		t.Fatalf("span status: %+v", spans)
	}
}

func TestObserve_PanicOutsideRecoveryStillLogged(t *testing.T) {
	var buf bytes.Buffer
	h := distribution.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(errors.New("boom")) }),
		distribution.Recovery(nil), distribution.Observe(vanilla.NewJSON(&buf, log.LevelInfo), observability.Telemetry{}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != 500 || !strings.Contains(buf.String(), `"error":"panic: boom"`) || !strings.Contains(buf.String(), `"http.route":"unknown"`) {
		t.Fatalf("code %d line %s", rec.Code, buf.String())
	}
}

func TestObserve_DoesNotBreakStreaming(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "data: %d\n\n", i)
			f.Flush()
		}
		if err := http.NewResponseController(w).SetWriteDeadline(timeZero); err != nil {
			t.Errorf("ResponseController cannot reach the writer: %v", err)
		}
	})
	hs := newHarness(mux)
	srv := httptest.NewServer(distribution.Chain(hs.h, distribution.RequestLogging(nil)))
	defer srv.Close()
	res, err := http.Get(srv.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || strings.Count(string(body), "data:") != 3 {
		t.Fatalf("stream broken: %d %q", res.StatusCode, body)
	}
}

var timeZero = time.Time{}

package distribution_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	logvanilla "github.com/jhermoso/karpo-fw-go/pkg/log/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	metricsvanilla "github.com/jhermoso/karpo-fw-go/pkg/metrics/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
	tracevanilla "github.com/jhermoso/karpo-fw-go/pkg/trace/vanilla"
)

// observed is a handler behind the standard chain, with everything it leaves behind.
type observed struct {
	handler http.Handler
	logs    *bytes.Buffer
	spans   *tracevanilla.Recorder
	reg     *metricsvanilla.Registry
}

func observe(mux *http.ServeMux, inner ...distribution.Middleware) observed {
	o := observed{logs: &bytes.Buffer{}, spans: tracevanilla.NewRecorder(0), reg: metricsvanilla.NewRegistry()}
	chain := append([]distribution.Middleware{
		distribution.Correlation(),
		distribution.Observe(logvanilla.NewJSON(o.logs, log.LevelInfo), tracevanilla.New(tracevanilla.WithExporter(o.spans)), o.reg),
	}, inner...)
	o.handler = distribution.Chain(mux, chain...)
	return o
}

func (o observed) lines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(o.logs.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("not a JSON line: %v\n%s", err, l)
		}
		out = append(out, m)
	}
	return out
}

func (o observed) do(method, target string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	o.handler.ServeHTTP(rec, req)
	return rec
}

// passthrough is a middleware that copies the request, as Authorize and TenantActorContext do.
func passthrough(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), struct{ k string }{"x"}, 1)))
	})
}

func TestObserve_OneLineOneSpanOneObservation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /parties/{id}", func(w http.ResponseWriter, r *http.Request) {
		distribution.Respond(w, r, map[string]string{"id": r.PathValue("id")}, nil, 0)
	})
	o := observe(mux, passthrough)

	res := o.do(http.MethodGet, "/parties/0192aaaa-0000-7000-8000-000000000001?name=Ada", "X-Correlation-ID", "flow-1")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}

	lines := o.lines(t)
	if len(lines) != 1 {
		t.Fatalf("expected one line per request, got %d: %s", len(lines), o.logs.String())
	}
	l := lines[0]
	if l["msg"] != "http request" || l["level"] != "INFO" || l["method"] != "GET" || l["status"] != float64(200) {
		t.Fatalf("unexpected line: %v", l)
	}
	if l["route"] != "/parties/{id}" {
		t.Fatalf("the route must be the pattern, got %v", l["route"])
	}
	if l["correlation_id"] != "flow-1" {
		t.Fatalf("the line must carry the correlation: %v", l)
	}
	if _, ok := l["duration_ms"].(float64); !ok {
		t.Fatalf("the line must carry the duration: %v", l)
	}
	if s := o.logs.String(); strings.Contains(s, "0192aaaa") || strings.Contains(s, "Ada") {
		t.Fatalf("neither the path nor the query string may be logged: %s", s)
	}
	if n := strings.Count(o.logs.String(), `"correlation_id"`); n != 1 {
		t.Fatalf("correlation_id must appear once, got %d", n)
	}

	spans := o.spans.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected one server span, got %d", len(spans))
	}
	s := spans[0]
	if s.Kind != trace.KindServer || s.Name != "GET /parties/{id}" || s.HasParent() {
		t.Fatalf("unexpected span: %+v", s)
	}
	if l["trace_id"] != s.Context.TraceIDString() || l["span_id"] != s.Context.SpanIDString() {
		t.Fatalf("the line and the span must share the trace: %v / %s", l, s.Context.TraceIDString())
	}
	if s.Attr("correlation_id") != "flow-1" || s.Attr("status") != 200 || s.Err != nil {
		t.Fatalf("unexpected span attributes: %v", s.Attributes)
	}

	if n := o.reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", "/parties/{id}", "status", "200"); n != 1 {
		t.Fatalf("expected one observation, got %d (series: %v)", n, o.reg.SeriesLabels(metrics.HTTPServerRequestDuration))
	}
	if n := o.reg.HistogramTotal(metrics.HTTPServerRequestDuration); n != 1 {
		t.Fatalf("expected one observation in total, got %d", n)
	}
}

func TestObserve_ContinuesTheIncomingTrace(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		distribution.Respond(w, r, "pong", nil, 0)
	})
	o := observe(mux)

	o.do(http.MethodGet, "/ping", "traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	o.do(http.MethodGet, "/ping", "traceparent", "garbage")

	spans := o.spans.Spans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	parent := trace.SpanContext{SpanID: spans[0].ParentID}
	if spans[0].Context.TraceIDString() != "4bf92f3577b34da6a3ce929d0e0e4736" || parent.SpanIDString() != "00f067aa0ba902b7" {
		t.Fatalf("the span must continue the incoming trace: %+v", spans[0])
	}
	if spans[1].HasParent() || spans[1].Context.TraceID == spans[0].Context.TraceID {
		t.Fatalf("an invalid traceparent must start a new trace: %+v", spans[1])
	}
	if l := o.lines(t)[0]; l["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("the line must carry the incoming trace id: %v", l)
	}
}

func TestObserve_A500LeavesItsCause(t *testing.T) {
	cause := fmt.Errorf("loading party: %w", errors.New("sqlrepo: connection refused"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /parties/{id}", func(w http.ResponseWriter, r *http.Request) {
		distribution.WriteError(w, r, cause)
	})
	mux.HandleFunc("GET /missing/{id}", func(w http.ResponseWriter, r *http.Request) {
		distribution.WriteError(w, r, fmt.Errorf("party %w", domain.ErrNotFound))
	})
	mux.HandleFunc("GET /bare", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	o := observe(mux, passthrough)

	res := o.do(http.MethodGet, "/parties/42", "X-Correlation-ID", "flow-500")
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", res.Code)
	}
	if body := res.Body.String(); strings.Contains(body, "connection refused") {
		t.Fatalf("the caller must not see the cause: %s", body)
	}
	var problem distribution.ProblemDetails
	if err := json.Unmarshal(res.Body.Bytes(), &problem); err != nil || problem.CorrelationID != "flow-500" {
		t.Fatalf("the problem must return the correlation id: %+v (%v)", problem, err)
	}

	o.do(http.MethodGet, "/missing/42")
	o.do(http.MethodGet, "/bare")

	lines := o.lines(t)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	l := lines[0]
	if l["level"] != "ERROR" || l["status"] != float64(500) || l["correlation_id"] != "flow-500" || l["route"] != "/parties/{id}" {
		t.Fatalf("unexpected 500 line: %v", l)
	}
	if l["error"] != cause.Error() || l["error_type"] != "*errors.errorString" {
		t.Fatalf("the 500 line must carry its cause: %v", l)
	}
	if l := lines[1]; l["level"] != "INFO" || l["status"] != float64(404) || l["error"] != nil {
		t.Fatalf("a 4xx is an Info line without a cause: %v", l)
	}
	if l := lines[2]; l["level"] != "ERROR" || l["status"] != float64(502) || !strings.Contains(fmt.Sprint(l["error"]), "without a recorded error") {
		t.Fatalf("a 5xx written by hand must still be an Error line: %v", l)
	}

	spans := o.spans.Spans()
	if spans[0].Err == nil || spans[1].Err != nil {
		t.Fatalf("only the 5xx span is failed: %v / %v", spans[0].Err, spans[1].Err)
	}
	if n := o.reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", "/parties/{id}", "status", "500"); n != 1 {
		t.Fatalf("expected one 500 observation, got %d", n)
	}
}

func TestObserve_PanicsLeaveTheirCause(t *testing.T) {
	for _, order := range []string{"recovery outside", "recovery inside"} {
		t.Run(order, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
			var logs bytes.Buffer
			reg := metricsvanilla.NewRegistry()
			obs := distribution.Observe(logvanilla.NewJSON(&logs, log.LevelInfo), nil, reg)
			chain := []distribution.Middleware{distribution.Recovery(nil), distribution.Correlation(), obs}
			if order == "recovery inside" {
				chain = []distribution.Middleware{distribution.Correlation(), obs, distribution.Recovery(nil)}
			}
			rec := httptest.NewRecorder()
			distribution.Chain(mux, chain...).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d", rec.Code)
			}
			var l map[string]any
			if err := json.Unmarshal(logs.Bytes(), &l); err != nil {
				t.Fatalf("expected one JSON line: %v\n%s", err, logs.String())
			}
			if l["level"] != "ERROR" || l["status"] != float64(500) || !strings.Contains(fmt.Sprint(l["error"]), "kaboom") {
				t.Fatalf("the panic must leave its cause: %v", l)
			}
			if l["correlation_id"] != rec.Header().Get(distribution.CorrelationHeader) || l["correlation_id"] == "" {
				t.Fatalf("the line must carry the correlation returned to the caller: %v", l)
			}
			if n := reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", "/boom", "status", "500"); n != 1 {
				t.Fatalf("the panic must be measured as a 500: %v", reg.SeriesLabels(metrics.HTTPServerRequestDuration))
			}
			// The body carries the correlation too, wherever Recovery sits in the chain.
			var problem distribution.ProblemDetails
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil || problem.CorrelationID == "" ||
				problem.CorrelationID != rec.Header().Get(distribution.CorrelationHeader) {
				t.Fatalf("the 500 must return its correlation id: %s", rec.Body.String())
			}
		})
	}
}

func TestRecovery_LogsWithTheCorrelationAndLetsAbortsThrough(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("kaboom") })
	mux.HandleFunc("GET /abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	var logs bytes.Buffer
	h := distribution.Chain(mux, distribution.Recovery(logvanilla.NewJSON(&logs, log.LevelInfo)), distribution.Correlation())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	var l map[string]any
	if err := json.Unmarshal(logs.Bytes(), &l); err != nil {
		t.Fatalf("expected one JSON line: %v\n%s", err, logs.String())
	}
	if l["correlation_id"] == nil || l["correlation_id"] != rec.Header().Get(distribution.CorrelationHeader) {
		t.Fatalf("the recovery line must carry the correlation: %v", l)
	}

	defer func() {
		if p := recover(); p != http.ErrAbortHandler { //nolint:errorlint // identity, as net/http
			t.Fatalf("http.ErrAbortHandler must go through Recovery, got %v", p)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/abort", nil))
}

func TestObserve_RouteNeverFallsBackToThePath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /plain/{id}", func(w http.ResponseWriter, _ *http.Request) {
		distribution.WriteJSON(w, http.StatusOK, "no request at hand")
	})

	// Behind a middleware that copies the request, a handler that never tells its pattern is
	// "unmatched", like a 404 of the mux; the path is never used.
	o := observe(mux, passthrough)
	o.do(http.MethodGet, "/plain/7")
	o.do(http.MethodGet, "/nowhere/123")
	o.do("BREW", "/nowhere/123")
	for _, labels := range o.reg.SeriesLabels(metrics.HTTPServerRequestDuration) {
		if labels[3] != distribution.RouteUnmatched {
			t.Fatalf("unexpected route label: %v", labels)
		}
	}
	if n := o.reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", distribution.MethodOther, "route", "unmatched", "status", "404"); n != 1 {
		t.Fatalf("unknown methods share one label: %v", o.reg.SeriesLabels(metrics.HTTPServerRequestDuration))
	}

	// With the router at hand, every matched request gets its pattern.
	reg := metricsvanilla.NewRegistry()
	h := distribution.Chain(mux, distribution.Observe(nil, nil, reg, distribution.ObserveRoutes(mux)), passthrough)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/plain/7", nil))
	if n := reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", "/plain/{id}", "status", "200"); n != 1 {
		t.Fatalf("ObserveRoutes must resolve the pattern: %v", reg.SeriesLabels(metrics.HTTPServerRequestDuration))
	}

	// Wrapping the mux directly, the mux itself tells the pattern.
	reg = metricsvanilla.NewRegistry()
	h = distribution.Chain(mux, distribution.Observe(nil, nil, reg))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/plain/7", nil))
	if n := reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", "/plain/{id}", "status", "200"); n != 1 {
		t.Fatalf("the pattern set by the mux must be used: %v", reg.SeriesLabels(metrics.HTTPServerRequestDuration))
	}
}

func TestObserve_QuietPathsAreMeasuredButNotLogged(t *testing.T) {
	fail := false
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if fail {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	var logs bytes.Buffer
	reg := metricsvanilla.NewRegistry()
	h := distribution.Chain(mux, distribution.Observe(logvanilla.NewJSON(&logs, log.LevelInfo), nil, reg, distribution.ObserveQuietly("/healthz")))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if logs.Len() != 0 {
		t.Fatalf("a quiet path must not be logged when it succeeds: %s", logs.String())
	}
	fail = true
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !strings.Contains(logs.String(), `"status":503`) {
		t.Fatalf("a failing quiet path must be logged: %s", logs.String())
	}
	if n := reg.HistogramTotal(metrics.HTTPServerRequestDuration); n != 2 {
		t.Fatalf("quiet paths are still measured, got %d", n)
	}
}

func TestObserve_WithoutDestinationsDoesNothing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, r *http.Request) {
		if trace.FromContext(r.Context()).Context().IsValid() {
			t.Error("without a tracer there is no span")
		}
		distribution.Respond(w, r, "pong", nil, 0)
	})
	rec := httptest.NewRecorder()
	distribution.Chain(mux, distribution.Observe(nil, nil, nil)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `"pong"` {
		t.Fatalf("unexpected response: %d %s", rec.Code, rec.Body.String())
	}
}

// A streamed response (server-sent events) must cross Observe and RequestLogging: each event
// reaches the client when it is flushed, not when the handler returns.
func TestObserve_StreamedResponsesCrossTheMiddleware(t *testing.T) {
	for name, mw := range map[string]distribution.Middleware{
		"Observe":        distribution.Observe(logvanilla.NewJSON(&bytes.Buffer{}, log.LevelInfo), tracevanilla.New(), metricsvanilla.NewRegistry()),
		"RequestLogging": distribution.RequestLogging(logvanilla.NewJSON(&bytes.Buffer{}, log.LevelInfo)),
	} {
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			mux := http.NewServeMux()
			mux.HandleFunc("GET /events", func(w http.ResponseWriter, _ *http.Request) {
				flusher, ok := w.(http.Flusher)
				if !ok {
					t.Error("the wrapped writer must still be an http.Flusher")
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: first\n\n")
				flusher.Flush()
				<-release
				fmt.Fprint(w, "data: second\n\n")
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Errorf("ResponseController must reach the real writer: %v", err)
				}
			})
			srv := httptest.NewServer(distribution.Chain(mux, mw))
			defer srv.Close()

			res, err := http.Get(srv.URL + "/events")
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()

			first := make(chan string, 1)
			reader := bufio.NewReader(res.Body)
			go func() {
				line, _ := reader.ReadString('\n')
				first <- line
			}()
			select {
			case line := <-first:
				if line != "data: first\n" {
					t.Fatalf("unexpected first event %q", line)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the first event did not arrive before the handler returned")
			}
			close(release)
		})
	}
}

func TestCorrelation_ClientValueIsValidated(t *testing.T) {
	var seen string
	h := distribution.Chain(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = application.CorrelationID(r.Context())
	}), distribution.Correlation())

	cases := []struct {
		name, header string
		kept         bool
	}{
		{"uuid", "0192aaaa-0000-7000-8000-000000000001", true},
		{"trace id", "4bf92f3577b34da6a3ce929d0e0e4736", true},
		{"punctuation", "gw:req_1.2-3", true},
		{"64 characters", strings.Repeat("a", 64), true},
		{"65 characters", strings.Repeat("a", 65), false},
		{"200 characters", strings.Repeat("a", 200), false},
		{"spaces", "a b", false},
		{"quote", `a"b`, false},
		{"line break", "a\nb", false},
		{"non ASCII", "añ", false},
		{"e-mail", "ada@example.com", false},
		{"absent", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if c.header != "" {
				req.Header.Set(distribution.CorrelationHeader, c.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			echoed := rec.Header().Get(distribution.CorrelationHeader)
			if seen != echoed || !distribution.ValidCorrelationID(seen) {
				t.Fatalf("context %q and response %q must hold the same valid id", seen, echoed)
			}
			if c.kept != (seen == c.header) {
				t.Fatalf("header %q: kept = %v, context = %q", c.header, seen == c.header, seen)
			}
		})
	}
}

func TestObserve_InformationalStatusIsNotTheFinalOne(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /hints", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", "</app.css>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		distribution.WriteError(w, r, errors.New("backend down"))
	})
	o := observe(mux)
	o.do(http.MethodGet, "/hints")

	if n := o.reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", "/hints", "status", "500"); n != 1 {
		t.Fatalf("the final status is the 500, not the 103: %v", o.reg.SeriesLabels(metrics.HTTPServerRequestDuration))
	}
	lines := o.lines(t)
	if len(lines) != 1 || lines[0]["level"] != "ERROR" || lines[0]["error"] != "backend down" {
		t.Fatalf("the 500 after early hints must be logged as an error: %v", lines)
	}
	if spans := o.spans.Spans(); len(spans) != 1 || spans[0].Err == nil {
		t.Fatalf("the span must fail: %+v", spans)
	}
}

func TestObserve_AbortedHandlersAreMeasuredAndLogged(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /proxy", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	o := observe(mux)
	func() {
		defer func() {
			if p := recover(); p != http.ErrAbortHandler { //nolint:errorlint // identity, as net/http
				t.Fatalf("the abort must keep propagating, got %v", p)
			}
		}()
		o.do(http.MethodGet, "/proxy")
	}()

	status := strconv.Itoa(distribution.StatusAborted)
	if n := o.reg.HistogramCount(metrics.HTTPServerRequestDuration, "method", "GET", "route", "/proxy", "status", status); n != 1 {
		t.Fatalf("an aborted request must be measured: %v", o.reg.SeriesLabels(metrics.HTTPServerRequestDuration))
	}
	lines := o.lines(t)
	if len(lines) != 1 || lines[0]["level"] != "WARN" || lines[0]["error_type"] != distribution.ErrorTypeAborted ||
		lines[0]["route"] != "/proxy" || lines[0]["correlation_id"] == nil {
		t.Fatalf("an aborted request must leave its line: %v", lines)
	}
	if spans := o.spans.Spans(); len(spans) != 1 || spans[0].Err == nil || spans[0].Name != "GET /proxy" {
		t.Fatalf("the span must carry route and error: %+v", spans)
	}
}

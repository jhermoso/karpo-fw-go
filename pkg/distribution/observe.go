package distribution

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

// Fields of the request line written by Observe. C# (Paranoia.Karpo.Fw) writes the same names.
const (
	RequestLogMessage = "http request"
	FieldMethod       = "method"
	FieldRoute        = "route"
	FieldStatus       = "status"
	FieldDurationMS   = "duration_ms"
	FieldCorrelation  = "correlation_id"
	FieldActor        = "actor"
	FieldError        = "error"
	FieldErrorType    = "error_type"

	// RouteUnmatched is the route of requests that matched no pattern (404 of the mux, or a
	// handler that never told its pattern): the raw path is never used, it would create one
	// metric series per identifier.
	RouteUnmatched = "unmatched"
	// MethodOther replaces methods outside the standard set, which a client can invent.
	MethodOther = "_OTHER"
)

// Router resolves the pattern a request matches. *http.ServeMux implements it.
type Router interface {
	Handler(r *http.Request) (h http.Handler, pattern string)
}

type observeConfig struct {
	router Router
	quiet  map[string]bool
}

// ObserveOption configures Observe.
type ObserveOption func(*observeConfig)

// ObserveRoutes resolves the route pattern by asking router (the mux the routes are registered
// on) before the request is served. Without it the route is the pattern told by the response
// helpers (WriteError, Respond) or the one the mux set when Observe wraps it directly; handlers
// that answer with WriteJSON behind other middleware need this option to get a route.
func ObserveRoutes(router Router) ObserveOption {
	return func(c *observeConfig) { c.router = router }
}

// ObserveQuietly skips the log line of successful requests to these exact paths (e.g. /healthz
// and /readyz, polled every few seconds). They are still measured, and failures are still logged.
func ObserveQuietly(paths ...string) ObserveOption {
	return func(c *observeConfig) {
		for _, p := range paths {
			c.quiet[p] = true
		}
	}
}

// requestRecord is what the inner layers tell Observe about the request. The ServeMux sets the
// pattern on the request it receives and every middleware in between works on a copy
// (r.WithContext), so the outer middleware cannot read it from its own request.
type requestRecord struct {
	mu    sync.Mutex
	route string
	actor string
	err   error
}

type recordKey struct{}

func recordOf(ctx context.Context) *requestRecord {
	rec, _ := ctx.Value(recordKey{}).(*requestRecord)
	return rec
}

// noteRoute tells Observe the pattern the mux matched for r.
func noteRoute(r *http.Request) {
	if r == nil || r.Pattern == "" {
		return
	}
	if rec := recordOf(r.Context()); rec != nil {
		rec.mu.Lock()
		rec.route = r.Pattern
		rec.mu.Unlock()
	}
}

// RecordError tells Observe the error behind the response of r, so a 5xx leaves its cause in
// the log and in the span. WriteError, Respond and Recovery call it; handlers that write a 5xx
// by other means should call it too. Without Observe in the chain it does nothing.
func RecordError(r *http.Request, err error) {
	if r == nil || err == nil {
		return
	}
	noteRoute(r)
	if rec := recordOf(r.Context()); rec != nil {
		rec.mu.Lock()
		rec.err = err
		rec.mu.Unlock()
	}
}

// noteActor tells Observe who the caller is (identifier only).
func noteActor(ctx context.Context) {
	rec := recordOf(ctx)
	if rec == nil {
		return
	}
	if a := application.ActorFrom(ctx); !a.IsSystem() {
		rec.mu.Lock()
		rec.actor = a.PartyID.String()
		rec.mu.Unlock()
	}
}

// Observe is the telemetry of the HTTP entry point. For every request it leaves:
//
//   - a server span, child of the incoming W3C traceparent when there is a valid one;
//   - one observation of http.server.request.duration (seconds) by method, route and status;
//   - one log line with method, route, status, duration_ms, correlation_id, trace_id, span_id
//     and the actor id: Info, or Error with the cause when the status is 5xx.
//
// The route is always the pattern ("/parties/{id}"), never the path, and nothing personal is
// written: no query string, no headers, no bodies, no actor name.
//
// Mount it right after Correlation, outside everything else:
//
//	distribution.Chain(mux, distribution.Correlation(), distribution.Observe(logger, tracer, meter), ...)
//
// Any of logger, tracer and meter may be nil: that part is skipped.
func Observe(logger log.Logger, tracer trace.Tracer, meter metrics.Meter, opts ...ObserveOption) Middleware {
	cfg := observeConfig{quiet: map[string]bool{}}
	for _, opt := range opts {
		opt(&cfg)
	}
	tracer = trace.OrNoop(tracer)
	duration := metrics.OrNoop(meter).Histogram(metrics.HTTPServerRequestDuration, metrics.UnitSeconds,
		"Duration of HTTP server requests.")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &requestRecord{}
			if cfg.router != nil {
				if _, pattern := cfg.router.Handler(r); pattern != "" {
					rec.route = pattern
				}
			}
			ctx := context.WithValue(r.Context(), recordKey{}, rec)

			startOpts := []trace.StartOption{trace.WithKind(trace.KindServer)}
			if parent, ok := trace.ParseTraceParent(r.Header.Get(trace.TraceParentHeader)); ok {
				startOpts = append(startOpts, trace.WithRemoteParent(parent))
			}
			method := knownMethod(r.Method)
			ctx, span := tracer.Start(ctx, method, startOpts...)
			noteActor(ctx)

			rw := &responseWriter{ResponseWriter: w}
			inner := r.WithContext(ctx)

			finish := func(panicked any) {
				elapsed := time.Since(start)
				status := rw.status
				rec.mu.Lock()
				route, actor, cause := rec.route, rec.actor, rec.err
				rec.mu.Unlock()
				if panicked != nil {
					// Recovery (outside) will answer 500 unless the response already started.
					if status == 0 {
						status = http.StatusInternalServerError
					}
					if err, ok := panicked.(error); ok {
						cause = err
					} else {
						cause = fmt.Errorf("panic: %v", panicked)
					}
				}
				if status == 0 {
					status = http.StatusOK
				}
				if route == "" {
					route = firstNonEmpty(inner.Pattern, r.Pattern)
				}
				route = routeTemplate(route)
				if status >= 500 && cause == nil {
					cause = fmt.Errorf("status %d written without a recorded error", status)
				}
				correlation := application.CorrelationID(ctx)

				duration.Record(ctx, elapsed.Seconds(),
					metrics.LabelMethod, method, metrics.LabelRoute, route, metrics.LabelStatus, strconv.Itoa(status))

				span.SetName(method + " " + route)
				span.SetAttributes(FieldMethod, method, FieldRoute, route, FieldStatus, status)
				if correlation != "" {
					span.SetAttributes(FieldCorrelation, correlation)
				}
				if actor != "" {
					span.SetAttributes(FieldActor, actor)
				}
				if status >= 500 {
					span.RecordError(cause)
				}
				span.End()

				if logger == nil || (status < 500 && cfg.quiet[r.URL.Path]) {
					return
				}
				args := []any{
					FieldMethod, method,
					FieldRoute, route,
					FieldStatus, status,
					FieldDurationMS, float64(elapsed.Microseconds()) / 1000,
				}
				if correlation != "" {
					args = append(args, FieldCorrelation, correlation)
				}
				if sc := span.Context(); sc.IsValid() {
					args = append(args, trace.TraceIDKey, sc.TraceIDString(), trace.SpanIDKey, sc.SpanIDString())
				}
				if actor != "" {
					args = append(args, FieldActor, actor)
				}
				l := logger.WithContext(ctx)
				if status >= 500 {
					l.Error(RequestLogMessage, append(args, FieldError, cause.Error(), FieldErrorType, errorType(cause))...)
					return
				}
				l.Info(RequestLogMessage, args...)
			}

			defer func() {
				if p := recover(); p != nil {
					if p != http.ErrAbortHandler { //nolint:errorlint // the sentinel is compared by identity, as net/http does
						finish(p)
					} else {
						span.End()
					}
					panic(p)
				}
			}()
			next.ServeHTTP(rw, inner)
			finish(nil)
		})
	}
}

// errorType names the innermost error type of the chain, without its message.
func errorType(err error) string {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return fmt.Sprintf("%T", err)
		}
		err = next
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// routeTemplate turns a ServeMux pattern ("[METHOD ][HOST]/path") into the route label: the path
// part only.
func routeTemplate(pattern string) string {
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		return pattern[i:]
	}
	return RouteUnmatched
}

func knownMethod(m string) string {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return m
	}
	return MethodOther
}

// responseWriter records the status without hiding the capabilities of the wrapped writer:
// Flush works for streamed responses (server-sent events) and Unwrap lets
// http.ResponseController reach the real writer (deadlines, hijacking).
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher.
func (w *responseWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap returns the wrapped writer (http.ResponseController).
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// MaxCorrelationIDLength is the longest correlation id accepted from a client: the outbox and
// audit tables store it in 64 characters.
const MaxCorrelationIDLength = 64

// ValidCorrelationID reports whether a client-supplied correlation id can be used as is: 1 to
// 64 characters among letters, digits, '-', '_', '.' and ':'. That covers UUIDs, ULIDs and trace
// ids, and keeps out line breaks, quotes and anything that could not be stored.
func ValidCorrelationID(id string) bool {
	if id == "" || len(id) > MaxCorrelationIDLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == ':':
		default:
			return false
		}
	}
	return true
}

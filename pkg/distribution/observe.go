package distribution

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
	"github.com/jhermoso/karpo-fw-go/pkg/observability"
)

// Observe is the request telemetry of a service: for every request it starts a server span
// (continuing the caller's trace when the traceparent header is valid), records the duration in
// http.server.request.duration by method, route and status, and writes one log line with the
// same names, the correlation and trace ids and, for every 5xx, its cause.
//
// The line carries no personal data: the route is the ServeMux pattern ("GET /api/x/{id}"),
// never the raw path, and neither the query string, the headers nor the client address appear.
//
// Mount it after Correlation, so the line carries the correlation id, and as close to the mux
// as possible: the route is read from the request the mux matched or from the response helpers
// (Respond, WriteJSON, WriteError). A nil logger means log.Default; a zero Telemetry creates no
// spans and no metrics, and the line is still written.
func Observe(logger log.Logger, tel observability.Telemetry) Middleware {
	if logger == nil {
		logger = log.Default()
	}
	return func(next http.Handler) http.Handler {
		duration := tel.M().Histogram(observability.MetricHTTPServerDuration, "s", "Duration of HTTP server requests.")
		tracer := tel.T()
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := r.Context()
			if sc, ok := observability.ParseTraceparent(r.Header.Get(observability.TraceparentHeader)); ok {
				ctx = observability.ContextWithRemoteSpanContext(ctx, sc)
			}
			ctx, span := tracer.Start(ctx, r.Method, observability.WithKind(observability.SpanKindServer),
				observability.WithAttributes(observability.String(observability.AttrHTTPMethod, r.Method)))
			st := &requestState{}
			ctx = context.WithValue(ctx, stateKey{}, st)
			sw := &statusWriter{ResponseWriter: w}
			inner := r.WithContext(ctx)

			defer func() {
				rec := recover()
				status := sw.status()
				if rec != nil {
					st.setCause(panicError(rec))
					if !sw.wrote {
						status = http.StatusInternalServerError // the Recovery outside will write it
					}
				}
				route := st.routeOr(inner.Pattern, r.Pattern)
				elapsed := time.Since(start)
				attrs := []observability.Attr{
					observability.String(observability.AttrHTTPMethod, r.Method),
					observability.String(observability.AttrHTTPRoute, route),
					observability.Int(observability.AttrHTTPStatusCode, status),
				}
				duration.Record(ctx, elapsed.Seconds(), attrs...)

				span.SetName(r.Method + " " + route) // OpenTelemetry server span name: "{method} {route}"
				span.SetAttributes(attrs[1:]...)
				if id := application.CorrelationID(ctx); id != "" {
					span.SetAttributes(observability.String(observability.FieldCorrelationID, id))
				}
				fields := []any{
					observability.AttrHTTPMethod, r.Method,
					observability.AttrHTTPRoute, route,
					observability.AttrHTTPStatusCode, status,
					observability.FieldDurationMs, math.Round(float64(elapsed.Microseconds())) / 1000,
				}
				line := logger.WithContext(ctx)
				if status >= http.StatusInternalServerError {
					cause := st.getCause()
					msg, typ := "no cause recorded: the handler wrote the status itself", strconv.Itoa(status)
					if cause != nil {
						msg, typ = cause.Error(), errorType(cause)
						span.RecordError(cause)
					}
					span.SetStatus(observability.StatusError, msg)
					line.Error("http request", append(fields, observability.FieldError, msg, observability.FieldErrorType, typ)...)
				} else {
					line.Info("http request", fields...)
				}
				span.End()
				if rec != nil {
					panic(rec)
				}
			}()
			next.ServeHTTP(sw, inner)
		})
	}
}

// RecordError attaches the cause of a failed request to its telemetry, so the request line and
// the span of a 5xx carry it. WriteError and Recovery call it; a handler that writes its own 5xx
// should too. Without Observe in the chain it does nothing.
func RecordError(ctx context.Context, err error) {
	if st, ok := ctx.Value(stateKey{}).(*requestState); ok && err != nil {
		st.setCause(err)
	}
}

// noteRoute records the matched route pattern for Observe (r.Pattern is set by ServeMux on the
// request it hands to the handler, which Observe may not see when another middleware copied it).
func noteRoute(r *http.Request) {
	if r == nil || r.Pattern == "" {
		return
	}
	if st, ok := r.Context().Value(stateKey{}).(*requestState); ok {
		st.mu.Lock()
		st.route = r.Pattern
		st.mu.Unlock()
	}
}

type stateKey struct{}

type requestState struct {
	mu    sync.Mutex
	route string
	cause error
}

func (s *requestState) setCause(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cause == nil {
		s.cause = err // the first cause is the root one; a later panic while writing is not
	}
}

func (s *requestState) getCause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cause
}

func (s *requestState) routeOr(patterns ...string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.route != "" {
		return routeTemplate(s.route)
	}
	for _, p := range patterns {
		if p != "" {
			return routeTemplate(p)
		}
	}
	return observability.RouteUnknown
}

// routeTemplate turns a ServeMux pattern ("[METHOD ][HOST]/PATH") into the http.route of the
// OpenTelemetry conventions: the path template alone ("/api/x/{id}").
func routeTemplate(pattern string) string {
	if i := strings.IndexByte(pattern, '/'); i >= 0 {
		return pattern[i:]
	}
	return observability.RouteUnknown
}

func panicError(rec any) error {
	if err, ok := rec.(error); ok {
		return fmt.Errorf("panic: %w", err)
	}
	return fmt.Errorf("panic: %v", rec)
}

// errorType is the Go type of the innermost error of a chain of single wraps (what the C# side
// reports as the exception type).
func errorType(err error) string {
	for {
		inner := errors.Unwrap(err)
		if inner == nil {
			return fmt.Sprintf("%T", err)
		}
		err = inner
	}
}

// statusWriter records the status of a response without hiding what the underlying writer can
// do: it flushes (http.Flusher, so server-sent events and streamed downloads keep working),
// hijacks, and unwraps for http.ResponseController.
type statusWriter struct {
	http.ResponseWriter
	code  int
	wrote bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wrote && code >= 200 { // 1xx are informational: the final status comes later
		w.code, w.wrote = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.code, w.wrote = http.StatusOK, true
	}
	return w.ResponseWriter.Write(b)
}

// Flush implements http.Flusher; it is a no-op when the underlying writer cannot flush.
func (w *statusWriter) Flush() {
	if !w.wrote {
		w.code, w.wrote = http.StatusOK, true
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Hijack implements http.Hijacker (http.ErrNotSupported when the underlying writer cannot).
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

// Unwrap lets http.ResponseController reach the underlying writer (deadlines, full duplex...).
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) status() int {
	if !w.wrote {
		return http.StatusOK // nothing written: net/http sends 200
	}
	return w.code
}

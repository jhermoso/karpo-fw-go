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

// Campos de la línea de petición que escribe Observe. C# (Paranoia.Karpo.Fw) escribe los mismos nombres.
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

	// RouteUnmatched es la ruta de las peticiones que no casan con ningún patrón (404 del mux, o
	// un manejador que nunca dijo su patrón): nunca se usa la ruta en bruto, crearía una serie de
	// métricas por identificador.
	RouteUnmatched = "unmatched"
	// MethodOther sustituye a los métodos fuera del conjunto estándar, que un cliente puede inventar.
	MethodOther = "_OTHER"

	// StatusAborted es el estado que se registra para un manejador que abortó (http.ErrAbortHandler)
	// antes de escribir una respuesta: el cliente no recibió ninguna. 499 es el código convencional
	// de nginx para «cerrada sin respuesta»; ErrorTypeAborted es su error_type.
	StatusAborted    = 499
	ErrorTypeAborted = "aborted"
)

// Router resuelve el patrón con el que casa una petición. *http.ServeMux lo implementa.
type Router interface {
	Handler(r *http.Request) (h http.Handler, pattern string)
}

type observeConfig struct {
	router Router
	quiet  map[string]bool
}

// ObserveOption configura Observe.
type ObserveOption func(*observeConfig)

// ObserveRoutes resuelve el patrón de la ruta preguntando a router (el mux en el que están
// registradas las rutas) antes de servir la petición. Sin ella, la ruta es el patrón que dicen las
// funciones auxiliares de respuesta (WriteError, Respond) o el que puso el mux cuando Observe lo
// envuelve directamente; los manejadores que responden con WriteJSON detrás de otro middleware
// necesitan esta opción para tener ruta.
func ObserveRoutes(router Router) ObserveOption {
	return func(c *observeConfig) { c.router = router }
}

// ObserveQuietly omite la línea de log de las peticiones con éxito a estas rutas exactas (p. ej.
// /healthz y /readyz, consultadas cada pocos segundos). Se siguen midiendo, y los fallos se siguen
// registrando.
func ObserveQuietly(paths ...string) ObserveOption {
	return func(c *observeConfig) {
		for _, p := range paths {
			c.quiet[p] = true
		}
	}
}

// requestRecord es lo que las capas internas le cuentan a Observe de la petición. El ServeMux pone
// el patrón en la petición que recibe y cada middleware intermedio trabaja sobre una copia
// (r.WithContext), así que el middleware externo no puede leerlo de su propia petición.
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

// noteRoute le dice a Observe el patrón con el que el mux ha casado r.
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

// RecordError le dice a Observe el error que hay detrás de la respuesta de r, para que un 5xx deje
// su causa en el log y en el span. La llaman WriteError, Respond y Recovery; los manejadores que
// escriben un 5xx por otros medios también deberían llamarla. Sin Observe en la cadena no hace nada.
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

// noteActor le dice a Observe quién llama (sólo el identificador).
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

// Observe es la telemetría de la entrada HTTP. Por cada petición deja:
//
//   - un span de servidor, hijo del traceparent W3C entrante cuando hay uno válido;
//   - una observación de http.server.request.duration (segundos) por método, ruta y estado;
//   - una línea de log con method, route, status, duration_ms, correlation_id, trace_id, span_id
//     y el id del actor: Info, o Error con la causa cuando el estado es 5xx.
//
// La ruta es siempre el patrón ("/parties/{id}"), nunca la ruta real, y no se escribe nada
// personal: ni query string, ni cabeceras, ni cuerpos, ni nombre del actor.
//
// Se monta justo después de Correlation, fuera de todo lo demás:
//
//	distribution.Chain(mux, distribution.Correlation(), distribution.Observe(logger, tracer, meter), ...)
//
// Cualquiera de logger, tracer y meter puede ser nil: esa parte se omite.
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
				aborted := panicked == http.ErrAbortHandler //nolint:errorlint // the sentinel is compared by identity, as net/http does
				if panicked != nil {
					// Recovery (fuera) responderá 500 salvo que la respuesta ya haya empezado. Un
					// manejador abortado no recibe respuesta alguna: StatusAborted lo marca.
					if status == 0 {
						status = http.StatusInternalServerError
						if aborted {
							status = StatusAborted
						}
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
				failed := status >= 500 || aborted
				if failed {
					span.RecordError(cause)
				}
				span.End()

				if logger == nil || (!failed && cfg.quiet[r.URL.Path]) {
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
				if aborted {
					l.Warn(RequestLogMessage, append(args, FieldError, cause.Error(), FieldErrorType, ErrorTypeAborted)...)
					return
				}
				if status >= 500 {
					l.Error(RequestLogMessage, append(args, FieldError, cause.Error(), FieldErrorType, errorType(cause))...)
					return
				}
				l.Info(RequestLogMessage, args...)
			}

			defer func() {
				if p := recover(); p != nil {
					// Los manejadores abortados (http.ErrAbortHandler, p. ej. httputil.ReverseProxy cuando
					// falla el upstream) también se miden y se registran: en masa son una incidencia.
					finish(p)
					panic(p)
				}
			}()
			next.ServeHTTP(rw, inner)
			finish(nil)
		})
	}
}

// errorType nombra el tipo de error más interno de la cadena, sin su mensaje.
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

// routeTemplate convierte un patrón de ServeMux ("[METHOD ][HOST]/path") en la etiqueta de ruta:
// sólo la parte de la ruta.
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

// responseWriter registra el estado sin ocultar las capacidades del writer envuelto: Flush
// funciona con respuestas en streaming (server-sent events) y Unwrap deja que
// http.ResponseController llegue al writer real (plazos, hijacking).
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	// Los 1xx son informativos (100 Continue, 103 Early Hints): el estado final llega después.
	// 101 Switching Protocols es final (la conexión cambia de protocolo).
	if w.status == 0 && (code >= 200 || code == http.StatusSwitchingProtocols) {
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

// Flush implementa http.Flusher.
func (w *responseWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Unwrap devuelve el writer envuelto (http.ResponseController).
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// MaxCorrelationIDLength es la longitud máxima del id de correlación que se acepta de un cliente:
// las tablas del outbox y de auditoría lo guardan en 64 caracteres.
const MaxCorrelationIDLength = 64

// ValidCorrelationID dice si un id de correlación aportado por el cliente se puede usar tal cual:
// de 1 a 64 caracteres entre letras, dígitos, '-', '_', '.' y ':'. Eso cubre UUID, ULID e ids de
// traza, y deja fuera saltos de línea, comillas y todo lo que no se podría guardar.
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

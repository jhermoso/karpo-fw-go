package distribution

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

// Middleware define la firma estándar de un middleware HTTP.
type Middleware func(http.Handler) http.Handler

// Chain envuelve un manejador base con varios middlewares, que se ejecutan en el orden dado.
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// Recovery captura los panics no gestionados, registra el error en el log y devuelve una respuesta
// de error JSON 500 con el id de correlación. http.ErrAbortHandler no es un error que haya que
// responder: se vuelve a lanzar para que net/http aborte la respuesta, como hace sin Recovery.
//
// Recovery suele ser el middleware más externo, fuera de Correlation, así que su petición no lleva
// el id de correlación: toma entonces el que Correlation ya ha puesto en la cabecera de la respuesta.
func Recovery(logger log.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler { //nolint:errorlint // the sentinel is compared by identity, as net/http does
						panic(rec)
					}
					err, ok := rec.(error)
					if !ok {
						err = fmt.Errorf("%v", rec)
					}
					ctx := r.Context()
					correlation := application.CorrelationID(ctx)
					if id := w.Header().Get(CorrelationHeader); correlation == "" && ValidCorrelationID(id) {
						correlation = id
						ctx = application.WithCorrelationID(ctx, id)
					}
					RecordError(r, err)
					if logger != nil {
						logger.WithContext(ctx).Error("Unhandled HTTP panic recovered", "error", err, "path", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(ProblemDetails{
						Title:         "Internal Server Error",
						Status:        http.StatusInternalServerError,
						Detail:        "An unexpected internal server error occurred",
						CorrelationID: correlation,
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestLogging registra en el log el método HTTP, la ruta, la duración y el código de estado.
//
// Es preferible Observe: registra el patrón de la ruta en vez de la ruta, añade la traza y la causa
// de cada 5xx, y mide la petición. RequestLogging se queda para quien sólo quiere la línea.
func RequestLogging(logger log.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &responseWriter{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			if logger != nil {
				duration := time.Since(start)
				status := rec.status
				if status == 0 {
					status = http.StatusOK
				}
				logger.WithContext(r.Context()).Info("HTTP request processed",
					"method", r.Method,
					"path", r.URL.Path,
					"status", status,
					"duration_ms", duration.Milliseconds(),
				)
			}
		})
	}
}

// Cabeceras que lee TenantActorContext.
const (
	TenantHeader       = "X-Tenant-ID"
	ActorHeader        = "X-Actor-ID"
	ActorNameHeader    = "X-Actor-Name"
	OrganizationHeader = "X-Organization-ID"
	ChannelHeader      = "X-Channel"
)

// TenantActorContext pasa X-Tenant-ID, X-Actor-ID y X-Organization-ID al contexto de la
// petición. Cuando X-Actor-ID es un UUID pone además el actor de aplicación (application.WithActor,
// con el nombre de X-Actor-Name), que el Orchestrator usa para sellar los datos de auditoría;
// X-Channel pone el canal de la operación (web, mobile, device, api).
//
// Estas cabeceras deben venir de una pasarela de confianza que haya autenticado a quien llama:
// este middleware no autentica a nadie. Las API autenticadas usan en su lugar Authorize, que
// deduce el actor del contexto de autorización resuelto e ignora estas cabeceras.
func TenantActorContext() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if tenant := r.Header.Get(TenantHeader); tenant != "" {
				ctx = WithTenantID(ctx, tenant)
			}
			if actor := r.Header.Get(ActorHeader); actor != "" {
				ctx = WithActorID(ctx, actor)
				if id, err := domain.ParseUUID(actor); err == nil {
					name := r.Header.Get(ActorNameHeader)
					if name == "" {
						name = actor
					}
					if a, err := vocab.NewActor(id, name); err == nil {
						ctx = application.WithActor(ctx, a)
					}
				}
			}
			if org := r.Header.Get(OrganizationHeader); org != "" {
				ctx = WithOrganizationID(ctx, org)
			}
			if ch := r.Header.Get(ChannelHeader); ch != "" {
				ctx = application.WithChannel(ctx, ch)
			}
			noteActor(ctx)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CorrelationHeader lleva el id de correlación de un flujo de negocio de un servicio a otro.
const CorrelationHeader = "X-Correlation-ID"

// Correlation propaga X-Correlation-ID (y genera uno si falta) al contexto de aplicación, para que
// los logs y los mensajes del outbox de todo el flujo lo compartan, y lo devuelve en la respuesta.
// Un valor del cliente que no es un ValidCorrelationID (demasiado largo para las columnas que lo
// guardan, o con caracteres que podrían falsear líneas de log) se sustituye por uno nuevo y nunca
// se devuelve.
func Correlation() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(CorrelationHeader)
			if !ValidCorrelationID(id) {
				id = domain.NewUUID().String()
			}
			w.Header().Set(CorrelationHeader, id)
			next.ServeHTTP(w, r.WithContext(application.WithCorrelationID(r.Context(), id)))
		})
	}
}

// CORS pone las cabeceras estándar y permisivas de Cross-Origin Resource Sharing en los endpoints de la API.
func CORS() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID, X-Actor-ID, X-Actor-Name, X-Organization-ID, X-Channel, X-Correlation-ID, Idempotency-Key")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

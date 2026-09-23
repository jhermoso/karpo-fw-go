package distribution

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/log"
)

// Middleware defines a standard HTTP middleware signature.
type Middleware func(http.Handler) http.Handler

// Chain wraps a base handler with multiple middlewares executed in the order provided.
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// Recovery catches unhandled panics, logs the error, and returns a JSON 500 error response.
func Recovery(logger log.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					err, ok := rec.(error)
					if !ok {
						err = fmt.Errorf("%v", rec)
					}
					if logger != nil {
						logger.Error("Unhandled HTTP panic recovered", "error", err, "path", r.URL.Path)
					}
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(ProblemDetails{
						Title:  "Internal Server Error",
						Status: http.StatusInternalServerError,
						Detail: "An unexpected internal server error occurred",
					})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *responseRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

// RequestLogging logs the incoming HTTP method, path, duration, and status code.
func RequestLogging(logger log.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rec, r)

			if logger != nil {
				duration := time.Since(start)
				logger.Info("HTTP request processed",
					"method", r.Method,
					"path", r.URL.Path,
					"status", rec.statusCode,
					"duration_ms", duration.Milliseconds(),
				)
			}
		})
	}
}

// TenantActorContext extracts X-Tenant-ID, X-Actor-ID, and X-Organization-ID headers into the request context.
func TenantActorContext() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			if tenant := r.Header.Get("X-Tenant-ID"); tenant != "" {
				ctx = WithTenantID(ctx, tenant)
			}
			if actor := r.Header.Get("X-Actor-ID"); actor != "" {
				ctx = WithActorID(ctx, actor)
			}
			if org := r.Header.Get("X-Organization-ID"); org != "" {
				ctx = WithOrganizationID(ctx, org)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CORS provides standard permissive Cross-Origin Resource Sharing headers for API endpoints.
func CORS() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Tenant-ID, X-Actor-ID, X-Organization-ID")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

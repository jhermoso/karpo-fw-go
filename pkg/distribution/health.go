package distribution

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// CheckFunc evaluates the health of a specific subsystem.
type CheckFunc func(ctx context.Context) error

// HealthRegistry manages liveness and readiness health checks.
type HealthRegistry struct {
	mu              sync.RWMutex
	readinessProbes map[string]CheckFunc
}

// NewHealthRegistry creates a new HealthRegistry.
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{
		readinessProbes: make(map[string]CheckFunc),
	}
}

// RegisterReadinessProbe registers a check required for the service to accept traffic.
func (h *HealthRegistry) RegisterReadinessProbe(name string, probe CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readinessProbes[name] = probe
}

// HealthStatus represents the payload returned by health endpoints.
type HealthStatus struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// LivenessHandler returns HTTP 200 OK as long as the process is running.
func (h *HealthRegistry) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(HealthStatus{Status: "UP"})
	}
}

// ReadinessHandler evaluates all registered readiness probes. If any probe fails, it returns 503.
func (h *HealthRegistry) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h.mu.RLock()
		probes := make(map[string]CheckFunc, len(h.readinessProbes))
		for k, v := range h.readinessProbes {
			probes[k] = v
		}
		h.mu.RUnlock()

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		results := make(map[string]string)
		allOk := true

		for name, probe := range probes {
			if err := probe(ctx); err != nil {
				allOk = false
				results[name] = "FAIL: " + err.Error()
			} else {
				results[name] = "OK"
			}
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if !allOk {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(HealthStatus{
				Status: "DOWN",
				Checks: results,
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(HealthStatus{
			Status: "UP",
			Checks: results,
		})
	}
}

// Mount mounts /healthz and /readyz onto the provided ServeMux.
func (h *HealthRegistry) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.LivenessHandler())
	mux.HandleFunc("/readyz", h.ReadinessHandler())
}

package distribution

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// CheckFunc evalúa la salud de un subsistema concreto.
type CheckFunc func(ctx context.Context) error

// HealthRegistry gestiona las comprobaciones de salud de liveness y readiness.
type HealthRegistry struct {
	mu              sync.RWMutex
	readinessProbes map[string]CheckFunc
}

// NewHealthRegistry crea un HealthRegistry nuevo.
func NewHealthRegistry() *HealthRegistry {
	return &HealthRegistry{
		readinessProbes: make(map[string]CheckFunc),
	}
}

// RegisterReadinessProbe registra una comprobación necesaria para que el servicio acepte tráfico.
func (h *HealthRegistry) RegisterReadinessProbe(name string, probe CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.readinessProbes[name] = probe
}

// HealthStatus representa el cuerpo que devuelven los endpoints de salud.
type HealthStatus struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// LivenessHandler devuelve HTTP 200 OK mientras el proceso esté en marcha.
func (h *HealthRegistry) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(HealthStatus{Status: "UP"})
	}
}

// ReadinessHandler evalúa todas las sondas de readiness registradas. Si falla alguna, devuelve 503.
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

// Mount monta /healthz y /readyz en el ServeMux dado.
func (h *HealthRegistry) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.LivenessHandler())
	mux.HandleFunc("/readyz", h.ReadinessHandler())
}

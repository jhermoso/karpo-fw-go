// Package audit composes the Audit (Historial: quién cambió qué y cuándo) bounded context. It has
// no storage: it reads the audit logs the other contexts keep.
package audit

import (
	"net/http"

	aapp "github.com/jhermoso/karpo-fw-go/contexts/audit/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
)

// Module is the composed context.
type Module struct {
	Service  *aapp.Service
	Registry *aapp.Registry
}

// Compose builds the context. The host then registers each kind of aggregate.
func Compose() *Module {
	reg := aapp.NewRegistry()
	return &Module{Service: aapp.NewService(reg), Registry: reg}
}

// Register declares a kind of aggregate, the audit log of its context and how to tell whether the
// caller may see it (nil: only a global administrator reads its history).
func (m *Module) Register(aggregateType string, log application.AuditLog, guard aapp.Guard) *Module {
	m.Registry.Register(aggregateType, log, guard)
	return m
}

// RegisterRoutes implements distribution.EndpointModule.
func (m *Module) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/audit/types", func(w http.ResponseWriter, r *http.Request) {
		out, err := m.Service.Types.Handle(r.Context(), aapp.ListTypes{})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
	mux.HandleFunc("GET /api/audit/trail/{type}/{id}", func(w http.ResponseWriter, r *http.Request) {
		out, err := m.Service.Trail.Handle(r.Context(), aapp.GetTrail{Type: r.PathValue("type"), ID: r.PathValue("id")})
		distribution.Respond(w, r, out, err, http.StatusOK)
	})
}

var _ distribution.EndpointModule = (*Module)(nil)

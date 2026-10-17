// Package audit compone el contexto delimitado Audit (Historial: quién cambió qué y cuándo). No
// tiene almacenamiento: lee los registros de auditoría que llevan los otros contextos.
package audit

import (
	"net/http"

	aapp "github.com/jhermoso/karpo-fw-go/contexts/audit/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution"
)

// Module es el contexto compuesto.
type Module struct {
	Service  *aapp.Service
	Registry *aapp.Registry
}

// Compose construye el contexto. Después el anfitrión registra cada tipo de agregado.
func Compose() *Module {
	reg := aapp.NewRegistry()
	return &Module{Service: aapp.NewService(reg), Registry: reg}
}

// Register declara un tipo de agregado, el registro de auditoría de su contexto y cómo saber si
// quien llama puede verlo (nil: sólo un administrador global lee su historial).
func (m *Module) Register(aggregateType string, log application.AuditLog, guard aapp.Guard) *Module {
	m.Registry.Register(aggregateType, log, guard)
	return m
}

// RegisterRoutes implementa distribution.EndpointModule.
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

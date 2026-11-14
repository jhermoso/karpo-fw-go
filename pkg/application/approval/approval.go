// Package approval es la capa de aplicación del motor de aprobaciones (D-156): la política que dice
// qué acciones exigen aprobación, el permiso de aprobar cada acción y el servicio que pide,
// aprueba, rechaza y cancela solicitudes. El modelo y sus reglas están en pkg/domain/approval.
//
// Cómo lo usa un contexto (docs/APROBACIONES.md):
//  1. antes de ejecutar una acción, pregunta a la Policy si la exige en la empresa;
//  2. si la exige, llama a Service.Request en lugar de ejecutarla y responde «pendiente»;
//  3. se suscribe al evento approval.approved de su acción y la aplica entonces.
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	dom "github.com/jhermoso/karpo-fw-go/pkg/domain/approval"
)

// Policy dice si una acción exige aprobación en una empresa. La configuración por empresa es el
// paso 2 de D-156; aquí sólo están Never y Fixed.
type Policy interface {
	Requires(ctx context.Context, company domain.UUID, action dom.Action) (bool, error)
}

// Never es la política por defecto: ninguna acción exige aprobación.
type Never struct{}

// Requires devuelve siempre false.
func (Never) Requires(context.Context, domain.UUID, dom.Action) (bool, error) { return false, nil }

// Fixed exige aprobación para las acciones listadas, en todas las empresas (pruebas y arranque).
type Fixed map[dom.Action]bool

// Requires dice si la acción está en la lista.
func (f Fixed) Requires(_ context.Context, _ domain.UUID, a dom.Action) (bool, error) {
	return f[a], nil
}

// ApprovePermission devuelve el permiso de aprobar (o rechazar) una acción, con la forma de los
// permisos de Karpo {Subdomain}.{Resource}.{Action}: "purchases.supplier.block" ->
// "Purchases.Supplier.ApproveBlock".
func ApprovePermission(a dom.Action) authz.Permission {
	c, r, v := a.Segments()
	return authz.PermissionOf(pascal(c), pascal(r), "Approve"+pascal(v))
}

func pascal(s string) string {
	var b strings.Builder
	for _, p := range strings.Split(s, "_") {
		if p != "" {
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return b.String()
}

// Repository es el almacén de solicitudes (memory.NewRepository o sqlrepo con pkg/persistence/approvals).
type Repository = domain.Repository[dom.RequestID, *dom.Request]

// Service pide y resuelve solicitudes. El actor sale del contexto de autorización de ctx. Los
// eventos quedan pendientes en el agregado: los despacha quien orqueste la unidad de trabajo
// (application.Orchestrator + outbox), como en cualquier otra orden.
type Service struct {
	Repo Repository
}

// RequestInput es lo que se pide.
type RequestInput struct {
	Company domain.UUID
	Action  dom.Action
	Target  dom.Target
	Payload json.RawMessage
	Reason  string
}

func actor(ctx context.Context) (*authz.Context, domain.UUID, error) {
	c, ok := authz.FromContext(ctx)
	if !ok {
		return nil, domain.UUID{}, fmt.Errorf("%w: no authorization context", domain.ErrUnauthorized)
	}
	return c, c.Actor().PartyID, nil
}

// Request crea una solicitud pendiente a nombre del actor. Quien llama ya ha comprobado que el
// actor puede pedir la acción (el mismo permiso que ejecutarla).
func (s Service) Request(ctx context.Context, in RequestInput) (*dom.Request, error) {
	_, by, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := dom.New(dom.NewRequestID(), in.Company, in.Action, in.Target, in.Payload, in.Reason, by)
	if err != nil {
		return nil, err
	}
	if err := s.Repo.Save(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// Approve aprueba: exige el permiso de aprobar la acción y que el actor no sea el solicitante.
func (s Service) Approve(ctx context.Context, id dom.RequestID, comment string) (*dom.Request, error) {
	return s.resolve(ctx, id, true, func(r *dom.Request, by domain.UUID) error { return r.Approve(by, comment) })
}

// Reject rechaza con comentario: mismo permiso que aprobar.
func (s Service) Reject(ctx context.Context, id dom.RequestID, comment string) (*dom.Request, error) {
	return s.resolve(ctx, id, true, func(r *dom.Request, by domain.UUID) error { return r.Reject(by, comment) })
}

// Cancel retira la solicitud: sólo su solicitante, sin permiso adicional.
func (s Service) Cancel(ctx context.Context, id dom.RequestID, comment string) (*dom.Request, error) {
	return s.resolve(ctx, id, false, func(r *dom.Request, by domain.UUID) error { return r.Cancel(by, comment) })
}

func (s Service) resolve(ctx context.Context, id dom.RequestID, needsPermission bool, fn func(*dom.Request, domain.UUID) error) (*dom.Request, error) {
	c, by, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := s.Repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if needsPermission {
		if p := ApprovePermission(r.Action()); !c.HasPermission(p) {
			return nil, fmt.Errorf("%w: permission %s required", domain.ErrForbidden, p)
		}
	}
	if err := fn(r, by); err != nil {
		return nil, err
	}
	if err := s.Repo.Save(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

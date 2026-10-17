package authz

import (
	"context"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Permission es un código de permiso "{Subdomain}.{Resource}.{Action}" (p. ej. "Parties.Party.Create").
type Permission string

// Wildcard concede todos los permisos. Los principales de servicio nunca lo reciben.
const Wildcard Permission = "*.*.*"

// PermissionOf construye un código de permiso.
func PermissionOf(subdomain, resource, action string) Permission {
	return Permission(subdomain + "." + resource + "." + action)
}

// WellFormed dice si el código tiene tres segmentos no vacíos.
func (p Permission) WellFormed() bool {
	parts := strings.Split(string(p), ".")
	if len(parts) != 3 {
		return false
	}
	for _, s := range parts {
		if strings.TrimSpace(s) == "" {
			return false
		}
	}
	return true
}

// MustPermission valida un código en tiempo de composición (entra en pánico si está mal formado),
// como el RequirePermission de C# que rechaza los nombres de política mal formados.
func MustPermission(code string) Permission {
	p := Permission(code)
	if !p.WellFormed() {
		panic(fmt.Sprintf("authz: malformed permission %q, expected {Subdomain}.{Resource}.{Action}", code))
	}
	return p
}

// Require comprueba que ctx lleva un contexto de autorización que concede p. Devuelve un error
// que coincide con domain.ErrUnauthorized si no hay contexto (sin alternativa Legacy) y con
// domain.ErrForbidden si falta el permiso.
func Require(ctx context.Context, p Permission) error {
	c, ok := FromContext(ctx)
	if !ok {
		return fmt.Errorf("%w: no authorization context", domain.ErrUnauthorized)
	}
	if !c.HasPermission(p) {
		return fmt.Errorf("%w: permission %s required", domain.ErrForbidden, p)
	}
	return nil
}

// RequireWrite comprueba que el sujeto de ctx puede escribir datos propiedad de org (concesión Full
// dentro del ámbito, o administrador global). El mensaje genérico nunca revela si la organización existe.
func RequireWrite(ctx context.Context, org domain.UUID) error {
	c, ok := FromContext(ctx)
	if !ok {
		return fmt.Errorf("%w: no authorization context", domain.ErrUnauthorized)
	}
	if !c.CanWrite(org) {
		return fmt.Errorf("%w: organization outside the writable scope", domain.ErrForbidden)
	}
	return nil
}

// ServicePrincipal es un llamante técnico configurado y sus permisos (contrato v1, sección 6).
type ServicePrincipal struct {
	Subject     domain.UUID
	Name        string
	Permissions []Permission
}

// ServicePrincipalContext construye el contexto de un principal de servicio configurado: sin
// concesiones, sin ámbito de organizaciones, nunca administrador global, y el comodín se descarta
// aunque esté configurado.
func ServicePrincipalContext(tenant string, sp ServicePrincipal, req Request) (*Context, error) {
	perms := make([]Permission, 0, len(sp.Permissions))
	for _, p := range sp.Permissions {
		if p != Wildcard && p.WellFormed() {
			perms = append(perms, p)
		}
	}
	return NewContext(Context{
		TenantID: tenant, Subject: sp.Subject, SubjectName: sp.Name, Kind: Service,
		ActorPartyID: sp.Subject, Permissions: perms,
		RequestedOrganizations: req.RequestedOrganizations, EffectiveOrganizations: []domain.UUID{},
		PolicyVersion: PolicyVersion(true, nil, perms, nil), CorrelationID: req.CorrelationID,
	})
}

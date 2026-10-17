package authz

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// AccessLevel es el acceso que tiene un usuario a una organización.
type AccessLevel string

// Niveles de acceso (contrato v1, P2: Full escribe; ReadOnly lee; Restricted se comporta como ReadOnly).
const (
	Full       AccessLevel = "Full"
	ReadOnly   AccessLevel = "ReadOnly"
	Restricted AccessLevel = "Restricted"
)

// ParseAccessLevel analiza un nivel almacenado; los valores desconocidos o vacíos son Restricted (nunca falla).
func ParseAccessLevel(s string) AccessLevel {
	switch AccessLevel(s) {
	case Full, ReadOnly:
		return AccessLevel(s)
	}
	return Restricted
}

// CanWrite dice si el nivel permite escribir.
func (l AccessLevel) CanWrite() bool { return l == Full }

// Grant es un acceso del sujeto a una organización, tal como lo guarda Security. IncludeSubsidiaries
// se transporta pero no expande nada en v1 (decisión de negocio P2).
type Grant struct {
	OrganizationID      domain.UUID `json:"organizationId"`
	Level               AccessLevel `json:"level"`
	IncludeSubsidiaries bool        `json:"includeSubsidiaries"`
}

// Context es la autorización resuelta de una petición (AuthorizationContext del contrato v1).
// Se construye con NewContext, que hace cumplir los invariantes del contrato.
type Context struct {
	TenantID               string        `json:"tenantId"`
	Subject                domain.UUID   `json:"subjectId"`
	SubjectName            string        `json:"subjectName"`
	Kind                   ActorKind     `json:"actorKind"`
	ActorPartyID           domain.UUID   `json:"actorPartyId,omitzero"`
	GlobalAdmin            bool          `json:"isGlobalAdmin"`
	Grants                 []Grant       `json:"grants"`
	Permissions            []Permission  `json:"permissions"`
	RequestedOrganizations []domain.UUID `json:"requestedOrganizationIds,omitempty"`
	EffectiveOrganizations []domain.UUID `json:"effectiveOrganizationIds"`
	PolicyVersion          string        `json:"policyVersion"`
	CorrelationID          string        `json:"correlationId,omitempty"`
}

// NewContext valida los invariantes del contrato: un humano necesita un id de party; las
// organizaciones efectivas deben ser un subconjunto de las concesiones. Cuando las efectivas son nil
// se calculan como concesiones ∩ pedidas (todas las concesiones si no se pidió nada).
func NewContext(c Context) (*Context, error) {
	if c.Subject.IsZero() || strings.TrimSpace(c.SubjectName) == "" {
		return nil, fmt.Errorf("%w: subject and name are required", domain.ErrValidation)
	}
	if c.Kind == Human && c.ActorPartyID.IsZero() {
		return nil, fmt.Errorf("%w: a human actor requires a party id (contract v1, invariant 5)", domain.ErrValidation)
	}
	granted := make([]domain.UUID, 0, len(c.Grants))
	for _, g := range c.Grants {
		if !containsUUID(granted, g.OrganizationID) {
			granted = append(granted, g.OrganizationID)
		}
	}
	if c.EffectiveOrganizations == nil {
		c.EffectiveOrganizations = EffectiveOrganizations(c.Grants, c.RequestedOrganizations)
	}
	for _, id := range c.EffectiveOrganizations {
		if !containsUUID(granted, id) {
			return nil, fmt.Errorf("%w: effective organizations must be a subset of the grants (contract v1, invariant 2)", domain.ErrValidation)
		}
	}
	c.Grants = slices.Clone(c.Grants)
	c.Permissions = slices.Clone(c.Permissions)
	return &c, nil
}

// EffectiveOrganizations calcula concesiones ∩ pedidas (todas las organizaciones concedidas si no
// se pidió nada), sin expandir filiales.
func EffectiveOrganizations(grants []Grant, requested []domain.UUID) []domain.UUID {
	out := []domain.UUID{}
	for _, g := range grants {
		if containsUUID(out, g.OrganizationID) {
			continue
		}
		if len(requested) == 0 || containsUUID(requested, g.OrganizationID) {
			out = append(out, g.OrganizationID)
		}
	}
	return out
}

// HasPermission dice si el contexto concede un permiso: los administradores globales y el
// comodín lo conceden todo; los códigos mal formados nunca se conceden.
func (c *Context) HasPermission(p Permission) bool {
	if !p.WellFormed() {
		return false
	}
	return c.GlobalAdmin || slices.Contains(c.Permissions, Wildcard) || slices.Contains(c.Permissions, p)
}

// CanRead dice si la organización está dentro del ámbito efectivo.
func (c *Context) CanRead(org domain.UUID) bool {
	return c.GlobalAdmin || containsUUID(c.EffectiveOrganizations, org)
}

// CanWrite dice si el sujeto puede escribir datos propiedad de org: los administradores globales
// siempre; los demás necesitan una concesión Full sobre org y que org esté dentro del ámbito efectivo (P2).
func (c *Context) CanWrite(org domain.UUID) bool {
	if c.GlobalAdmin {
		return true
	}
	if !containsUUID(c.EffectiveOrganizations, org) {
		return false
	}
	for _, g := range c.Grants {
		if g.OrganizationID == org && g.Level.CanWrite() {
			return true
		}
	}
	return false
}

// PolicyVersion calcula la versión determinista de la política de un sujeto (SHA-256, hex) a partir
// de su marca de activo, roles, permisos y concesiones, todo ordenado. Los resolutores la incluyen en
// el contexto para que los clientes puedan detectar revocaciones; cambia con cualquier cambio relevante.
func PolicyVersion(active bool, roles []string, permissions []Permission, grants []Grant) string {
	r := slices.Clone(roles)
	slices.Sort(r)
	p := make([]string, len(permissions))
	for i, x := range permissions {
		p[i] = string(x)
	}
	slices.Sort(p)
	g := make([]string, len(grants))
	for i, x := range grants {
		g[i] = fmt.Sprintf("%s:%s:%t", x.OrganizationID, x.Level, x.IncludeSubsidiaries)
	}
	slices.Sort(g)
	h := sha256.Sum256([]byte(fmt.Sprintf("active=%t|roles=%s|perms=%s|grants=%s",
		active, strings.Join(r, ","), strings.Join(p, ","), strings.Join(g, ","))))
	return hex.EncodeToString(h[:])
}

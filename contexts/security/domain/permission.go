package domain

import (
	"crypto/sha256"
	"slices"
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// Permission es un código de permiso "{Namespace}.{Resource}.{Action}" (contrato v1), por ejemplo
// "Parties.Party.Create". El código es la identidad del permiso: cada contexto delimitado declara
// los suyos y Security guarda el catálogo (decisión 4).
type Permission string

// Wildcard concede todos los permisos. Sólo lo lleva el rol del sistema GlobalSuperAdmin.
const Wildcard Permission = "*.*.*"

// Acciones que conoce la regla estándar de roles (decisión 4, la regla P5 de C#).
const (
	ActionRead   = "Read"
	ActionCreate = "Create"
	ActionUpdate = "Update"
)

func (p Permission) parts() ([]string, bool) {
	parts := strings.Split(string(p), ".")
	if len(parts) != 3 {
		return nil, false
	}
	for _, s := range parts {
		if s == "" || strings.TrimSpace(s) != s {
			return nil, false
		}
	}
	return parts, true
}

// WellFormed dice si el código tiene tres segmentos no vacíos.
func (p Permission) WellFormed() bool {
	_, ok := p.parts()
	return ok
}

// Namespace devuelve el primer segmento (el contexto delimitado).
func (p Permission) Namespace() string { return p.segment(0) }

// Resource devuelve el segundo segmento.
func (p Permission) Resource() string { return p.segment(1) }

// Action devuelve el tercer segmento.
func (p Permission) Action() string { return p.segment(2) }

func (p Permission) segment(i int) string {
	if parts, ok := p.parts(); ok {
		return parts[i]
	}
	return ""
}

// normalizePermissions valida, quita duplicados y ordena códigos de permiso.
func normalizePermissions(perms []Permission) ([]Permission, error) {
	var v fw.Validation
	out := make([]Permission, 0, len(perms))
	for _, p := range perms {
		if !v.Require(p.WellFormed() && len(p) <= 200, "permissions", "format", "malformed permission "+string(p)) {
			continue
		}
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out, v.Err()
}

// PermissionKind es el nombre estable del tipo de agregado de una entrada del catálogo.
const PermissionKind = "security.permission"

// PermissionID es la identidad de almacenamiento de una entrada del catálogo. Se deriva del código,
// así que cada instalación da al mismo permiso el mismo id; los otros contextos nunca la ven.
type PermissionID struct{ fw.UUID }

// PermissionIDOf deriva la identidad de un permiso a partir de su código.
func PermissionIDOf(code Permission) PermissionID {
	sum := sha256.Sum256([]byte("karpo.security.permission:" + string(code)))
	var u fw.UUID
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x80 // RFC 9562 versión 8 (basada en nombre, propia)
	u[8] = (u[8] & 0x3f) | 0x80
	return PermissionID{u}
}

// PermissionEntry es una entrada del catálogo de permisos. Las entradas nunca se borran: un permiso
// que ya nadie declara se desactiva y deja de conceder (falla cerrado).
type PermissionEntry struct {
	fw.BaseAggregateRoot[PermissionID]
	traits.Activation
	code        Permission
	description string
}

// DeclarePermission crea la entrada del catálogo de un permiso.
func DeclarePermission(code Permission, description string) (*PermissionEntry, error) {
	return ReconstitutePermission(code, description, true)
}

// ReconstitutePermission reconstruye una entrada del catálogo a partir del estado persistido.
func ReconstitutePermission(code Permission, description string, active bool) (*PermissionEntry, error) {
	var v fw.Validation
	v.Require(code.WellFormed() && len(code) <= 200, "code", "format", "a permission is {Namespace}.{Resource}.{Action}")
	description = strings.TrimSpace(description)
	v.Require(len(description) <= 500, "description", "length", "at most 500 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	base, err := fw.NewBaseAggregateRoot(PermissionKind, PermissionIDOf(code))
	if err != nil {
		return nil, err
	}
	return &PermissionEntry{BaseAggregateRoot: base, Activation: traits.RestoredActivation(active), code: code, description: description}, nil
}

// Code devuelve el código del permiso.
func (p *PermissionEntry) Code() Permission { return p.code }

// Description devuelve la descripción.
func (p *PermissionEntry) Description() string { return p.description }

// DefaultDescription describe un permiso a partir de sus segmentos (como hacía el catálogo de C#).
func DefaultDescription(code Permission) string {
	if code == Wildcard {
		return "Full access to all resources and actions"
	}
	return code.Action() + " access to " + code.Namespace() + "." + code.Resource()
}

// Campos de las entradas del catálogo.
var (
	PermFieldCode   = spec.Comparable("code", func(p *PermissionEntry) string { return string(p.code) })
	PermFieldActive = spec.Comparable("active", (*PermissionEntry).IsActive)
)

// Catalog es el conjunto de permisos en vigor: las entradas activas del catálogo.
type Catalog struct{ codes map[Permission]struct{} }

// NewCatalog construye un catálogo a partir de códigos.
func NewCatalog(codes ...Permission) Catalog {
	c := Catalog{codes: make(map[Permission]struct{}, len(codes))}
	for _, p := range codes {
		c.codes[p] = struct{}{}
	}
	return c
}

// Has dice si el permiso está en vigor.
func (c Catalog) Has(p Permission) bool {
	_, ok := c.codes[p]
	return ok
}

// Codes devuelve los permisos en vigor, ordenados.
func (c Catalog) Codes() []Permission {
	out := make([]Permission, 0, len(c.codes))
	for p := range c.codes {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// Check informa de los permisos que el catálogo no conoce.
func (c Catalog) Check(perms []Permission) error {
	var v fw.Validation
	for _, p := range perms {
		v.Require(c.Has(p), "permissions", "unknown", "unknown permission "+string(p))
	}
	return v.Err()
}

// StandardPermissions es la regla que da a cada rol del sistema sus permisos (decisión 4, la regla
// que Javier aprobó como P5): GlobalSuperAdmin, el comodín; OrganizationAdmin, todo menos lo
// reservado al administrador global; StandardUser, Read, Create y Update fuera de Security;
// ReadOnlyUser, todos los Read; Customer, nada. Cualquier otra acción (Approve, Issue, Submit...)
// sólo llega a OrganizationAdmin o a un rol propio.
func StandardPermissions(role RoleID, c Catalog) []Permission {
	var out []Permission
	for _, p := range c.Codes() {
		if p == Wildcard {
			if role == RoleGlobalSuperAdmin {
				out = append(out, p)
			}
			continue
		}
		switch role {
		case RoleOrganizationAdmin:
			if !slices.Contains(GlobalOnlyPermissions, p) {
				out = append(out, p)
			}
		case RoleStandardUser:
			a := p.Action()
			if p.Namespace() != Namespace && (a == ActionRead || a == ActionCreate || a == ActionUpdate) {
				out = append(out, p)
			}
		case RoleReadOnlyUser:
			if p.Action() == ActionRead {
				out = append(out, p)
			}
		}
	}
	return out
}

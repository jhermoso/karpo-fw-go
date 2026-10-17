package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// RoleKind es el nombre estable del tipo de agregado.
const RoleKind = "security.role"

// Role es un conjunto de permisos con nombre. Un rol por instalación: las organizaciones a las que
// se aplica salen de los accesos de cada usuario, no del rol (decisión Q2 de C#).
type Role struct {
	fw.BaseAggregateRoot[RoleID]
	traits.Audited
	name        string
	description string
	system      bool
	permissions []Permission // ordenados, sin duplicados
}

// RoleState es el estado persistido de un rol.
type RoleState struct {
	Name        string
	Description string
	System      bool
	Permissions []Permission
	Audit       traits.AuditStamp
}

func cleanText(s string) string { return strings.Join(strings.Fields(s), " ") }

// ReconstituteRole reconstruye un rol a partir del estado persistido.
func ReconstituteRole(id RoleID, s RoleState) (*Role, error) {
	base, err := fw.NewBaseAggregateRoot(RoleKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	name := cleanText(s.Name)
	v.Require(name != "" && utf8.RuneCountInString(name) <= 100, "name", "length", "a role needs a name of at most 100 characters")
	description := strings.TrimSpace(s.Description)
	v.Require(utf8.RuneCountInString(description) <= 500, "description", "length", "at most 500 characters")
	perms, err := normalizePermissions(s.Permissions)
	v.Merge("", err)
	v.Require(!slices.Contains(perms, Wildcard) || id == RoleGlobalSuperAdmin, "permissions", "wildcard",
		"only the GlobalSuperAdmin role carries the wildcard")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Role{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), name: name, description: description,
		system: s.System, permissions: perms}, nil
}

// DefineRole crea un rol propio.
func DefineRole(id RoleID, name, description string, perms []Permission) (*Role, error) {
	r, err := ReconstituteRole(id, RoleState{Name: name, Description: description, Permissions: perms})
	if err != nil {
		return nil, err
	}
	r.Raise(RoleDefined{EventMeta: r.NewEventMeta(), Name: r.name, Permissions: r.permissionCodes()})
	return r, nil
}

// NewSystemRole construye un rol del sistema con los permisos que le da la regla estándar.
func NewSystemRole(seed SystemRole, c Catalog) (*Role, error) {
	return ReconstituteRole(seed.ID, RoleState{Name: seed.Name, Description: seed.Description, System: true,
		Permissions: StandardPermissions(seed.ID, c)})
}

// Name devuelve el nombre.
func (r *Role) Name() string { return r.name }

// NameKey devuelve el nombre en la forma que se usa para que los nombres sean únicos (sin distinguir mayúsculas).
func (r *Role) NameKey() string { return strings.ToLower(r.name) }

// Description devuelve la descripción.
func (r *Role) Description() string { return r.description }

// IsSystem dice si el rol es uno de los roles del sistema sembrados.
func (r *Role) IsSystem() bool { return r.system }

// Permissions devuelve los permisos que lleva el rol, ordenados.
func (r *Role) Permissions() []Permission { return slices.Clone(r.permissions) }

// IsGlobalAdmin dice si el rol hace a sus usuarios administradores globales.
func (r *Role) IsGlobalAdmin() bool { return slices.Contains(r.permissions, Wildcard) }

func (r *Role) permissionCodes() []string {
	out := make([]string, len(r.permissions))
	for i, p := range r.permissions {
		out[i] = string(p)
	}
	return out
}

func (r *Role) requireCustom(action string) error {
	if r.system {
		return fw.Violation("security.system_role", "a system role cannot be "+action)
	}
	return nil
}

// Describe cambia el nombre y la descripción de un rol propio.
func (r *Role) Describe(name, description string) error {
	if err := r.requireCustom("edited"); err != nil {
		return err
	}
	n, err := ReconstituteRole(r.ID(), RoleState{Name: name, Description: description, Permissions: r.permissions})
	if err != nil {
		return err
	}
	if n.name == r.name && n.description == r.description {
		return nil
	}
	r.name, r.description = n.name, n.description
	r.Raise(RoleDescribed{EventMeta: r.NewEventMeta(), Name: r.name})
	return nil
}

// SetPermissions sustituye los permisos de un rol propio. Quien llama los comprueba contra el
// catálogo; un rol propio nunca lleva el comodín.
func (r *Role) SetPermissions(perms []Permission) error {
	if err := r.requireCustom("edited"); err != nil {
		return err
	}
	n, err := ReconstituteRole(r.ID(), RoleState{Name: r.name, Permissions: perms})
	if err != nil {
		return err
	}
	r.replacePermissions(n.permissions)
	return nil
}

// SyncSystemPermissions da a un rol del sistema los permisos que la regla estándar calcula a partir
// del catálogo. Dice si cambiaron.
func (r *Role) SyncSystemPermissions(c Catalog) bool {
	if !r.system {
		return false
	}
	return r.replacePermissions(StandardPermissions(r.ID(), c))
}

func (r *Role) replacePermissions(perms []Permission) bool {
	if slices.Equal(perms, r.permissions) {
		return false
	}
	r.permissions = slices.Clone(perms)
	r.Raise(RolePermissionsChanged{EventMeta: r.NewEventMeta(), Name: r.name, Permissions: r.permissionCodes()})
	return true
}

// Retire marca un rol propio para quitarlo (la aplicación lo borra cuando ningún usuario lo tiene).
func (r *Role) Retire() error {
	if err := r.requireCustom("retired"); err != nil {
		return err
	}
	r.Raise(RoleRetired{EventMeta: r.NewEventMeta(), Name: r.name})
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (r *Role) AuditSnapshot() map[string]any {
	return map[string]any{"name": r.name, "description": r.description, "permissions": strings.Join(r.permissionCodes(), ",")}
}

// Campos y especificaciones de Role.
var (
	RoleFieldID          = spec.Comparable("id", func(r *Role) RoleID { return r.ID() })
	RoleFieldName        = spec.Text("name", (*Role).Name)
	RoleFieldNameKey     = spec.Comparable("name_key", (*Role).NameKey)
	RoleFieldPermissions = spec.Collection("permissions", (*Role).Permissions)
	RolePermFieldCode    = spec.Comparable("code", func(p Permission) string { return string(p) })
)

// RolesWithIDs selecciona los roles dados.
func RolesWithIDs(ids ...RoleID) spec.Spec[*Role] { return RoleFieldID.In(ids...) }

// RoleNamed selecciona el rol con ese nombre, sin distinguir mayúsculas.
func RoleNamed(name string) spec.Spec[*Role] {
	return RoleFieldNameKey.Eq(strings.ToLower(cleanText(name)))
}

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
	granted     []Permission // de un rol del sistema: lo que la regla estándar le ha dado alguna vez (D-98)
}

// RoleState es el estado persistido de un rol.
type RoleState struct {
	Name        string
	Description string
	System      bool
	Permissions []Permission
	// Granted son los permisos que la regla estándar ya ha dado a un rol del sistema. Un rol propio
	// no lleva ninguno.
	Granted []Permission
	Audit   traits.AuditStamp
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
	granted, err := normalizePermissions(s.Granted)
	v.Merge("", err)
	v.Require(s.System || len(granted) == 0, "granted", "custom", "only a system role records what the standard rule gave it")
	if err := v.Err(); err != nil {
		return nil, err
	}
	if s.System {
		// Lo que lleva cuenta como concedido aunque falte (restauración parcial, filas a mano).
		granted = union(granted, perms)
	}
	return &Role{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), name: name, description: description,
		system: s.System, permissions: perms, granted: granted}, nil
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

// NewSystemRole construye un rol del sistema con los permisos que le da la regla estándar, que
// quedan como concedidos.
func NewSystemRole(seed SystemRole, c Catalog) (*Role, error) {
	perms := StandardPermissions(seed.ID, c)
	return ReconstituteRole(seed.ID, RoleState{Name: seed.Name, Description: seed.Description, System: true,
		Permissions: perms, Granted: perms})
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

// Granted devuelve, de un rol del sistema, los permisos que la regla estándar le ha dado alguna vez,
// ordenados: los que lleva y los que el administrador global le retiró.
func (r *Role) Granted() []Permission { return slices.Clone(r.granted) }

// Withdrawn devuelve, de un rol del sistema, lo concedido que ya no lleva: lo que el administrador
// global le retiró, ordenado.
func (r *Role) Withdrawn() []Permission {
	var out []Permission
	for _, p := range r.granted {
		if !slices.Contains(r.permissions, p) {
			out = append(out, p)
		}
	}
	return out
}

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
	if err := r.requireCustom("renamed"); err != nil {
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

// SetPermissions sustituye los permisos de un rol. Quien llama los comprueba contra el catálogo c,
// el que está en vigor; un rol propio nunca lleva el comodín. A un rol del sistema el administrador
// global le quita permisos o le devuelve alguno de los que la regla estándar le da, nunca uno que la
// regla no le da (D-98); GlobalSuperAdmin no se edita: sin el comodín no quedaría administrador
// global.
func (r *Role) SetPermissions(perms []Permission, c Catalog) error {
	if r.ID() == RoleGlobalSuperAdmin {
		return fw.Violation("security.system_role", "the GlobalSuperAdmin role keeps the wildcard")
	}
	n, err := ReconstituteRole(r.ID(), RoleState{Name: r.name, System: r.system, Permissions: perms})
	if err != nil {
		return err
	}
	if r.system {
		standard := StandardPermissions(r.ID(), c)
		for _, p := range n.permissions {
			if !slices.Contains(standard, p) {
				return fw.Violation("security.not_standard_permission",
					"the standard rule does not give "+string(p)+" to the "+r.name+" role")
			}
		}
		r.granted = union(r.granted, n.permissions)
	}
	r.replacePermissions(n.permissions)
	return nil
}

// SyncSystemPermissions pone un rol del sistema al paso del catálogo con la regla estándar,
// concediendo una sola vez (D-98): recibe los permisos que la regla le da y nunca le había dado; uno
// que ya le dio y el administrador global le retiró no vuelve; uno que lleva y la regla ya no le da
// (porque el catálogo lo desactivó) se le quita y deja de contar como concedido, así que vuelve si
// el catálogo lo reactiva. Dice si cambió.
func (r *Role) SyncSystemPermissions(c Catalog) bool {
	if !r.system {
		return false
	}
	perms, granted := r.systemSync(c)
	changed := !slices.Equal(granted, r.granted)
	r.granted = granted
	return r.replacePermissions(perms) || changed
}

// SystemSyncPending dice si SyncSystemPermissions cambiaría el rol.
func (r *Role) SystemSyncPending(c Catalog) bool {
	if !r.system {
		return false
	}
	perms, granted := r.systemSync(c)
	return !slices.Equal(perms, r.permissions) || !slices.Equal(granted, r.granted)
}

// systemSync calcula los permisos y lo concedido de un rol del sistema tras una sincronización.
func (r *Role) systemSync(c Catalog) (perms, granted []Permission) {
	standard := StandardPermissions(r.ID(), c)
	for _, p := range r.permissions {
		if slices.Contains(standard, p) {
			perms = append(perms, p)
		}
	}
	for _, p := range r.granted {
		if slices.Contains(standard, p) || !slices.Contains(r.permissions, p) { // en vigor, o retirado
			granted = append(granted, p)
		}
	}
	for _, p := range standard {
		if !slices.Contains(r.granted, p) && !slices.Contains(r.permissions, p) { // nuevo para este rol
			perms = append(perms, p)
			granted = append(granted, p)
		}
	}
	slices.Sort(perms)
	slices.Sort(granted)
	return perms, granted
}

// union devuelve, ordenados y sin duplicados, los permisos de a y de b, sin tocar ninguno de los dos.
func union(a, b []Permission) []Permission {
	out := slices.Concat(a, b)
	slices.Sort(out)
	return slices.Compact(out)
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

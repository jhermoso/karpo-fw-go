package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// RoleKind is the stable aggregate type name.
const RoleKind = "security.role"

// Role is a named set of permissions. One role per installation: the organizations it applies to
// come from the accesses of each user, not from the role (C# decision Q2).
type Role struct {
	fw.BaseAggregateRoot[RoleID]
	traits.Audited
	name        string
	description string
	system      bool
	permissions []Permission // sorted, without duplicates
}

// RoleState is the persisted state of a role.
type RoleState struct {
	Name        string
	Description string
	System      bool
	Permissions []Permission
	Audit       traits.AuditStamp
}

func cleanText(s string) string { return strings.Join(strings.Fields(s), " ") }

// ReconstituteRole rebuilds a role from persisted state.
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

// DefineRole creates a custom role.
func DefineRole(id RoleID, name, description string, perms []Permission) (*Role, error) {
	r, err := ReconstituteRole(id, RoleState{Name: name, Description: description, Permissions: perms})
	if err != nil {
		return nil, err
	}
	r.Raise(RoleDefined{EventMeta: r.NewEventMeta(), Name: r.name, Permissions: r.permissionCodes()})
	return r, nil
}

// NewSystemRole builds a system role with the permissions the standard rule gives it.
func NewSystemRole(seed SystemRole, c Catalog) (*Role, error) {
	return ReconstituteRole(seed.ID, RoleState{Name: seed.Name, Description: seed.Description, System: true,
		Permissions: StandardPermissions(seed.ID, c)})
}

// Name returns the name.
func (r *Role) Name() string { return r.name }

// NameKey returns the name in the form used to keep names unique (case-insensitive).
func (r *Role) NameKey() string { return strings.ToLower(r.name) }

// Description returns the description.
func (r *Role) Description() string { return r.description }

// IsSystem reports whether the role is one of the seeded system roles.
func (r *Role) IsSystem() bool { return r.system }

// Permissions returns the permissions the role carries, sorted.
func (r *Role) Permissions() []Permission { return slices.Clone(r.permissions) }

// IsGlobalAdmin reports whether the role makes its users global administrators.
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

// Describe changes the name and the description of a custom role.
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

// SetPermissions replaces the permissions of a custom role. The caller checks them against the
// catalog; a custom role never carries the wildcard.
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

// SyncSystemPermissions gives a system role the permissions the standard rule computes from the
// catalog. It reports whether they changed.
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

// Retire marks a custom role for removal (the application deletes it once no user holds it).
func (r *Role) Retire() error {
	if err := r.requireCustom("retired"); err != nil {
		return err
	}
	r.Raise(RoleRetired{EventMeta: r.NewEventMeta(), Name: r.name})
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (r *Role) AuditSnapshot() map[string]any {
	return map[string]any{"name": r.name, "description": r.description, "permissions": strings.Join(r.permissionCodes(), ",")}
}

// Role fields and specifications.
var (
	RoleFieldID          = spec.Comparable("id", func(r *Role) RoleID { return r.ID() })
	RoleFieldName        = spec.Text("name", (*Role).Name)
	RoleFieldNameKey     = spec.Comparable("name_key", (*Role).NameKey)
	RoleFieldPermissions = spec.Collection("permissions", (*Role).Permissions)
	RolePermFieldCode    = spec.Comparable("code", func(p Permission) string { return string(p) })
)

// RolesWithIDs matches the given roles.
func RolesWithIDs(ids ...RoleID) spec.Spec[*Role] { return RoleFieldID.In(ids...) }

// RoleNamed matches the role with that name, ignoring case.
func RoleNamed(name string) spec.Spec[*Role] {
	return RoleFieldNameKey.Eq(strings.ToLower(cleanText(name)))
}

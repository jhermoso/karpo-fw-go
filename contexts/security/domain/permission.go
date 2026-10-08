package domain

import (
	"crypto/sha256"
	"slices"
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// Permission is a permission code "{Namespace}.{Resource}.{Action}" (contract v1), for example
// "Parties.Party.Create". The code is the identity of the permission: every bounded context
// declares its own and Security keeps the catalog (decision 4).
type Permission string

// Wildcard grants every permission. Only the GlobalSuperAdmin system role carries it.
const Wildcard Permission = "*.*.*"

// Actions the standard role rule knows (decision 4, the C# P5 rule).
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

// WellFormed reports whether the code has three non-empty segments.
func (p Permission) WellFormed() bool {
	_, ok := p.parts()
	return ok
}

// Namespace returns the first segment (the bounded context).
func (p Permission) Namespace() string { return p.segment(0) }

// Resource returns the second segment.
func (p Permission) Resource() string { return p.segment(1) }

// Action returns the third segment.
func (p Permission) Action() string { return p.segment(2) }

func (p Permission) segment(i int) string {
	if parts, ok := p.parts(); ok {
		return parts[i]
	}
	return ""
}

// normalizePermissions validates, deduplicates and sorts permission codes.
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

// PermissionKind is the stable aggregate type name of a catalog entry.
const PermissionKind = "security.permission"

// PermissionID is the storage identity of a catalog entry. It is derived from the code, so every
// installation gives the same permission the same id; other contexts never see it.
type PermissionID struct{ fw.UUID }

// PermissionIDOf derives the identity of a permission from its code.
func PermissionIDOf(code Permission) PermissionID {
	sum := sha256.Sum256([]byte("karpo.security.permission:" + string(code)))
	var u fw.UUID
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x80 // RFC 9562 version 8 (name-based, custom)
	u[8] = (u[8] & 0x3f) | 0x80
	return PermissionID{u}
}

// PermissionEntry is one entry of the permission catalog. Entries are never deleted: a permission
// nobody declares anymore is deactivated and stops granting (fail closed).
type PermissionEntry struct {
	fw.BaseAggregateRoot[PermissionID]
	traits.Activation
	code        Permission
	description string
}

// DeclarePermission creates the catalog entry of a permission.
func DeclarePermission(code Permission, description string) (*PermissionEntry, error) {
	return ReconstitutePermission(code, description, true)
}

// ReconstitutePermission rebuilds a catalog entry from persisted state.
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

// Code returns the permission code.
func (p *PermissionEntry) Code() Permission { return p.code }

// Description returns the description.
func (p *PermissionEntry) Description() string { return p.description }

// DefaultDescription describes a permission from its segments (as the C# catalog did).
func DefaultDescription(code Permission) string {
	if code == Wildcard {
		return "Full access to all resources and actions"
	}
	return code.Action() + " access to " + code.Namespace() + "." + code.Resource()
}

// Catalog entry fields.
var (
	PermFieldCode   = spec.Comparable("code", func(p *PermissionEntry) string { return string(p.code) })
	PermFieldActive = spec.Comparable("active", (*PermissionEntry).IsActive)
)

// Catalog is the set of permissions in force: the active entries of the catalog.
type Catalog struct{ codes map[Permission]struct{} }

// NewCatalog builds a catalog from codes.
func NewCatalog(codes ...Permission) Catalog {
	c := Catalog{codes: make(map[Permission]struct{}, len(codes))}
	for _, p := range codes {
		c.codes[p] = struct{}{}
	}
	return c
}

// Has reports whether the permission is in force.
func (c Catalog) Has(p Permission) bool {
	_, ok := c.codes[p]
	return ok
}

// Codes returns the permissions in force, sorted.
func (c Catalog) Codes() []Permission {
	out := make([]Permission, 0, len(c.codes))
	for p := range c.codes {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// Check reports the permissions the catalog does not know.
func (c Catalog) Check(perms []Permission) error {
	var v fw.Validation
	for _, p := range perms {
		v.Require(c.Has(p), "permissions", "unknown", "unknown permission "+string(p))
	}
	return v.Err()
}

// StandardPermissions is the rule that gives each system role its permissions (decision 4, the
// rule Javier approved as P5): GlobalSuperAdmin the wildcard; OrganizationAdmin everything but
// what is reserved to the global administrator; StandardUser Read, Create and Update outside
// Security; ReadOnlyUser every Read; Customer nothing. Any other action (Approve, Issue,
// Submit...) reaches OrganizationAdmin or a custom role only.
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

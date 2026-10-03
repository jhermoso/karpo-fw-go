package authz

import (
	"context"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Permission is a permission code "{Subdomain}.{Resource}.{Action}" (e.g. "Parties.Party.Create").
type Permission string

// Wildcard grants every permission. Service principals never receive it.
const Wildcard Permission = "*.*.*"

// PermissionOf builds a permission code.
func PermissionOf(subdomain, resource, action string) Permission {
	return Permission(subdomain + "." + resource + "." + action)
}

// WellFormed reports whether the code has three non-empty segments.
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

// MustPermission validates a code at composition time (panics when malformed), like the C#
// RequirePermission refusing malformed policy names.
func MustPermission(code string) Permission {
	p := Permission(code)
	if !p.WellFormed() {
		panic(fmt.Sprintf("authz: malformed permission %q, expected {Subdomain}.{Resource}.{Action}", code))
	}
	return p
}

// Require checks that ctx carries an authorization context granting p. It returns an error
// matching domain.ErrUnauthorized without a context (no Legacy fallback) and
// domain.ErrForbidden without the permission.
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

// RequireWrite checks that the subject in ctx may write data owned by org (Full grant inside the
// scope, or global admin). The generic message never reveals whether the organization exists.
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

// ServicePrincipal is a configured technical caller and its permissions (contract v1, section 6).
type ServicePrincipal struct {
	Subject     domain.UUID
	Name        string
	Permissions []Permission
}

// ServicePrincipalContext builds the context of a configured service principal: no grants, no
// organization scope, never global admin, and the wildcard is dropped even if configured.
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

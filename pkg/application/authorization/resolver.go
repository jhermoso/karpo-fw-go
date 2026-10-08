// Package authorization implements the authz contracts: the generic resolver of the
// "contrato de autorización v1" (Go port of ErpKernel SecurityAuthorizationContextResolver)
// over an authz.Directory, and an in-memory directory for tests and local development.
package authorization

import (
	"context"
	"slices"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// DefaultGlobalAdminRole is the role that makes a subject global administrator.
const DefaultGlobalAdminRole = "GlobalSuperAdmin"

// Options configure a Resolver.
type Options struct {
	TenantID          string                   // default "erp-security" (the JWT issuer)
	GlobalAdminRole   string                   // default DefaultGlobalAdminRole
	ServicePrincipals []authz.ServicePrincipal // technical callers (never global admin, no wildcard)
}

// Resolver resolves authorization contexts from a Directory. It resolves once per request and
// keeps no cache between requests (contract v1, D7).
type Resolver struct {
	dir  authz.Directory
	opts Options
}

var _ authz.Resolver = (*Resolver)(nil)

// NewResolver builds a Resolver.
func NewResolver(dir authz.Directory, opts Options) *Resolver {
	if opts.TenantID == "" {
		opts.TenantID = "erp-security"
	}
	if opts.GlobalAdminRole == "" {
		opts.GlobalAdminRole = DefaultGlobalAdminRole
	}
	return &Resolver{dir: dir, opts: opts}
}

// Resolve implements authz.Resolver: incomplete principal -> Deny "incomplete-principal";
// service principals from configuration; unknown/inactive/locked users -> Deny; directory
// failure -> Indeterminate (fail closed).
func (r *Resolver) Resolve(ctx context.Context, p authz.Principal, req authz.Request) authz.Resolution {
	if !p.Complete() {
		return authz.Denied(authz.ReasonIncompletePrincipal)
	}
	if p.Kind == authz.Service {
		return r.service(p, req)
	}
	s, found, err := r.dir.Subject(ctx, p.Subject)
	switch {
	case err != nil:
		return authz.Undetermined("repository-failure")
	case !found:
		return authz.Denied(authz.ReasonUnknownSubject)
	case !s.Active:
		return authz.Denied(authz.ReasonInactiveUser)
	case s.Locked:
		return authz.Denied(authz.ReasonLockedUser)
	}
	c, err := authz.NewContext(authz.Context{
		TenantID: r.opts.TenantID, Subject: p.Subject, SubjectName: p.Name, Kind: authz.Human,
		ActorPartyID: p.PartyID,
		GlobalAdmin:  slices.Contains(s.Roles, r.opts.GlobalAdminRole) || slices.Contains(s.Permissions, authz.Wildcard),
		Grants:       s.Grants, Permissions: s.Permissions,
		RequestedOrganizations: req.RequestedOrganizations,
		PolicyVersion:          authz.PolicyVersion(s.Active, s.Roles, s.Permissions, s.Grants),
		CorrelationID:          req.CorrelationID,
	})
	if err != nil {
		return authz.Undetermined("invalid-snapshot")
	}
	return authz.Allowed(c)
}

func (r *Resolver) service(p authz.Principal, req authz.Request) authz.Resolution {
	for _, sp := range r.opts.ServicePrincipals {
		if sp.Subject != p.Subject {
			continue
		}
		if sp.Name == "" {
			sp.Name = p.Name
		}
		c, err := authz.ServicePrincipalContext(r.opts.TenantID, sp, req)
		if err != nil {
			return authz.Undetermined("invalid-service-principal")
		}
		return authz.Allowed(c)
	}
	return authz.Denied(authz.ReasonUnknownService)
}

// MemoryDirectory is an in-memory authz.Directory for tests and local development.
type MemoryDirectory struct {
	mu       sync.RWMutex
	subjects map[domain.UUID]authz.Subject
	fail     error
}

var _ authz.Directory = (*MemoryDirectory)(nil)

// NewMemoryDirectory builds an empty directory.
func NewMemoryDirectory() *MemoryDirectory {
	return &MemoryDirectory{subjects: map[domain.UUID]authz.Subject{}}
}

// Put registers or replaces a subject.
func (d *MemoryDirectory) Put(id domain.UUID, s authz.Subject) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subjects[id] = s
}

// FailWith makes every lookup fail (nil restores it), to exercise Indeterminate.
func (d *MemoryDirectory) FailWith(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fail = err
}

// Subject implements authz.Directory.
func (d *MemoryDirectory) Subject(_ context.Context, id domain.UUID) (authz.Subject, bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.fail != nil {
		return authz.Subject{}, false, d.fail
	}
	s, ok := d.subjects[id]
	return s, ok, nil
}

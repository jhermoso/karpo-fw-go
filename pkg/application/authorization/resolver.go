// Package authorization implementa los contratos de authz: el resolutor genérico del
// «contrato de autorización v1» (port a Go de SecurityAuthorizationContextResolver de ErpKernel)
// sobre un authz.Directory, y un directorio en memoria para pruebas y desarrollo local.
package authorization

import (
	"context"
	"slices"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// DefaultGlobalAdminRole es el rol que hace de un sujeto administrador global.
const DefaultGlobalAdminRole = "GlobalSuperAdmin"

// Options configuran un Resolver.
type Options struct {
	TenantID          string                   // por defecto "erp-security" (el emisor del JWT)
	GlobalAdminRole   string                   // por defecto DefaultGlobalAdminRole
	ServicePrincipals []authz.ServicePrincipal // llamantes técnicos (nunca administrador global, sin comodín)
}

// Resolver resuelve contextos de autorización a partir de un Directory. Resuelve una vez por
// petición y no guarda caché entre peticiones (contrato v1, D7).
type Resolver struct {
	dir  authz.Directory
	opts Options
}

var _ authz.Resolver = (*Resolver)(nil)

// NewResolver construye un Resolver.
func NewResolver(dir authz.Directory, opts Options) *Resolver {
	if opts.TenantID == "" {
		opts.TenantID = "erp-security"
	}
	if opts.GlobalAdminRole == "" {
		opts.GlobalAdminRole = DefaultGlobalAdminRole
	}
	return &Resolver{dir: dir, opts: opts}
}

// Resolve implementa authz.Resolver: principal incompleto -> Deny "incomplete-principal";
// principales de servicio desde la configuración; usuarios desconocidos/inactivos/bloqueados -> Deny;
// fallo del directorio -> Indeterminate (falla cerrado).
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

// MemoryDirectory es un authz.Directory en memoria para pruebas y desarrollo local.
type MemoryDirectory struct {
	mu       sync.RWMutex
	subjects map[domain.UUID]authz.Subject
	fail     error
}

var _ authz.Directory = (*MemoryDirectory)(nil)

// NewMemoryDirectory construye un directorio vacío.
func NewMemoryDirectory() *MemoryDirectory {
	return &MemoryDirectory{subjects: map[domain.UUID]authz.Subject{}}
}

// Put registra o sustituye un sujeto.
func (d *MemoryDirectory) Put(id domain.UUID, s authz.Subject) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subjects[id] = s
}

// FailWith hace fallar todas las búsquedas (nil lo restablece), para ejercitar Indeterminate.
func (d *MemoryDirectory) FailWith(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fail = err
}

// Subject implementa authz.Directory.
func (d *MemoryDirectory) Subject(_ context.Context, id domain.UUID) (authz.Subject, bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.fail != nil {
		return authz.Subject{}, false, d.fail
	}
	s, ok := d.subjects[id]
	return s, ok, nil
}

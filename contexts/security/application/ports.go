// Package application holds the Security use cases: signing in with a password and keeping the
// session, administering users, roles and organization accesses under the rules of delegated
// administration, the permission catalog, and the ports Security offers to the rest of the
// system (authz.Directory, the external identity authenticator, the user directory).
package application

import (
	"context"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Permissions of the Security use cases.
var (
	PermUserRead     = authz.MustPermission(string(domain.PermUserRead))
	PermUserCreate   = authz.MustPermission(string(domain.PermUserCreate))
	PermUserUpdate   = authz.MustPermission(string(domain.PermUserUpdate))
	PermUserRole     = authz.MustPermission(string(domain.PermUserRole))
	PermAccessRead   = authz.MustPermission(string(domain.PermAccessRead))
	PermAccessUpdate = authz.MustPermission(string(domain.PermAccessUpdate))
	PermRoleRead     = authz.MustPermission(string(domain.PermRoleRead))
	PermRoleUpdate   = authz.MustPermission(string(domain.PermRoleUpdate))
)

// Permissions returns the permissions this context declares to the catalog.
func Permissions() []authz.Permission {
	return []authz.Permission{PermUserRead, PermUserCreate, PermUserUpdate, PermUserRole, PermAccessRead, PermAccessUpdate,
		PermRoleRead, PermRoleUpdate}
}

// PasswordHasher hashes and verifies passwords. The hash is self-describing (algorithm and cost
// inside), so the cost can be raised without a migration.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Verify reports whether the password matches the hash, and whether the hash is weaker than
	// the current settings and should be recomputed.
	Verify(password, hash string) (ok, stale bool)
}

// TokenIssuer issues the access token of the own login. Security does not know its format: the
// composition root adapts the issuer of the host (today, the shared-secret JWT).
type TokenIssuer interface {
	IssueAccessToken(ctx context.Context, subject fw.UUID, username string, party fw.UUID, expiresAt time.Time) (string, error)
}

// PartyDirectory tells whether a party exists (adapter over the Parties directory).
type PartyDirectory interface {
	PartyExists(ctx context.Context, party fw.UUID) (bool, error)
}

// InternalOrganizations tells whether a party is an internal organization, the only kind of
// organization an access can be granted to (adapter over the Parties catalog).
type InternalOrganizations interface {
	IsInternalOrganization(ctx context.Context, organization fw.UUID) (bool, error)
}

// Deps are the ports the use cases need. Recorder, Audit, Parties and Organizations are optional;
// zero durations and lockout take the defaults.
type Deps struct {
	Users         domain.UserRepository
	Roles         domain.RoleRepository
	Permissions   domain.PermissionRepository
	Sessions      domain.SessionRepository
	UoW           fw.UnitOfWork
	Recorder      app.EventRecorder
	Audit         app.AuditLog
	Hasher        PasswordHasher
	Tokens        TokenIssuer
	Parties       PartyDirectory
	Organizations InternalOrganizations
	Lockout       domain.LockoutPolicy
	AccessTTL     time.Duration // life of an access token (default 15 minutes)
	SessionTTL    time.Duration // life of a refresh token (default 7 days)
}

// Defaults of the own login.
const (
	DefaultAccessTTL  = 15 * time.Minute
	DefaultSessionTTL = 7 * 24 * time.Hour
)

func (d Deps) withDefaults() Deps {
	if d.AccessTTL <= 0 {
		d.AccessTTL = DefaultAccessTTL
	}
	if d.SessionTTL <= 0 {
		d.SessionTTL = DefaultSessionTTL
	}
	if d.Lockout.MaxAttempts == 0 {
		d.Lockout = domain.DefaultLockout
	}
	return d
}

// administratorOf builds the domain Administrator of the request from its authorization context.
func administratorOf(ctx context.Context) (domain.Administrator, error) {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return domain.Administrator{}, fmt.Errorf("%w: no authorization context", fw.ErrUnauthorized)
	}
	a := domain.Administrator{User: domain.UserID{UUID: ac.Subject}, Global: ac.GlobalAdmin}
	for _, p := range ac.Permissions {
		a.Permissions = append(a.Permissions, domain.Permission(p))
	}
	for _, org := range ac.EffectiveOrganizations {
		a.Organizations = append(a.Organizations, domain.OrganizationID{UUID: org})
		if ac.CanWrite(org) {
			a.Full = append(a.Full, domain.OrganizationID{UUID: org})
		}
	}
	return a, nil
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func invalid(field, code, message string) error {
	var v fw.Validation
	v.Add(field, code, message)
	return v.Err()
}

func first[T any](xs []T, err error) (T, bool, error) {
	var zero T
	if err != nil || len(xs) == 0 {
		return zero, false, err
	}
	return xs[0], true, nil
}

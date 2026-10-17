// Package application contiene los casos de uso de Security: iniciar sesión con contraseña y
// mantener la sesión, administrar usuarios, roles y accesos a organizaciones bajo las reglas de la
// administración delegada, el catálogo de permisos, y los puertos que Security ofrece al resto del
// sistema (authz.Directory, el autenticador de identidades externas, el directorio de usuarios).
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

// Permisos de los casos de uso de Security.
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

// Permissions devuelve los permisos que este contexto declara al catálogo.
func Permissions() []authz.Permission {
	return []authz.Permission{PermUserRead, PermUserCreate, PermUserUpdate, PermUserRole, PermAccessRead, PermAccessUpdate,
		PermRoleRead, PermRoleUpdate}
}

// PasswordHasher calcula el hash de las contraseñas y las verifica. El hash es autodescriptivo
// (lleva dentro el algoritmo y el coste), así que el coste se puede subir sin una migración.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Verify dice si la contraseña coincide con el hash, y si el hash es más débil que la
	// configuración actual y debería recalcularse.
	Verify(password, hash string) (ok, stale bool)
}

// TokenIssuer emite el token de acceso del login propio. Security no conoce su formato: la raíz de
// composición adapta el emisor del anfitrión (hoy, el JWT de secreto compartido).
type TokenIssuer interface {
	IssueAccessToken(ctx context.Context, subject fw.UUID, username string, party fw.UUID, expiresAt time.Time) (string, error)
}

// PartyDirectory dice si existe una party (adaptador sobre el directorio de Parties).
type PartyDirectory interface {
	PartyExists(ctx context.Context, party fw.UUID) (bool, error)
}

// InternalOrganizations dice si una party es una organización interna, la única clase de
// organización a la que se puede conceder un acceso (adaptador sobre el catálogo de Parties).
type InternalOrganizations interface {
	IsInternalOrganization(ctx context.Context, organization fw.UUID) (bool, error)
}

// Deps son los puertos que necesitan los casos de uso. Recorder, Audit, Parties y Organizations son
// opcionales; las duraciones y el bloqueo a cero toman los valores por defecto.
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
	AccessTTL     time.Duration // vida de un token de acceso (por defecto 15 minutos)
	SessionTTL    time.Duration // vida de un token de refresco (por defecto 7 días)
}

// Valores por defecto del login propio.
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

// administratorOf construye el Administrator de dominio de la petición a partir de su contexto de autorización.
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

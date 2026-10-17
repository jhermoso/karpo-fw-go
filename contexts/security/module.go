// Package security compone el contexto delimitado Security sobre un backend intercambiable en caliente: los casos
// de uso, el módulo HTTP, el authz.Directory real que lee el resolvedor de autorización, el
// autenticador de identidades externas, el directorio de usuarios y el relay de su Published
// Language.
//
// Un anfitrión monta HTTP.RegisterPublicRoutes fuera de distribution.Authorize y HTTP.RegisterRoutes
// dentro, construye su resolvedor con authorization.NewResolver(module.Directory, ...), y al
// arrancar, tras migrar, llama a SyncCatalog con los permisos de cada contexto y a
// BootstrapFromEnv.
package security

import (
	"context"
	"errors"
	"os"
	"time"

	sapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	"github.com/jhermoso/karpo-fw-go/contexts/security/contracts"
	sdist "github.com/jhermoso/karpo-fw-go/contexts/security/distribution"
	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/outbox"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// Variables de entorno del administrador de arranque (los mismos nombres que el sembrador de C#).
// Se leen una vez al arrancar y nunca se guardan: hay que quitarlas tras el primer inicio de sesión.
const (
	EnvBootstrapUser     = "KARPO_BOOTSTRAP_ADMIN_USER"
	EnvBootstrapPassword = "KARPO_BOOTSTRAP_ADMIN_PASSWORD"
)

// Module es el contexto compuesto.
type Module struct {
	Service *sapp.Service
	// Directory es el authz.Directory respaldado por la base de datos: se pasa a authorization.NewResolver.
	Directory authz.Directory
	// Users es el directorio de usuarios para otros contextos.
	Users             contracts.Users
	HTTP              *sdist.Module
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog

	users domain.UserRepository
}

// Option configura la composición.
type Option func(*sapp.Deps)

// WithTokenIssuer fija quién emite el token de acceso del login propio (necesario para iniciar
// sesión con contraseña; un anfitrión que sólo acepta identidades externas no lo necesita).
func WithTokenIssuer(t sapp.TokenIssuer) Option { return func(d *sapp.Deps) { d.Tokens = t } }

// WithPasswordHasher sustituye el hasher por defecto (PBKDF2-HMAC-SHA256, 600 000 iteraciones).
func WithPasswordHasher(h sapp.PasswordHasher) Option { return func(d *sapp.Deps) { d.Hasher = h } }

// WithParties valida con Parties la party de un usuario y la organización de un acceso.
func WithParties(p infrastructure.PartiesDirectory) Option {
	return func(d *sapp.Deps) { d.Parties, d.Organizations = p, p }
}

// WithLockout fija la política de bloqueo (por defecto: 5 intentos fallidos, 15 minutos).
func WithLockout(p domain.LockoutPolicy) Option { return func(d *sapp.Deps) { d.Lockout = p } }

// WithLifetimes fija la vida del token de acceso y de la sesión de refresco.
func WithLifetimes(access, session time.Duration) Option {
	return func(d *sapp.Deps) { d.AccessTTL, d.SessionTTL = access, session }
}

type noIssuer struct{}

func (noIssuer) IssueAccessToken(context.Context, fw.UUID, string, fw.UUID, time.Time) (string, error) {
	return "", errors.New("security: no token issuer configured (security.WithTokenIssuer)")
}

// Compose construye el contexto sobre sw.
func Compose(sw *hotswap.Switch, opts ...Option) *Module {
	users := hotswap.Repository(sw, infrastructure.UserRepositoryFactory)
	roles := hotswap.Repository(sw, infrastructure.RoleRepositoryFactory)
	permissions := hotswap.Repository(sw, infrastructure.PermissionRepositoryFactory)
	integration := hotswap.Outbox(sw, infrastructure.IntegrationOutboxFactory)
	audit := hotswap.AuditLog(sw, infrastructure.AuditLogFactory)
	deps := sapp.Deps{
		Users: users, Roles: roles, Permissions: permissions,
		Sessions: hotswap.Repository(sw, infrastructure.SessionRepositoryFactory),
		UoW:      sw, Audit: audit, Hasher: infrastructure.NewPBKDF2(0), Tokens: noIssuer{},
		Recorder: outbox.Recorders(outbox.NewRecorder(hotswap.Outbox(sw, infrastructure.OutboxFactory)),
			sapp.Publications(messaging.NewRecorder(contracts.Source, integration))),
	}
	for _, o := range opts {
		o(&deps)
	}
	svc := sapp.NewService(deps)
	return &Module{Service: svc, Directory: sapp.Directory{Users: users, Roles: roles, Permissions: permissions},
		Users: sapp.Users{Repository: users}, HTTP: sdist.NewModule(svc), IntegrationOutbox: integration, Audit: audit, users: users}
}

// Authenticator devuelve el authz.Authenticator de un proveedor de identidad externo: verifier
// valida sus tokens y Security asocia la identidad afirmada con el usuario vinculado.
func (m *Module) Authenticator(verifier contracts.TokenVerifier) authz.Authenticator {
	return sapp.Authenticator{Verifier: verifier, Users: m.users}
}

// SyncCatalog declara los permisos de los contextos delimitados del anfitrión (ver Catalog.Sync).
func (m *Module) SyncCatalog(ctx context.Context, declared ...authz.Permission) (sapp.CatalogSync, error) {
	return m.Service.Catalog.Sync(ctx, declared...)
}

// BootstrapFromEnv crea el administrador de arranque a partir del entorno (ver Bootstrap.Run).
func (m *Module) BootstrapFromEnv(ctx context.Context) (string, error) {
	return m.Service.Bootstrap.Run(ctx, os.Getenv(EnvBootstrapUser), os.Getenv(EnvBootstrapPassword))
}

// Relay reenvía el Published Language a un transporte (al menos una vez).
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}

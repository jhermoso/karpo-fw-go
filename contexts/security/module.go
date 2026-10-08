// Package security composes the Security bounded context on a hot-swap backend: the use cases,
// the HTTP module, the real authz.Directory the authorization resolver reads, the authenticator
// of external identities, the user directory and the relay of its Published Language.
//
// A host mounts HTTP.RegisterPublicRoutes outside distribution.Authorize and HTTP.RegisterRoutes
// inside it, builds its resolver with authorization.NewResolver(module.Directory, ...), and on
// start, after migrating, calls SyncCatalog with the permissions of every context and
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

// Environment variables of the bootstrap administrator (the same names as the C# seeder). They
// are read once on start and never stored: remove them after the first sign-in.
const (
	EnvBootstrapUser     = "KARPO_BOOTSTRAP_ADMIN_USER"
	EnvBootstrapPassword = "KARPO_BOOTSTRAP_ADMIN_PASSWORD"
)

// Module is the composed context.
type Module struct {
	Service *sapp.Service
	// Directory is the authz.Directory backed by the database: pass it to authorization.NewResolver.
	Directory authz.Directory
	// Users is the user directory for other contexts.
	Users             contracts.Users
	HTTP              *sdist.Module
	IntegrationOutbox application.OutboxStore
	Audit             application.AuditLog

	users domain.UserRepository
}

// Option configures the composition.
type Option func(*sapp.Deps)

// WithTokenIssuer sets who issues the access token of the own login (required to sign in with a
// password; a host that only accepts external identities does not need it).
func WithTokenIssuer(t sapp.TokenIssuer) Option { return func(d *sapp.Deps) { d.Tokens = t } }

// WithPasswordHasher replaces the default hasher (PBKDF2-HMAC-SHA256, 600 000 iterations).
func WithPasswordHasher(h sapp.PasswordHasher) Option { return func(d *sapp.Deps) { d.Hasher = h } }

// WithParties validates the party of a user and the organization of an access with Parties.
func WithParties(p infrastructure.PartiesDirectory) Option {
	return func(d *sapp.Deps) { d.Parties, d.Organizations = p, p }
}

// WithLockout sets the lockout policy (default: 5 failed attempts, 15 minutes).
func WithLockout(p domain.LockoutPolicy) Option { return func(d *sapp.Deps) { d.Lockout = p } }

// WithLifetimes sets the life of the access token and of the refresh session.
func WithLifetimes(access, session time.Duration) Option {
	return func(d *sapp.Deps) { d.AccessTTL, d.SessionTTL = access, session }
}

type noIssuer struct{}

func (noIssuer) IssueAccessToken(context.Context, fw.UUID, string, fw.UUID, time.Time) (string, error) {
	return "", errors.New("security: no token issuer configured (security.WithTokenIssuer)")
}

// Compose builds the context on sw.
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

// Authenticator returns the authz.Authenticator of an external identity provider: verifier
// validates its tokens and Security maps the asserted identity to the linked user.
func (m *Module) Authenticator(verifier contracts.TokenVerifier) authz.Authenticator {
	return sapp.Authenticator{Verifier: verifier, Users: m.users}
}

// SyncCatalog declares the permissions of the bounded contexts of the host (see Catalog.Sync).
func (m *Module) SyncCatalog(ctx context.Context, declared ...authz.Permission) (sapp.CatalogSync, error) {
	return m.Service.Catalog.Sync(ctx, declared...)
}

// BootstrapFromEnv creates the bootstrap administrator from the environment (see Bootstrap.Run).
func (m *Module) BootstrapFromEnv(ctx context.Context) (string, error) {
	return m.Service.Bootstrap.Run(ctx, os.Getenv(EnvBootstrapUser), os.Getenv(EnvBootstrapPassword))
}

// Relay forwards the Published Language to a transport (at-least-once).
func (m *Module) Relay(sender application.MessageSender, opts ...outbox.RelayOption) *outbox.Relay {
	return messaging.NewRelay(contracts.Source, m.IntegrationOutbox, sender, opts...)
}

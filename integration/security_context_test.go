package integration

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/security"
	sapp "github.com/jhermoso/karpo-fw-go/contexts/security/application"
	scontracts "github.com/jhermoso/karpo-fw-go/contexts/security/contracts"
	sdist "github.com/jhermoso/karpo-fw-go/contexts/security/distribution"
	sdomain "github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	sinfra "github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure/securityconformance"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authorization"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/distribution/jwtauth"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestSecurityConformance ejecuta la batería de mapeo de Security en cada motor: la semilla con los
// GUID de C#, cada especificación en SQL frente a su evaluación en memoria, idas y vueltas, hijos
// reemplazados con el agregado y los índices únicos en los que se apoya el dominio.
func TestSecurityConformance(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			sinfra.DropAll(ctx, db)
			m, err := sinfra.Migrator(db)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
			securityconformance.Run(t, db)
		})
	}
}

// TestSecurityContext ejecuta Security con Parties en cada motor (una base de datos, dos historias
// de migraciones): la sincronización del catálogo, el administrador de arranque, el login, las
// sesiones con detección de reutilización, la administración delegada, y Parties autorizado a
// través del authz.Directory real leído de la base de datos en cada resolución.
func TestSecurityContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			sinfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), sinfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}

			sw := hotswap.New(db)
			jwt, _ := jwtauth.New(jwtauth.Config{Secret: []byte("integration")})
			pm := parties.Compose(sw, nil)
			sec := security.Compose(sw, security.WithTokenIssuer(sdist.HS256Issuer{JWT: jwt}), security.WithPasswordHasher(sinfra.NewPBKDF2(1000)),
				security.WithParties(sinfra.PartiesDirectory{Directory: pm.Directory, Organizations: pm.Organizations}),
				security.WithLockout(sdomain.LockoutPolicy{MaxAttempts: 2, Duration: time.Hour}))
			resolver := authorization.NewResolver(sec.Directory, authorization.Options{})
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			// as resuelve a quien llama exactamente como lo hace distribution.Authorize: token -> principal ->
			// contexto leído de la base de datos, y el sujeto resuelto como actor de la auditoría.
			as := func(tok sapp.Tokens) context.Context {
				t.Helper()
				p, err := jwt.Authenticate(ctx, tok.AccessToken)
				must(err)
				res := resolver.Resolve(ctx, p, authz.Request{})
				if res.Outcome != authz.Allow {
					t.Fatalf("resolution: %+v", res)
				}
				return application.WithActor(authz.WithContext(ctx, res.Context), res.Context.Actor())
			}

			report, err := sec.SyncCatalog(ctx, papp.Permissions()...)
			if err != nil || report.Added != len(papp.Permissions()) || report.RolesUpdated != 3 { // OrganizationAdmin, StandardUser, ReadOnlyUser
				t.Fatalf("catalog: %+v %v", report, err)
			}
			if again, err := sec.SyncCatalog(ctx, papp.Permissions()...); err != nil || again != (sapp.CatalogSync{}) {
				t.Fatalf("idempotent: %+v %v", again, err)
			}
			out, err := sec.Service.Bootstrap.Run(ctx, "root", "bootstrap-password-1")
			if err != nil || out != sapp.BootstrapCreated {
				t.Fatalf("bootstrap: %q %v", out, err)
			}

			// El administrador de arranque cambia su contraseña antes que nada.
			boot, err := sec.Service.Login.Handle(ctx, sapp.Login{Username: "Root", Password: "bootstrap-password-1"})
			must(err)
			if _, err := sec.Service.SearchUsers.Handle(as(boot), sapp.SearchUsers{}); !errors.Is(err, fw.ErrForbidden) {
				t.Fatalf("must change the password first: %v", err)
			}
			rootTok, err := sec.Service.ChangePassword.Handle(as(boot), sapp.ChangePassword{CurrentPassword: "bootstrap-password-1", NewPassword: "root-password-0001"})
			must(err)
			root := as(rootTok)

			// Parties a través del directorio real.
			acme, err := pm.Service.RegisterOrganization.Handle(root, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			beta, err := pm.Service.RegisterOrganization.Handle(root, papp.RegisterOrganization{LegalName: "Beta", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			person := func(name string) string {
				p, err := pm.Service.RegisterPerson.Handle(root, papp.RegisterPerson{GivenName: name, FirstSurname: "Test"})
				must(err)
				return p.ID
			}
			register := func(admin context.Context, name, org, level string) sapp.UserDTO {
				t.Helper()
				u, err := sec.Service.RegisterUser.Handle(admin, sapp.RegisterUser{Username: name, Party: person(name), Password: name + "-initial-pass",
					Access: &sapp.AccessInput{Organization: org, Level: level}})
				must(err)
				return u
			}
			signIn := func(name string) sapp.Tokens {
				t.Helper()
				tok, err := sec.Service.Login.Handle(ctx, sapp.Login{Username: name, Password: name + "-initial-pass"})
				must(err)
				tok, err = sec.Service.ChangePassword.Handle(as(tok), sapp.ChangePassword{CurrentPassword: name + "-initial-pass", NewPassword: name + "-password-0001"})
				must(err)
				return tok
			}
			uid := func(u sapp.UserDTO) sdomain.UserID { id, _ := sdomain.ParseUserID(u.ID); return id }
			org := func(id string) sdomain.OrganizationID { o, _ := sdomain.ParseOrganizationID(id); return o }

			martaUser := register(root, "marta", acme.ID, "Full")
			_, err = sec.Service.AssignRole.Handle(root, sapp.AssignRole{ID: uid(martaUser), Role: sdomain.RoleOrganizationAdmin})
			must(err)
			pedroUser := register(root, "pedro", beta.ID, "Full")
			if _, err := sec.Service.RegisterUser.Handle(root, sapp.RegisterUser{Username: "MARTA", Party: person("Other"),
				Access: &sapp.AccessInput{Organization: acme.ID, Level: "Full"}}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("user names are unique ignoring case: %v", err)
			}

			marta := as(signIn("marta"))
			mc, _ := authz.FromContext(marta)
			if mc.GlobalAdmin || !mc.HasPermission(papp.PermPartyCreate) || mc.HasPermission(sapp.PermRoleUpdate) ||
				!mc.CanWrite(fw.MustParseUUID(acme.ID)) || len(mc.Grants) != 1 {
				t.Fatalf("context of marta from the database: %+v", mc)
			}
			if _, err := pm.Service.RegisterPerson.Handle(marta, papp.RegisterPerson{GivenName: "Cliente", FirstSurname: "Uno",
				Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: pdomain.RelCustomer.String()}}); err != nil {
				t.Fatalf("marta writes in acme: %v", err)
			}
			if _, err := pm.Service.RegisterPerson.Handle(marta, papp.RegisterPerson{GivenName: "Cliente", FirstSurname: "Dos",
				Affiliation: &papp.NewAffiliation{Organization: beta.ID, RelationshipType: pdomain.RelCustomer.String()}}); !errors.Is(err, fw.ErrNotFound) {
				t.Fatalf("beta is out of marta's scope: %v", err)
			}

			// Administración delegada, con las especificaciones traducidas a SQL.
			carlosUser := register(marta, "carlos", acme.ID, "ReadOnly")
			_, err = sec.Service.AssignRole.Handle(marta, sapp.AssignRole{ID: uid(carlosUser), Role: sdomain.RoleStandardUser})
			must(err)
			if _, err := sec.Service.AssignRole.Handle(marta, sapp.AssignRole{ID: uid(carlosUser), Role: sdomain.RoleGlobalSuperAdmin}); !errors.Is(err, fw.ErrForbidden) {
				t.Fatalf("nobody grants what they do not have: %v", err)
			}
			if _, err := sec.Service.GetUser.Handle(marta, sapp.GetUser{ID: uid(pedroUser)}); !errors.Is(err, fw.ErrNotFound) {
				t.Fatalf("pedro is out of sight: %v", err)
			}
			_, err = sec.Service.GrantAccess.Handle(marta, sapp.GrantAccess{ID: uid(pedroUser), Organization: org(acme.ID), Level: "ReadOnly"})
			must(err)
			if _, err := sec.Service.SetUserActive.Handle(marta, sapp.SetUserActive{ID: uid(pedroUser)}); !errors.Is(err, fw.ErrForbidden) {
				t.Fatalf("marta does not encompass beta: %v", err)
			}
			visible, err := sec.Service.SearchUsers.Handle(marta, sapp.SearchUsers{})
			must(err)
			mine, err := sec.Service.SearchUsers.Handle(marta, sapp.SearchUsers{Administrable: true})
			must(err)
			names := func(p fw.Page[sapp.UserDTO]) (out []string) {
				for _, u := range p.Items {
					out = append(out, u.Username)
				}
				return out
			}
			if !slices.Equal(names(visible), []string{"carlos", "marta", "pedro"}) || !slices.Equal(names(mine), []string{"carlos", "marta"}) {
				t.Fatalf("visible %v, administrable %v", names(visible), names(mine))
			}
			if _, err := sec.Service.SetUserActive.Handle(marta, sapp.SetUserActive{ID: uid(martaUser)}); !errors.Is(err, fw.ErrForbidden) {
				t.Fatalf("nobody administers themselves: %v", err)
			}

			// Revocación en la siguiente resolución con el mismo token; sesiones con detección de reutilización.
			carlosTok := signIn("carlos")
			if _, err := pm.Service.Search.Handle(as(carlosTok), papp.SearchParties{}); err != nil {
				t.Fatalf("a standard user reads parties: %v", err)
			}
			_, err = sec.Service.RevokeRole.Handle(root, sapp.RevokeRole{ID: uid(carlosUser), Role: sdomain.RoleStandardUser})
			must(err)
			if _, err := pm.Service.Search.Handle(as(carlosTok), papp.SearchParties{}); !errors.Is(err, fw.ErrForbidden) {
				t.Fatalf("the same token no longer reads: %v", err)
			}
			second, err := sec.Service.Refresh.Handle(ctx, sapp.Refresh{RefreshToken: carlosTok.RefreshToken})
			must(err)
			if _, err := sec.Service.Refresh.Handle(ctx, sapp.Refresh{RefreshToken: carlosTok.RefreshToken}); !errors.Is(err, fw.ErrUnauthorized) {
				t.Fatalf("a rotated token again: %v", err)
			}
			if _, err := sec.Service.Refresh.Handle(ctx, sapp.Refresh{RefreshToken: second.RefreshToken}); !errors.Is(err, fw.ErrUnauthorized) {
				t.Fatalf("the family ended: %v", err)
			}

			// Bloqueo con marcas de tiempo guardadas por el motor, y desactivación.
			for range 2 {
				if _, err := sec.Service.Login.Handle(ctx, sapp.Login{Username: "carlos", Password: "wrong-password-000"}); !errors.Is(err, fw.ErrUnauthorized) {
					t.Fatalf("wrong password: %v", err)
				}
			}
			if _, err := sec.Service.Login.Handle(ctx, sapp.Login{Username: "carlos", Password: "carlos-password-0001"}); !errors.Is(err, fw.ErrUnauthorized) {
				t.Fatalf("locked: %v", err)
			}
			if res := resolver.Resolve(ctx, authz.Principal{Subject: uid(carlosUser).UUID, Name: "carlos", PartyID: fw.NewUUID(), Kind: authz.Human}, authz.Request{}); res.Reason != authz.ReasonLockedUser {
				t.Fatalf("a locked user is denied: %+v", res)
			}
			unlocked, err := sec.Service.UnlockUser.Handle(marta, sapp.UnlockUser{ID: uid(carlosUser)})
			if err != nil || unlocked.Locked {
				t.Fatalf("unlock: %+v %v", unlocked, err)
			}
			if _, err := sec.Service.Login.Handle(ctx, sapp.Login{Username: "carlos", Password: "carlos-password-0001"}); err != nil {
				t.Fatalf("login after unlock: %v", err)
			}
			if _, err := sec.Service.SetUserActive.Handle(root, sapp.SetUserActive{ID: uid(carlosUser)}); err != nil {
				t.Fatal(err)
			}
			if res := resolver.Resolve(ctx, authz.Principal{Subject: uid(carlosUser).UUID, Name: "carlos", PartyID: fw.NewUUID(), Kind: authz.Human}, authz.Request{}); res.Reason != authz.ReasonInactiveUser {
				t.Fatalf("a deactivated user is denied: %+v", res)
			}

			// Identidad externa, roles y auditoría.
			_, err = sec.Service.LinkIdentity.Handle(root, sapp.LinkIdentity{ID: uid(pedroUser), Issuer: "https://id.example", Subject: "sub-pedro"})
			must(err)
			if _, err := sec.Service.LinkIdentity.Handle(root, sapp.LinkIdentity{ID: uid(martaUser), Issuer: "https://id.example", Subject: "sub-pedro"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("an identity belongs to one user: %v", err)
			}
			p, err := sec.Authenticator(fixedIdentity{"https://id.example", "sub-pedro"}).Authenticate(ctx, "Bearer any")
			if err != nil || p.Subject != uid(pedroUser).UUID || p.Name != "pedro" {
				t.Fatalf("external identity: %+v %v", p, err)
			}
			if _, err := sec.Authenticator(fixedIdentity{"https://id.example", "sub-nobody"}).Authenticate(ctx, "Bearer any"); !errors.Is(err, authz.ErrInvalidCredentials) {
				t.Fatalf("an identity nobody linked: %v", err)
			}
			role, err := sec.Service.DefineRole.Handle(root, sapp.DefineRole{Name: "Auditor", Permissions: []string{"Parties.Party.Read"}})
			must(err)
			if _, err := sec.Service.DefineRole.Handle(root, sapp.DefineRole{Name: "auditor"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("role names are unique: %v", err)
			}
			roleID, _ := sdomain.ParseRoleID(role.ID)
			if _, err := sec.Service.RetireRole.Handle(root, sapp.RetireRole{ID: roleID}); err != nil {
				t.Fatalf("retire: %v", err)
			}
			if _, err := sec.Service.SetUserActive.Handle(marta, sapp.SetUserActive{ID: sdomain.BootstrapAdminUser}); !errors.Is(err, fw.ErrNotFound) {
				t.Fatalf("the global administrator is out of marta's sight: %v", err)
			}
			trail, err := sec.Audit.Trail(ctx, sdomain.UserKind, carlosUser.ID)
			if err != nil || len(trail) < 6 || trail[0].Actor.Name != "marta" {
				t.Fatalf("audit: %d records %v", len(trail), err)
			}
			pending, err := sec.IntegrationOutbox.Pending(ctx, 200, 5)
			if err != nil || len(pending) < 10 {
				t.Fatalf("published language: %d %v", len(pending), err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type fixedIdentity struct{ issuer, subject string }

func (f fixedIdentity) Verify(context.Context, string) (scontracts.ExternalIdentity, error) {
	return scontracts.ExternalIdentity{Issuer: f.issuer, Subject: f.subject}, nil
}

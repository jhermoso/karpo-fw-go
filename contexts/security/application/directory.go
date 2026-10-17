package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/security/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Directory implementa authz.Directory sobre los repositorios de Security: es la fuente real que
// lee el resolvedor de autorización genérico en cada petición (sin caché entre peticiones, D7), en
// lugar del directorio en memoria. Un usuario desactivado, un rol retirado, un permiso desactivado
// o un acceso retirado surten efecto en la siguiente petición con el mismo token.
type Directory struct {
	Users       domain.UserRepository
	Roles       domain.RoleRepository
	Permissions domain.PermissionRepository
}

var _ authz.Directory = Directory{}

// Subject implementa authz.Directory. Un usuario que tiene que cambiar su contraseña se devuelve sin
// roles, permisos ni accesos: hasta que lo haga, cada caso de uso protegido responde 403 y sólo
// queda abierto el cambio de contraseña, que no necesita permiso.
func (d Directory) Subject(ctx context.Context, id fw.UUID) (authz.Subject, bool, error) {
	u, err := d.Users.Get(ctx, domain.UserID{UUID: id})
	if errors.Is(err, fw.ErrNotFound) {
		return authz.Subject{}, false, nil
	}
	if err != nil {
		return authz.Subject{}, false, err
	}
	s := authz.Subject{Active: u.IsActive(), Locked: u.IsLockedAt(fw.Now())}
	if !s.Active || s.Locked || u.MustChangePassword() {
		return s, true, nil
	}
	if ids := u.Roles(); len(ids) > 0 {
		roles, err := d.Roles.Find(ctx, domain.RolesWithIDs(ids...))
		if err != nil {
			return authz.Subject{}, false, err
		}
		entries, err := d.Permissions.Find(ctx, domain.PermFieldActive.Eq(true))
		if err != nil {
			return authz.Subject{}, false, err
		}
		inForce := map[domain.Permission]bool{}
		for _, e := range entries {
			inForce[e.Code()] = true
		}
		for _, r := range roles {
			s.Roles = append(s.Roles, r.Name())
			for _, p := range r.Permissions() {
				if code := authz.Permission(p); inForce[p] && !slices.Contains(s.Permissions, code) {
					s.Permissions = append(s.Permissions, code)
				}
			}
		}
		slices.Sort(s.Roles)
		slices.Sort(s.Permissions)
	}
	for _, a := range u.Accesses() {
		s.Grants = append(s.Grants, authz.Grant{OrganizationID: a.Organization.UUID, Level: authz.ParseAccessLevel(string(a.Level)),
			IncludeSubsidiaries: a.IncludeSubsidiaries})
	}
	return s, true, nil
}

// Authenticator implementa authz.Authenticator para los tokens de un proveedor de identidad externo
// (decisión 1): el verificador dice quién afirma el proveedor que es quien llama, y Security
// dice con cuál de sus usuarios está vinculada esa identidad. Una identidad no vinculada no
// entra: Security nunca crea usuarios por su cuenta.
type Authenticator struct {
	Verifier contracts.TokenVerifier
	Users    domain.UserRepository
}

var _ authz.Authenticator = Authenticator{}

// Authenticate implementa authz.Authenticator. credentials es el token en bruto o "Bearer <token>".
func (a Authenticator) Authenticate(ctx context.Context, credentials string) (authz.Principal, error) {
	token := strings.TrimSpace(credentials)
	if len(token) > 7 && strings.EqualFold(token[:7], "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" {
		return authz.Principal{}, authz.ErrNoCredentials
	}
	asserted, err := a.Verifier.Verify(ctx, token)
	if err != nil {
		return authz.Principal{}, fmt.Errorf("%w: %v", authz.ErrInvalidCredentials, err)
	}
	id, err := domain.NewExternalIdentity(asserted.Issuer, asserted.Subject)
	if err != nil {
		return authz.Principal{}, fmt.Errorf("%w: incomplete identity", authz.ErrInvalidCredentials)
	}
	u, found, err := first(a.Users.Find(ctx, domain.WithIdentity(id)))
	if err != nil {
		return authz.Principal{}, err
	}
	if !found {
		return authz.Principal{}, fmt.Errorf("%w: identity not linked with a user", authz.ErrInvalidCredentials)
	}
	return authz.Principal{Subject: u.ID().UUID, Name: u.Username(), PartyID: u.Party().UUID, Kind: authz.Human,
		Claims: map[string]string{"iss": id.Issuer, "sub": id.Subject}}, nil
}

// Users implementa contracts.Users.
type Users struct{ Repository domain.UserRepository }

var _ contracts.Users = Users{}

func userRef(u *domain.User) contracts.UserRef {
	return contracts.UserRef{ID: u.ID().String(), Username: u.Username(), PartyID: u.Party().String(), Active: u.IsActive()}
}

func parseBatch(ids []string) ([]fw.UUID, error) {
	if len(ids) > contracts.MaxBatch {
		return nil, fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, contracts.MaxBatch)
	}
	var out []fw.UUID
	for _, s := range ids {
		if id, err := fw.ParseUUID(s); err == nil && !id.IsZero() {
			out = append(out, id)
		}
	}
	return out, nil
}

// Resolve implementa contracts.Users.
func (d Users) Resolve(ctx context.Context, userIDs []string) (map[string]contracts.UserRef, error) {
	ids, err := parseBatch(userIDs)
	out := map[string]contracts.UserRef{}
	if err != nil || len(ids) == 0 {
		return out, err
	}
	want := make([]domain.UserID, len(ids))
	for i, id := range ids {
		want[i] = domain.UserID{UUID: id}
	}
	users, err := d.Repository.Find(ctx, domain.UsersWithIDs(want...))
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.ID().String()] = userRef(u)
	}
	return out, nil
}

// ByParty implementa contracts.Users.
func (d Users) ByParty(ctx context.Context, partyIDs []string) (map[string][]contracts.UserRef, error) {
	ids, err := parseBatch(partyIDs)
	out := map[string][]contracts.UserRef{}
	if err != nil || len(ids) == 0 {
		return out, err
	}
	want := make([]domain.PartyID, len(ids))
	for i, id := range ids {
		want[i] = domain.PartyID{UUID: id}
	}
	users, err := d.Repository.Find(ctx, domain.UsersOfParties(want...), domain.UserFieldUsername.Asc())
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		out[u.Party().String()] = append(out[u.Party().String()], userRef(u))
	}
	return out, nil
}

// Publications traduce los eventos de dominio al Published Language. Los logins correctos y los
// fallidos se quedan en el registro de auditoría: no se publican.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	one := func(e app.IntegrationEvent) ([]app.IntegrationEvent, error) { return []app.IntegrationEvent{e}, nil }
	messaging.On(r, func(_ context.Context, e domain.UserRegistered) ([]app.IntegrationEvent, error) {
		return one(contracts.UserRegisteredV1{UserID: e.AggregateID, Username: e.Username, PartyID: e.Party})
	})
	messaging.On(r, func(_ context.Context, e domain.UserActivationChanged) ([]app.IntegrationEvent, error) {
		if e.Active {
			return one(contracts.UserReactivatedV1{UserID: e.AggregateID})
		}
		return one(contracts.UserDeactivatedV1{UserID: e.AggregateID})
	})
	messaging.On(r, func(_ context.Context, e domain.UserLocked) ([]app.IntegrationEvent, error) {
		return one(contracts.UserLockedV1{UserID: e.AggregateID, Until: e.Until})
	})
	messaging.On(r, func(_ context.Context, e domain.PasswordChanged) ([]app.IntegrationEvent, error) {
		return one(contracts.UserPasswordChangedV1{UserID: e.AggregateID, Reset: e.Reset})
	})
	messaging.On(r, func(_ context.Context, e domain.UserRoleAssigned) ([]app.IntegrationEvent, error) {
		return one(contracts.UserRoleAssignedV1{UserID: e.AggregateID, RoleID: e.Role, Role: e.RoleName})
	})
	messaging.On(r, func(_ context.Context, e domain.UserRoleRevoked) ([]app.IntegrationEvent, error) {
		return one(contracts.UserRoleRevokedV1{UserID: e.AggregateID, RoleID: e.Role})
	})
	messaging.On(r, func(_ context.Context, e domain.AccessGranted) ([]app.IntegrationEvent, error) {
		return one(contracts.OrganizationAccessGrantedV1{UserID: e.AggregateID, Organization: e.Organization, Level: e.Level})
	})
	messaging.On(r, func(_ context.Context, e domain.AccessChanged) ([]app.IntegrationEvent, error) {
		return one(contracts.OrganizationAccessChangedV1{UserID: e.AggregateID, Organization: e.Organization, Level: e.Level})
	})
	messaging.On(r, func(_ context.Context, e domain.AccessRevoked) ([]app.IntegrationEvent, error) {
		return one(contracts.OrganizationAccessRevokedV1{UserID: e.AggregateID, Organization: e.Organization})
	})
	messaging.On(r, func(_ context.Context, e domain.IdentityLinked) ([]app.IntegrationEvent, error) {
		return one(contracts.IdentityLinkedV1{UserID: e.AggregateID, Issuer: e.Issuer})
	})
	messaging.On(r, func(_ context.Context, e domain.IdentityUnlinked) ([]app.IntegrationEvent, error) {
		return one(contracts.IdentityUnlinkedV1{UserID: e.AggregateID, Issuer: e.Issuer})
	})
	messaging.On(r, func(_ context.Context, e domain.RoleDefined) ([]app.IntegrationEvent, error) {
		return one(contracts.RoleDefinedV1{RoleID: e.AggregateID, Name: e.Name, Permissions: e.Permissions})
	})
	messaging.On(r, func(_ context.Context, e domain.RolePermissionsChanged) ([]app.IntegrationEvent, error) {
		return one(contracts.RolePermissionsChangedV1{RoleID: e.AggregateID, Name: e.Name, Permissions: e.Permissions})
	})
	messaging.On(r, func(_ context.Context, e domain.RoleRetired) ([]app.IntegrationEvent, error) {
		return one(contracts.RoleRetiredV1{RoleID: e.AggregateID, Name: e.Name})
	})
	return r
}

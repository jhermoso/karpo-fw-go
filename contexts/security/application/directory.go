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

// Directory implements authz.Directory over the Security repositories: it is the real source the
// generic authorization resolver reads on every request (no cache between requests, D7), in
// place of the in-memory directory. A deactivated user, a revoked role, a deactivated permission
// or a revoked access take effect on the next request with the same token.
type Directory struct {
	Users       domain.UserRepository
	Roles       domain.RoleRepository
	Permissions domain.PermissionRepository
}

var _ authz.Directory = Directory{}

// Subject implements authz.Directory. A user that must change its password is returned without
// roles, permissions or accesses: until it does, every protected use case answers 403 and only
// the change of password, which needs no permission, is open.
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

// Authenticator implements authz.Authenticator for the tokens of an external identity provider
// (decision 1): the verifier says who the provider asserts the caller is, and Security answers
// which of its users that identity is linked with. An identity that is not linked does not get
// in: Security never creates users on its own.
type Authenticator struct {
	Verifier contracts.TokenVerifier
	Users    domain.UserRepository
}

var _ authz.Authenticator = Authenticator{}

// Authenticate implements authz.Authenticator. credentials is the raw token or "Bearer <token>".
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

// Users implements contracts.Users.
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

// Resolve implements contracts.Users.
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

// ByParty implements contracts.Users.
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

// Publications translates the domain events into the Published Language. Successful and failed
// logins stay in the audit log: they are not published.
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

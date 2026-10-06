package application

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// AccessInput is an organization access in a command.
type AccessInput struct {
	Organization        string `json:"organization"`
	Level               string `json:"level"`
	IncludeSubsidiaries bool   `json:"includeSubsidiaries,omitempty"`
}

// RegisterUser registers a user. Access is the first organization access: it is mandatory unless
// the caller is a global administrator, as nobody else could administer a user without accesses.
// A password set here must be changed by the user on the first sign-in.
type RegisterUser struct {
	Username string       `json:"username"`
	Party    string       `json:"party"`
	Password string       `json:"password,omitempty"`
	Access   *AccessInput `json:"access,omitempty"`
}

// RenameUser changes the user name.
type RenameUser struct {
	ID       domain.UserID `json:"-"`
	Username string        `json:"username"`
}

// SetUserActive deactivates or reactivates a user.
type SetUserActive struct {
	ID     domain.UserID
	Active bool
}

// UnlockUser lifts the lock of a user.
type UnlockUser struct{ ID domain.UserID }

// ResetPassword sets a new password the user must change on the next sign-in.
type ResetPassword struct {
	ID       domain.UserID `json:"-"`
	Password string        `json:"password"`
}

// AssignRole assigns a role to a user; RevokeRole revokes it.
type AssignRole struct {
	ID   domain.UserID
	Role domain.RoleID
}

// RevokeRole revokes a role from a user.
type RevokeRole struct {
	ID   domain.UserID
	Role domain.RoleID
}

// GrantAccess grants the access to an organization or changes its level.
type GrantAccess struct {
	ID                  domain.UserID         `json:"-"`
	Organization        domain.OrganizationID `json:"-"`
	Level               string                `json:"level"`
	IncludeSubsidiaries bool                  `json:"includeSubsidiaries,omitempty"`
}

// RevokeAccess revokes the access to an organization.
type RevokeAccess struct {
	ID           domain.UserID
	Organization domain.OrganizationID
}

// LinkIdentity links an external identity (issuer + subject) with a user.
type LinkIdentity struct {
	ID      domain.UserID `json:"-"`
	Issuer  string        `json:"issuer"`
	Subject string        `json:"subject"`
}

// UnlinkIdentity removes an external identity of a user.
type UnlinkIdentity struct {
	ID      domain.UserID `json:"-"`
	Issuer  string        `json:"issuer"`
	Subject string        `json:"subject"`
}

// GetUser loads a user.
type GetUser struct{ ID domain.UserID }

// SearchUsers searches the users visible to the caller. Administrable keeps only the ones the
// caller may administer.
type SearchUsers struct {
	Text, Organization, Role  string
	ActiveOnly, Administrable bool
	Page, Size                int
}

// RoleRefDTO names a role of a user.
type RoleRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// IdentityDTO is an external identity of a user.
type IdentityDTO struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// UserDTO is the transport form of a user. It never carries the password hash.
type UserDTO struct {
	ID                 string        `json:"id"`
	Username           string        `json:"username"`
	Party              string        `json:"party"`
	Active             bool          `json:"active"`
	Locked             bool          `json:"locked"`
	LockedUntil        *time.Time    `json:"lockedUntil,omitempty"`
	HasPassword        bool          `json:"hasPassword"`
	MustChangePassword bool          `json:"mustChangePassword"`
	LastLoginAt        *time.Time    `json:"lastLoginAt,omitempty"`
	Roles              []RoleRefDTO  `json:"roles"`
	Accesses           []AccessInput `json:"accesses,omitempty"`
	Identities         []IdentityDTO `json:"identities"`
	Version            int64         `json:"version"`
}

// roleNames resolves the names of the roles of users (one query).
func (s *service) roleNames(ctx context.Context, users ...*domain.User) (map[domain.RoleID]string, error) {
	var ids []domain.RoleID
	for _, u := range users {
		for _, r := range u.Roles() {
			if !slices.Contains(ids, r) {
				ids = append(ids, r)
			}
		}
	}
	names := map[domain.RoleID]string{}
	if len(ids) == 0 {
		return names, nil
	}
	roles, err := s.Roles.Find(ctx, domain.RolesWithIDs(ids...))
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		names[r.ID()] = r.Name()
	}
	return names, nil
}

// userDTO maps a user. The accesses are shown only to callers holding the permission to read them.
func userDTO(ctx context.Context, u *domain.User, names map[domain.RoleID]string) UserDTO {
	d := UserDTO{ID: u.ID().String(), Username: u.Username(), Party: u.Party().String(), Active: u.IsActive(),
		Locked: u.IsLockedAt(fw.Now()), LockedUntil: u.LockedUntil(), HasPassword: u.HasPassword(),
		MustChangePassword: u.MustChangePassword(), LastLoginAt: u.LastLoginAt(), Roles: []RoleRefDTO{},
		Identities: []IdentityDTO{}, Version: u.Version()}
	for _, r := range u.Roles() {
		d.Roles = append(d.Roles, RoleRefDTO{ID: r.String(), Name: names[r]})
	}
	if authz.Require(ctx, PermAccessRead) == nil {
		for _, a := range u.Accesses() {
			d.Accesses = append(d.Accesses, AccessInput{Organization: a.Organization.String(), Level: string(a.Level),
				IncludeSubsidiaries: a.IncludeSubsidiaries})
		}
	}
	for _, i := range u.Identities() {
		d.Identities = append(d.Identities, IdentityDTO{Issuer: i.Issuer, Subject: i.Subject})
	}
	return d
}

func (s *service) userResult(ctx context.Context, u *domain.User, err error) (UserDTO, error) {
	if err != nil {
		return UserDTO{}, err
	}
	names, err := s.roleNames(ctx, u)
	if err != nil {
		return UserDTO{}, err
	}
	return userDTO(ctx, u, names), nil
}

// parseAccess validates an access of a command and checks its organization is internal.
func (s *service) parseAccess(ctx context.Context, org domain.OrganizationID, level string, subsidiaries bool) (domain.OrganizationAccess, error) {
	l, err := domain.ParseAccessLevel(level)
	if err != nil {
		return domain.OrganizationAccess{}, err
	}
	if org.IsZero() {
		return domain.OrganizationAccess{}, invalid("organization", "required", "the organization is required")
	}
	if s.Organizations != nil {
		ok, err := s.Organizations.IsInternalOrganization(ctx, org.UUID)
		if err != nil {
			return domain.OrganizationAccess{}, err
		}
		if !ok {
			return domain.OrganizationAccess{}, invalid("organization", "unknown", "accesses are granted to internal organizations")
		}
	}
	return domain.OrganizationAccess{Organization: org, Level: l, IncludeSubsidiaries: subsidiaries}, nil
}

func (s *service) usernameFree(ctx context.Context, username string, except domain.UserID) error {
	taken, err := s.Users.Exists(ctx, domain.UserNamed(username).And(domain.UserFieldID.Ne(except)))
	if err != nil {
		return err
	}
	if taken {
		return fw.Violation("security.username_taken", "the user name is already in use")
	}
	return nil
}

// keepsGlobalAdmin checks that another active global administrator remains besides u.
func (s *service) keepsGlobalAdmin(ctx context.Context, u *domain.User) error {
	if !u.IsActive() || !u.HasRole(domain.RoleGlobalSuperAdmin) {
		return nil
	}
	other, err := s.Users.Exists(ctx, domain.GlobalAdministrators(u.ID()))
	if err != nil {
		return err
	}
	if !other {
		return domain.ErrLastGlobalAdmin
	}
	return nil
}

// administer loads a user and applies fn if the caller may administer it: a user outside the
// caller's sight does not exist (uniform 404); a visible one the caller does not fully encompass
// is forbidden (403).
func (s *service) administer(ctx context.Context, id domain.UserID, fn func(ctx context.Context, a domain.Administrator, u *domain.User) error) (UserDTO, error) {
	a, err := administratorOf(ctx)
	if err != nil {
		return UserDTO{}, err
	}
	u, err := s.users.Update(ctx, id, func(ctx context.Context, u *domain.User) error {
		if !a.Sees(u) && a.User != u.ID() {
			return fw.NotFound(domain.UserKind, id)
		}
		if err := a.CanAdminister(u); err != nil {
			return err
		}
		return fn(ctx, a, u)
	})
	return s.userResult(ctx, u, err)
}

// changeAccess loads a user and applies fn if the caller may change its access to org. The user
// needs not be visible: granting your organization to a user of another one is how it becomes so.
func (s *service) changeAccess(ctx context.Context, id domain.UserID, org domain.OrganizationID, fn func(ctx context.Context, u *domain.User) error) (UserDTO, error) {
	a, err := administratorOf(ctx)
	if err != nil {
		return UserDTO{}, err
	}
	u, err := s.users.Update(ctx, id, func(ctx context.Context, u *domain.User) error {
		if err := a.CanChangeAccess(u, org); err != nil {
			return err
		}
		return fn(ctx, u)
	})
	return s.userResult(ctx, u, err)
}

func (s *service) registerUser(ctx context.Context, c RegisterUser) (UserDTO, error) {
	a, err := administratorOf(ctx)
	if err != nil {
		return UserDTO{}, err
	}
	var v fw.Validation
	username, err := domain.NormalizeUsername(c.Username)
	v.Merge("", err)
	party, err := fw.ParseUUID(c.Party)
	v.Require(err == nil && !party.IsZero(), "party", "format", "party must be a party id")
	v.Require(c.Access != nil || a.Global, "access", "required", "a user needs a first organization access")
	if c.Password != "" {
		v.Merge("", domain.CheckPassword(c.Password, username))
	}
	if err := v.Err(); err != nil {
		return UserDTO{}, err
	}
	var access *domain.OrganizationAccess
	if c.Access != nil {
		org, err := domain.ParseOrganizationID(c.Access.Organization)
		if err != nil {
			return UserDTO{}, invalid("access.organization", "format", "organization must be a party id")
		}
		if err := a.CanGrant(org); err != nil {
			return UserDTO{}, err
		}
		acc, err := s.parseAccess(ctx, org, c.Access.Level, c.Access.IncludeSubsidiaries)
		if err != nil {
			return UserDTO{}, err
		}
		access = &acc
	}
	if s.Parties != nil {
		ok, err := s.Parties.PartyExists(ctx, party)
		if err != nil {
			return UserDTO{}, err
		}
		if !ok {
			return UserDTO{}, invalid("party", "unknown", "unknown party")
		}
	}
	var hash string
	if c.Password != "" {
		if hash, err = s.Hasher.Hash(c.Password); err != nil {
			return UserDTO{}, err
		}
	}
	u, err := domain.RegisterUser(domain.NewUserID(), domain.PartyID{UUID: party}, username)
	if err != nil {
		return UserDTO{}, err
	}
	if hash != "" {
		if err := u.SetPassword(hash, true, true); err != nil {
			return UserDTO{}, err
		}
	}
	if access != nil {
		if err := u.GrantAccess(*access); err != nil {
			return UserDTO{}, err
		}
	}
	err = s.UoW.Do(ctx, func(ctx context.Context) error {
		if err := s.usernameFree(ctx, username, domain.UserID{}); err != nil {
			return err
		}
		return s.users.Create(ctx, u)
	})
	return s.userResult(ctx, u, err)
}

func (s *service) searchUsers(ctx context.Context, q SearchUsers) (fw.Page[UserDTO], error) {
	a, err := administratorOf(ctx)
	if err != nil {
		return fw.Page[UserDTO]{}, err
	}
	var parts []spec.Specification[*domain.User]
	if !a.Global {
		parts = append(parts, domain.VisibleTo(a.Organizations...))
		if q.Administrable {
			parts = append(parts, domain.AdministrableBy(a.Full...))
		}
	}
	if t := strings.TrimSpace(q.Text); t != "" {
		parts = append(parts, domain.UserFieldUsername.ContainsFold(t))
	}
	if q.Organization != "" {
		org, err := domain.ParseOrganizationID(q.Organization)
		if err != nil {
			return fw.Page[UserDTO]{}, invalid("organization", "format", "organization must be a party id")
		}
		parts = append(parts, domain.VisibleTo(org))
	}
	if q.Role != "" {
		role, err := domain.ParseRoleID(q.Role)
		if err != nil {
			return fw.Page[UserDTO]{}, invalid("role", "format", "role must be a role id")
		}
		parts = append(parts, domain.HoldingRole(role))
	}
	if q.ActiveOnly {
		parts = append(parts, domain.UserFieldActive.Eq(true))
	}
	page, err := s.Users.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.UserFieldUsername.Asc()))
	if err != nil {
		return fw.Page[UserDTO]{}, err
	}
	names, err := s.roleNames(ctx, page.Items...)
	if err != nil {
		return fw.Page[UserDTO]{}, err
	}
	return fw.MapPage(page, func(u *domain.User) UserDTO { return userDTO(ctx, u, names) }), nil
}

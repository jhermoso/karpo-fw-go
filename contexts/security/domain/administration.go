package domain

import (
	"fmt"
	"slices"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Administrator is who performs an act of delegated administration, as resolved for the request:
// its permissions, the organizations in its scope and the ones it can write (Full access).
//
// The three rules of delegated administration (decision 5) are its methods:
//
//  1. nobody grants what they do not have (CanAssign, CanGrant);
//  2. you administer the users you fully encompass (CanAdminister, AdministrableBy);
//  3. nobody administers themselves (every rule rejects the administrator's own user). The other
//     half of rule 3, that an active global administrator always remains, needs the other users
//     and is checked by the application with GlobalAdministrators.
type Administrator struct {
	User          UserID
	Global        bool
	Permissions   []Permission
	Organizations []OrganizationID // effective scope: what the administrator sees
	Full          []OrganizationID // organizations with Full access inside the scope
}

func forbidden(msg string) error { return fmt.Errorf("%w: %s", fw.ErrForbidden, msg) }

func (a Administrator) holds(p Permission) bool {
	return a.Global || slices.Contains(a.Permissions, Wildcard) || slices.Contains(a.Permissions, p)
}

func (a Administrator) notSelf(u UserID) error {
	if a.User == u {
		return forbidden("nobody administers their own user")
	}
	return nil
}

// CanAssign checks rule 1 for roles: to assign or revoke a role the administrator must hold
// every permission the role carries. It replaces the C# check by role name and the unused
// hierarchy level: a custom role with more permissions than the administrator's is out of reach.
func (a Administrator) CanAssign(r *Role) error {
	for _, p := range r.Permissions() {
		if !a.holds(p) {
			return forbidden("the role carries permissions you do not have")
		}
	}
	return nil
}

// CanGrant checks rule 1 for organizations: to grant, change or revoke an access to an
// organization the administrator needs Full access to it.
func (a Administrator) CanGrant(org OrganizationID) error {
	if a.Global || slices.Contains(a.Full, org) {
		return nil
	}
	return forbidden("organization outside the writable scope")
}

// Sees reports whether the user is visible to the administrator: it has an access to an
// organization of the administrator's scope.
func (a Administrator) Sees(u *User) bool {
	return a.Global || VisibleTo(a.Organizations...).IsSatisfiedBy(u)
}

// Encompasses reports whether the administrator fully encompasses the user (rule 2).
func (a Administrator) Encompasses(u *User) bool {
	return a.Global || AdministrableBy(a.Full...).IsSatisfiedBy(u)
}

// CanAdminister checks rules 2 and 3 for one user: the administrator may rename, deactivate,
// unlock, reset the password, link identities or change the roles of a user only when it has
// Full access to every organization the user works in. A user without accesses belongs to the
// global administrator. In C# a single shared organization was enough, even read-only.
func (a Administrator) CanAdminister(u *User) error {
	if err := a.notSelf(u.ID()); err != nil {
		return err
	}
	if !a.Encompasses(u) {
		return forbidden("you do not have full access to every organization of the user")
	}
	return nil
}

// CanChangeAccess checks rules 1 and 3 for the access of a user to an organization.
func (a Administrator) CanChangeAccess(u *User, org OrganizationID) error {
	if err := a.notSelf(u.ID()); err != nil {
		return err
	}
	return a.CanGrant(org)
}

// User fields and specifications.
var (
	UserFieldID          = spec.Comparable("id", func(u *User) UserID { return u.ID() })
	UserFieldUsername    = spec.Text("username", (*User).Username)
	UserFieldUsernameKey = spec.Comparable("username_key", (*User).UsernameKey)
	UserFieldParty       = spec.Comparable("party", (*User).Party)
	UserFieldActive      = spec.Comparable("active", (*User).IsActive)

	UserFieldRoles      = spec.Collection("roles", (*User).Roles)
	UserRoleFieldRole   = spec.Comparable("role_id", func(r RoleID) RoleID { return r })
	UserFieldAccesses   = spec.Collection("accesses", (*User).Accesses)
	AccessFieldOrg      = spec.Comparable("organization", func(a OrganizationAccess) OrganizationID { return a.Organization })
	UserFieldIdentities = spec.Collection("identities", (*User).Identities)
	IdentityFieldIssuer = spec.Comparable("issuer", func(i ExternalIdentity) string { return i.Issuer })
	IdentityFieldSubj   = spec.Comparable("subject", func(i ExternalIdentity) string { return i.Subject })
)

// UsersWithIDs matches the given users.
func UsersWithIDs(ids ...UserID) spec.Spec[*User] { return UserFieldID.In(ids...) }

// UserNamed matches the user with that user name, ignoring case.
func UserNamed(username string) spec.Spec[*User] {
	return UserFieldUsernameKey.Eq(UsernameKey(username))
}

// UsersOfParties matches the users of the parties.
func UsersOfParties(parties ...PartyID) spec.Spec[*User] { return UserFieldParty.In(parties...) }

// WithIdentity matches the user linked with an external identity.
func WithIdentity(id ExternalIdentity) spec.Spec[*User] {
	return UserFieldIdentities.Any(IdentityFieldIssuer.Eq(id.Issuer).And(IdentityFieldSubj.Eq(id.Subject)))
}

// HoldingRole matches the users with any of the roles.
func HoldingRole(roles ...RoleID) spec.Spec[*User] {
	if len(roles) == 0 {
		return spec.None[*User]()
	}
	return UserFieldRoles.Any(UserRoleFieldRole.In(roles...))
}

// VisibleTo is the scope rule of Security for reading: a user does not belong to an organization,
// it has accesses; it is visible to whoever has one of those organizations in scope.
func VisibleTo(orgs ...OrganizationID) spec.Spec[*User] {
	if len(orgs) == 0 {
		return spec.None[*User]()
	}
	return UserFieldAccesses.Any(AccessFieldOrg.In(orgs...))
}

// AdministrableBy is the scope rule of Security for administering (rule 2): the users with at
// least one access, all of them to organizations where the administrator has Full access.
func AdministrableBy(full ...OrganizationID) spec.Spec[*User] {
	if len(full) == 0 {
		return spec.None[*User]()
	}
	return UserFieldAccesses.Any(AccessFieldOrg.In(full...)).And(UserFieldAccesses.None(AccessFieldOrg.NotIn(full...)))
}

// GlobalAdministrators matches the active users holding the GlobalSuperAdmin role, other than
// except: rule 3 requires that one always remains.
func GlobalAdministrators(except UserID) spec.Spec[*User] {
	return UserFieldActive.Eq(true).And(HoldingRole(RoleGlobalSuperAdmin), UserFieldID.Ne(except))
}

// ErrLastGlobalAdmin is returned when an act would leave the installation without an active
// global administrator.
var ErrLastGlobalAdmin = fw.Violation("security.last_global_admin", "an active global administrator must always remain")

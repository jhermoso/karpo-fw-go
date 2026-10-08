package domain

import (
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// UserRegistered is raised when a user is registered.
type UserRegistered struct {
	fw.EventMeta
	Username string `json:"username"`
	Party    string `json:"party"`
}

// EventType implements domain.Event.
func (UserRegistered) EventType() string { return "security.user_registered" }

// UserRenamed is raised when the user name changes.
type UserRenamed struct {
	fw.EventMeta
	Username string `json:"username"`
}

// EventType implements domain.Event.
func (UserRenamed) EventType() string { return "security.user_renamed" }

// UserActivationChanged is raised when a user is deactivated or reactivated.
type UserActivationChanged struct {
	fw.EventMeta
	Active bool `json:"active"`
}

// EventType implements domain.Event.
func (UserActivationChanged) EventType() string { return "security.user_activation_changed" }

// PasswordChanged is raised when the password changes. It never carries the password or its hash.
type PasswordChanged struct {
	fw.EventMeta
	Reset bool `json:"reset"` // set by an administrator
}

// EventType implements domain.Event.
func (PasswordChanged) EventType() string { return "security.password_changed" }

// UserLoggedIn is raised on a successful password login (audit only; it is not published).
type UserLoggedIn struct{ fw.EventMeta }

// EventType implements domain.Event.
func (UserLoggedIn) EventType() string { return "security.user_logged_in" }

// LoginFailed is raised on a failed password check (audit only; it is not published).
type LoginFailed struct {
	fw.EventMeta
	Attempts int `json:"attempts"`
}

// EventType implements domain.Event.
func (LoginFailed) EventType() string { return "security.login_failed" }

// UserLocked is raised when too many failed logins lock the user.
type UserLocked struct {
	fw.EventMeta
	Until time.Time `json:"until"`
}

// EventType implements domain.Event.
func (UserLocked) EventType() string { return "security.user_locked" }

// UserUnlocked is raised when an administrator lifts the lock.
type UserUnlocked struct{ fw.EventMeta }

// EventType implements domain.Event.
func (UserUnlocked) EventType() string { return "security.user_unlocked" }

// UserRoleAssigned is raised when a role is assigned to a user.
type UserRoleAssigned struct {
	fw.EventMeta
	Role     string `json:"role"`
	RoleName string `json:"roleName"`
}

// EventType implements domain.Event.
func (UserRoleAssigned) EventType() string { return "security.user_role_assigned" }

// UserRoleRevoked is raised when a role is revoked from a user.
type UserRoleRevoked struct {
	fw.EventMeta
	Role string `json:"role"`
}

// EventType implements domain.Event.
func (UserRoleRevoked) EventType() string { return "security.user_role_revoked" }

// AccessGranted is raised when a user gets access to an organization.
type AccessGranted struct {
	fw.EventMeta
	Organization string `json:"organization"`
	Level        string `json:"level"`
}

// EventType implements domain.Event.
func (AccessGranted) EventType() string { return "security.access_granted" }

// AccessChanged is raised when the access of a user to an organization changes.
type AccessChanged struct {
	fw.EventMeta
	Organization string `json:"organization"`
	Level        string `json:"level"`
}

// EventType implements domain.Event.
func (AccessChanged) EventType() string { return "security.access_changed" }

// AccessRevoked is raised when a user loses the access to an organization.
type AccessRevoked struct {
	fw.EventMeta
	Organization string `json:"organization"`
}

// EventType implements domain.Event.
func (AccessRevoked) EventType() string { return "security.access_revoked" }

// IdentityLinked is raised when an external identity is linked with a user.
type IdentityLinked struct {
	fw.EventMeta
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// EventType implements domain.Event.
func (IdentityLinked) EventType() string { return "security.identity_linked" }

// IdentityUnlinked is raised when an external identity is unlinked.
type IdentityUnlinked struct {
	fw.EventMeta
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// EventType implements domain.Event.
func (IdentityUnlinked) EventType() string { return "security.identity_unlinked" }

// RoleDefined is raised when a custom role is defined.
type RoleDefined struct {
	fw.EventMeta
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// EventType implements domain.Event.
func (RoleDefined) EventType() string { return "security.role_defined" }

// RoleDescribed is raised when the name or the description of a role changes.
type RoleDescribed struct {
	fw.EventMeta
	Name string `json:"name"`
}

// EventType implements domain.Event.
func (RoleDescribed) EventType() string { return "security.role_described" }

// RolePermissionsChanged is raised when the permissions of a role change.
type RolePermissionsChanged struct {
	fw.EventMeta
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// EventType implements domain.Event.
func (RolePermissionsChanged) EventType() string { return "security.role_permissions_changed" }

// RoleRetired is raised when a custom role is retired.
type RoleRetired struct {
	fw.EventMeta
	Name string `json:"name"`
}

// EventType implements domain.Event.
func (RoleRetired) EventType() string { return "security.role_retired" }

// Repositories of the context.
type (
	// UserRepository stores users.
	UserRepository = fw.Repository[UserID, *User]
	// RoleRepository stores roles.
	RoleRepository = fw.Repository[RoleID, *Role]
	// PermissionRepository stores the permission catalog.
	PermissionRepository = fw.Repository[PermissionID, *PermissionEntry]
	// SessionRepository stores refresh sessions.
	SessionRepository = fw.Repository[SessionID, *Session]
)

// Password policy (decision 3): length only, no composition rules.
const (
	MinPasswordLength = 12
	MaxPasswordLength = 128
)

// CheckPassword applies the password policy: 12 to 128 characters and different from the user
// name. It replaces the C#, which accepted any password.
func CheckPassword(password, username string) error {
	var v fw.Validation
	n := utf8.RuneCountInString(password)
	v.Require(n >= MinPasswordLength, "password", "too_short", "a password has at least 12 characters")
	v.Require(n <= MaxPasswordLength, "password", "too_long", "a password has at most 128 characters")
	v.Require(UsernameKey(password) != UsernameKey(username), "password", "username", "the password cannot be the user name")
	return v.Err()
}

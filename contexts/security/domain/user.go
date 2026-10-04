package domain

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
)

// UserKind is the stable aggregate type name.
const UserKind = "security.user"

// AccessLevel is the access a user has to an organization (contract v1, P2: Full writes,
// ReadOnly reads, Restricted behaves as ReadOnly).
type AccessLevel string

// Access levels.
const (
	Full       AccessLevel = "Full"
	ReadOnly   AccessLevel = "ReadOnly"
	Restricted AccessLevel = "Restricted"
)

// ParseAccessLevel parses a level. Unlike the C# free text, an unknown level is rejected.
func ParseAccessLevel(s string) (AccessLevel, error) {
	switch l := AccessLevel(s); l {
	case Full, ReadOnly, Restricted:
		return l, nil
	}
	var v fw.Validation
	v.Add("level", "unknown", "the access level is Full, ReadOnly or Restricted")
	return "", v.Err()
}

// OrganizationAccess is the access of a user to one internal organization. IncludeSubsidiaries
// travels in the authorization contract but expands nothing in v1 (decision P2).
type OrganizationAccess struct {
	Organization        OrganizationID
	Level               AccessLevel
	IncludeSubsidiaries bool
}

// ExternalIdentity links a user with an identity of an external identity provider: the issuer of
// its tokens and the subject inside that issuer (decision 1). The pair is unique in the
// installation.
type ExternalIdentity struct {
	Issuer  string
	Subject string
}

// NewExternalIdentity validates an external identity.
func NewExternalIdentity(issuer, subject string) (ExternalIdentity, error) {
	id := ExternalIdentity{Issuer: strings.TrimSpace(issuer), Subject: strings.TrimSpace(subject)}
	var v fw.Validation
	v.Require(id.Issuer != "" && len(id.Issuer) <= 200, "issuer", "length", "the issuer is required, at most 200 characters")
	v.Require(id.Subject != "" && len(id.Subject) <= 200, "subject", "length", "the subject is required, at most 200 characters")
	return id, v.Err()
}

var usernamePattern = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}._@+-]*$`)

// NormalizeUsername validates a user name: 3 to 100 characters, letters, digits and . _ @ + -,
// starting with a letter or a digit. Case is kept for display; uniqueness ignores it.
func NormalizeUsername(s string) (string, error) {
	s = strings.TrimSpace(s)
	var v fw.Validation
	n := utf8.RuneCountInString(s)
	v.Require(n >= 3 && n <= 100, "username", "length", "a user name has 3 to 100 characters")
	v.Require(n < 3 || usernamePattern.MatchString(s), "username", "format", "a user name has letters, digits and . _ @ + -")
	return s, v.Err()
}

// UsernameKey returns the form used to keep user names unique and to look them up.
func UsernameKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// LockoutPolicy locks a user after too many failed logins.
type LockoutPolicy struct {
	MaxAttempts int
	Duration    time.Duration
}

// DefaultLockout is the C# default: 5 failed attempts lock the user for 15 minutes.
var DefaultLockout = LockoutPolicy{MaxAttempts: 5, Duration: 15 * time.Minute}

// User is who can sign in and act: a party with credentials (an optional password, external
// identities), roles and accesses to organizations. A user is never deleted, only deactivated.
type User struct {
	fw.BaseAggregateRoot[UserID]
	traits.Activation
	traits.Audited
	party              PartyID
	username           string
	passwordHash       string // self-describing hash; empty when the user has no password
	mustChangePassword bool
	failedAttempts     int
	lockedUntil        *time.Time
	lastLoginAt        *time.Time
	roles              []RoleID
	accesses           []OrganizationAccess
	identities         []ExternalIdentity
}

// UserState is the persisted state of a user.
type UserState struct {
	Party              PartyID
	Username           string
	PasswordHash       string
	MustChangePassword bool
	FailedAttempts     int
	LockedUntil        *time.Time
	LastLoginAt        *time.Time
	Roles              []RoleID
	Accesses           []OrganizationAccess
	Identities         []ExternalIdentity
	Active             bool
	Audit              traits.AuditStamp
}

// ReconstituteUser rebuilds a user from persisted state.
func ReconstituteUser(id UserID, s UserState) (*User, error) {
	base, err := fw.NewBaseAggregateRoot(UserKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Party.IsZero(), "party", "required", "a user is a party")
	username, err := NormalizeUsername(s.Username)
	v.Merge("", err)
	v.Require(s.FailedAttempts >= 0, "failedAttempts", "range", "cannot be negative")
	u := &User{BaseAggregateRoot: base, Activation: traits.RestoredActivation(s.Active), Audited: traits.RestoredAudit(s.Audit),
		party: s.Party, username: username, passwordHash: s.PasswordHash, mustChangePassword: s.MustChangePassword,
		failedAttempts: s.FailedAttempts, lockedUntil: cloneTime(s.LockedUntil), lastLoginAt: cloneTime(s.LastLoginAt)}
	for _, r := range s.Roles {
		v.Require(!r.IsZero() && !slices.Contains(u.roles, r), "roles", "duplicate", "a role is assigned once")
		u.roles = append(u.roles, r)
	}
	for _, a := range s.Accesses {
		_, err := ParseAccessLevel(string(a.Level))
		v.Merge("accesses", err)
		v.Require(!a.Organization.IsZero() && u.accessIndex(a.Organization) < 0, "accesses", "duplicate", "one access per organization")
		u.accesses = append(u.accesses, a)
	}
	for _, i := range s.Identities {
		n, err := NewExternalIdentity(i.Issuer, i.Subject)
		v.Merge("identities", err)
		v.Require(!slices.Contains(u.identities, n), "identities", "duplicate", "an identity is linked once")
		u.identities = append(u.identities, n)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	// A stable order whatever the engine returns (SQL Server sorts GUIDs by byte groups).
	slices.SortFunc(u.roles, func(a, b RoleID) int { return strings.Compare(a.String(), b.String()) })
	slices.SortFunc(u.accesses, func(a, b OrganizationAccess) int {
		return strings.Compare(a.Organization.String(), b.Organization.String())
	})
	slices.SortFunc(u.identities, func(a, b ExternalIdentity) int {
		return strings.Compare(a.Issuer+"\x00"+a.Subject, b.Issuer+"\x00"+b.Subject)
	})
	return u, nil
}

// RegisterUser creates an active user without credentials, roles or accesses.
func RegisterUser(id UserID, party PartyID, username string) (*User, error) {
	u, err := ReconstituteUser(id, UserState{Party: party, Username: username, Active: true})
	if err != nil {
		return nil, err
	}
	u.Raise(UserRegistered{EventMeta: u.NewEventMeta(), Username: u.username, Party: party.String()})
	return u, nil
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	c := t.UTC()
	return &c
}

// Party returns the party behind the user.
func (u *User) Party() PartyID { return u.party }

// Username returns the user name as registered.
func (u *User) Username() string { return u.username }

// UsernameKey returns the lookup form of the user name.
func (u *User) UsernameKey() string { return UsernameKey(u.username) }

// PasswordHash returns the stored hash ("" when the user has no password). It never leaves the
// application layer.
func (u *User) PasswordHash() string { return u.passwordHash }

// HasPassword reports whether the user can sign in with a password.
func (u *User) HasPassword() bool { return u.passwordHash != "" }

// MustChangePassword reports whether the user has to change the password before doing anything.
func (u *User) MustChangePassword() bool { return u.mustChangePassword }

// FailedAttempts returns the consecutive failed logins.
func (u *User) FailedAttempts() int { return u.failedAttempts }

// LockedUntil returns the end of the lock, if any.
func (u *User) LockedUntil() *time.Time { return cloneTime(u.lockedUntil) }

// LastLoginAt returns the last successful password login, if any.
func (u *User) LastLoginAt() *time.Time { return cloneTime(u.lastLoginAt) }

// Roles returns the assigned roles.
func (u *User) Roles() []RoleID { return slices.Clone(u.roles) }

// Accesses returns the organization accesses.
func (u *User) Accesses() []OrganizationAccess { return slices.Clone(u.accesses) }

// Identities returns the linked external identities.
func (u *User) Identities() []ExternalIdentity { return slices.Clone(u.identities) }

// HasRole reports whether the role is assigned.
func (u *User) HasRole(r RoleID) bool { return slices.Contains(u.roles, r) }

func (u *User) accessIndex(org OrganizationID) int {
	return slices.IndexFunc(u.accesses, func(a OrganizationAccess) bool { return a.Organization == org })
}

// IsLockedAt reports whether the user is locked at t.
func (u *User) IsLockedAt(t time.Time) bool { return u.lockedUntil != nil && t.Before(*u.lockedUntil) }

// CanSignInAt reports whether the user may authenticate at t: active and not locked.
func (u *User) CanSignInAt(t time.Time) bool { return u.IsActive() && !u.IsLockedAt(t) }

func (u *User) requireActive(action string) error {
	if !u.IsActive() {
		return fw.Violation("security.inactive_user", "an inactive user cannot "+action)
	}
	return nil
}

// Rename changes the user name.
func (u *User) Rename(username string) error {
	if err := u.requireActive("be renamed"); err != nil {
		return err
	}
	username, err := NormalizeUsername(username)
	if err != nil {
		return err
	}
	if username == u.username {
		return nil
	}
	u.username = username
	u.Raise(UserRenamed{EventMeta: u.NewEventMeta(), Username: username})
	return nil
}

// SetPassword stores a new password hash (computed by the application from a password that
// passed the policy). reset tells whether an administrator set it; mustChange forces the user to
// change it before doing anything else. Changing the password lifts the lock.
func (u *User) SetPassword(hash string, mustChange, reset bool) error {
	if err := u.requireActive("change the password"); err != nil {
		return err
	}
	if hash == "" {
		var v fw.Validation
		v.Add("password", "required", "a password hash is required")
		return v.Err()
	}
	u.passwordHash, u.mustChangePassword = hash, mustChange
	u.failedAttempts, u.lockedUntil = 0, nil
	u.Raise(PasswordChanged{EventMeta: u.NewEventMeta(), Reset: reset})
	return nil
}

// UpgradePasswordHash replaces the hash of the same password with a stronger one (more
// iterations or another algorithm) after a successful login. It is not a password change.
func (u *User) UpgradePasswordHash(hash string) {
	if hash != "" && u.passwordHash != "" {
		u.passwordHash = hash
	}
}

// RecordLogin records a successful password login.
func (u *User) RecordLogin(at time.Time) {
	u.failedAttempts, u.lockedUntil = 0, nil
	u.lastLoginAt = cloneTime(&at)
	u.Raise(UserLoggedIn{EventMeta: u.NewEventMeta()})
}

// RecordFailedLogin counts a failed password check and locks the user when the policy says so.
// It reports whether the user became locked.
func (u *User) RecordFailedLogin(p LockoutPolicy, at time.Time) bool {
	if u.lockedUntil != nil && !at.Before(*u.lockedUntil) { // an expired lock starts a new count
		u.failedAttempts, u.lockedUntil = 0, nil
	}
	u.failedAttempts++
	u.Raise(LoginFailed{EventMeta: u.NewEventMeta(), Attempts: u.failedAttempts})
	if p.MaxAttempts <= 0 || u.failedAttempts < p.MaxAttempts {
		return false
	}
	until := at.Add(p.Duration).UTC()
	u.lockedUntil = &until
	u.Raise(UserLocked{EventMeta: u.NewEventMeta(), Until: until})
	return true
}

// Unlock lifts the lock and forgets the failed attempts.
func (u *User) Unlock() {
	if u.lockedUntil == nil && u.failedAttempts == 0 {
		return
	}
	u.failedAttempts, u.lockedUntil = 0, nil
	u.Raise(UserUnlocked{EventMeta: u.NewEventMeta()})
}

// Deactivate deactivates the user: it can no longer authenticate nor be authorized.
func (u *User) Deactivate() {
	if u.Activation.Deactivate() {
		u.Raise(UserActivationChanged{EventMeta: u.NewEventMeta(), Active: false})
	}
}

// Activate reactivates the user.
func (u *User) Activate() {
	if u.Activation.Activate() {
		u.Raise(UserActivationChanged{EventMeta: u.NewEventMeta(), Active: true})
	}
}

// AssignRole assigns a role. Assigning it twice changes nothing.
func (u *User) AssignRole(r *Role) error {
	if err := u.requireActive("receive roles"); err != nil {
		return err
	}
	if u.HasRole(r.ID()) {
		return nil
	}
	u.roles = append(slices.Clone(u.roles), r.ID())
	u.Raise(UserRoleAssigned{EventMeta: u.NewEventMeta(), Role: r.ID().String(), RoleName: r.Name()})
	return nil
}

// RevokeRole removes a role. It reports whether the user had it.
func (u *User) RevokeRole(id RoleID) bool {
	if !u.HasRole(id) {
		return false
	}
	u.roles = slices.DeleteFunc(slices.Clone(u.roles), func(r RoleID) bool { return r == id })
	u.Raise(UserRoleRevoked{EventMeta: u.NewEventMeta(), Role: id.String()})
	return true
}

// GrantAccess gives the user access to an organization, or changes the access it already has.
func (u *User) GrantAccess(a OrganizationAccess) error {
	if err := u.requireActive("receive organization accesses"); err != nil {
		return err
	}
	var v fw.Validation
	v.Require(!a.Organization.IsZero(), "organization", "required", "the organization is required")
	_, err := ParseAccessLevel(string(a.Level))
	v.Merge("", err)
	if err := v.Err(); err != nil {
		return err
	}
	next := slices.Clone(u.accesses)
	if i := u.accessIndex(a.Organization); i >= 0 {
		if next[i] == a {
			return nil
		}
		next[i] = a
		u.accesses = next
		u.Raise(AccessChanged{EventMeta: u.NewEventMeta(), Organization: a.Organization.String(), Level: string(a.Level)})
		return nil
	}
	u.accesses = append(next, a)
	u.Raise(AccessGranted{EventMeta: u.NewEventMeta(), Organization: a.Organization.String(), Level: string(a.Level)})
	return nil
}

// RevokeAccess removes the access to an organization. It reports whether the user had it.
func (u *User) RevokeAccess(org OrganizationID) bool {
	i := u.accessIndex(org)
	if i < 0 {
		return false
	}
	u.accesses = slices.Delete(slices.Clone(u.accesses), i, i+1)
	u.Raise(AccessRevoked{EventMeta: u.NewEventMeta(), Organization: org.String()})
	return true
}

// LinkIdentity links an external identity. The application checks no other user has it.
func (u *User) LinkIdentity(id ExternalIdentity) error {
	if err := u.requireActive("link identities"); err != nil {
		return err
	}
	id, err := NewExternalIdentity(id.Issuer, id.Subject)
	if err != nil {
		return err
	}
	if slices.Contains(u.identities, id) {
		return nil
	}
	u.identities = append(slices.Clone(u.identities), id)
	u.Raise(IdentityLinked{EventMeta: u.NewEventMeta(), Issuer: id.Issuer, Subject: id.Subject})
	return nil
}

// UnlinkIdentity removes an external identity. It reports whether the user had it.
func (u *User) UnlinkIdentity(id ExternalIdentity) bool {
	i := slices.Index(u.identities, id)
	if i < 0 {
		return false
	}
	u.identities = slices.Delete(slices.Clone(u.identities), i, i+1)
	u.Raise(IdentityUnlinked{EventMeta: u.NewEventMeta(), Issuer: id.Issuer, Subject: id.Subject})
	return true
}

// AuditSnapshot implements traits.Snapshotter. It never exposes the password hash, and leaves
// out the login counters, which change on every sign-in.
func (u *User) AuditSnapshot() map[string]any {
	roles := make([]string, len(u.roles))
	for i, r := range u.roles {
		roles[i] = r.String()
	}
	slices.Sort(roles)
	accesses := make([]string, len(u.accesses))
	for i, a := range u.accesses {
		accesses[i] = a.Organization.String() + ":" + string(a.Level)
	}
	slices.Sort(accesses)
	identities := make([]string, len(u.identities))
	for i, id := range u.identities {
		identities[i] = id.Issuer + "|" + id.Subject
	}
	slices.Sort(identities)
	return map[string]any{"username": u.username, "party": u.party.String(), "active": u.IsActive(),
		"hasPassword": u.HasPassword(), "mustChangePassword": u.mustChangePassword, "locked": u.lockedUntil != nil,
		"roles": strings.Join(roles, ","), "accesses": strings.Join(accesses, ","), "identities": strings.Join(identities, ",")}
}

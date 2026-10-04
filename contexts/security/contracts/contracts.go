// Package contracts is what other bounded contexts and hosts may depend on: the Published
// Language of Security (versioned integration events), its user directory and the port through
// which an external identity provider plugs in. No event carries passwords, hashes or tokens.
//
// The main port Security offers is not declared here: it is the framework's authz.Directory,
// implemented by the Security module, which the generic authorization resolver reads on every
// request.
package contracts

import (
	"context"
	"time"
)

// Source is the name of the publishing bounded context (Envelope.Source).
const Source = "security"

// MaxBatch is the maximum number of ids per directory call.
const MaxBatch = 900

// UserRef is what other contexts need of a user.
type UserRef struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	PartyID  string `json:"partyId"`
	Active   bool   `json:"active"`
}

// Users resolves users in batches. Missing users are absent.
type Users interface {
	// Resolve returns the users with those ids, by id.
	Resolve(ctx context.Context, userIDs []string) (map[string]UserRef, error)
	// ByParty returns the users of those parties, by party id.
	ByParty(ctx context.Context, partyIDs []string) (map[string][]UserRef, error)
}

// ExternalIdentity is who an external identity provider says the caller is: the issuer of the
// token and the subject inside that issuer.
type ExternalIdentity struct {
	Issuer  string
	Subject string
}

// TokenVerifier validates the token of an external identity provider (signature, issuer,
// audience, lifetime) and returns the identity it asserts. An adapter per provider implements it
// (OIDC with public keys); Security only links the identity with one of its users.
type TokenVerifier interface {
	Verify(ctx context.Context, credentials string) (ExternalIdentity, error)
}

// UserRegisteredV1 is published when a user is registered.
type UserRegisteredV1 struct {
	UserID   string `json:"userId"`
	Username string `json:"username"`
	PartyID  string `json:"partyId"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (UserRegisteredV1) IntegrationEventType() string { return "security.user-registered.v1" }

// UserDeactivatedV1 is published when a user is deactivated: it can no longer sign in.
type UserDeactivatedV1 struct {
	UserID string `json:"userId"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (UserDeactivatedV1) IntegrationEventType() string { return "security.user-deactivated.v1" }

// UserReactivatedV1 is published when a user is reactivated.
type UserReactivatedV1 struct {
	UserID string `json:"userId"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (UserReactivatedV1) IntegrationEventType() string { return "security.user-reactivated.v1" }

// UserLockedV1 is published when too many failed logins lock a user.
type UserLockedV1 struct {
	UserID string    `json:"userId"`
	Until  time.Time `json:"until"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (UserLockedV1) IntegrationEventType() string { return "security.user-locked.v1" }

// UserPasswordChangedV1 is published when the password of a user changes.
type UserPasswordChangedV1 struct {
	UserID string `json:"userId"`
	Reset  bool   `json:"reset"` // set by an administrator
}

// IntegrationEventType implements application.IntegrationEvent.
func (UserPasswordChangedV1) IntegrationEventType() string {
	return "security.user-password-changed.v1"
}

// UserRoleAssignedV1 is published when a role is assigned to a user.
type UserRoleAssignedV1 struct {
	UserID string `json:"userId"`
	RoleID string `json:"roleId"`
	Role   string `json:"role"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (UserRoleAssignedV1) IntegrationEventType() string { return "security.user-role-assigned.v1" }

// UserRoleRevokedV1 is published when a role is revoked from a user.
type UserRoleRevokedV1 struct {
	UserID string `json:"userId"`
	RoleID string `json:"roleId"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (UserRoleRevokedV1) IntegrationEventType() string { return "security.user-role-revoked.v1" }

// OrganizationAccessGrantedV1 is published when a user gets access to an organization.
type OrganizationAccessGrantedV1 struct {
	UserID       string `json:"userId"`
	Organization string `json:"organization"`
	Level        string `json:"level"` // Full | ReadOnly | Restricted
}

// IntegrationEventType implements application.IntegrationEvent.
func (OrganizationAccessGrantedV1) IntegrationEventType() string {
	return "security.organization-access-granted.v1"
}

// OrganizationAccessChangedV1 is published when the access level of a user changes.
type OrganizationAccessChangedV1 struct {
	UserID       string `json:"userId"`
	Organization string `json:"organization"`
	Level        string `json:"level"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (OrganizationAccessChangedV1) IntegrationEventType() string {
	return "security.organization-access-changed.v1"
}

// OrganizationAccessRevokedV1 is published when a user loses the access to an organization.
type OrganizationAccessRevokedV1 struct {
	UserID       string `json:"userId"`
	Organization string `json:"organization"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (OrganizationAccessRevokedV1) IntegrationEventType() string {
	return "security.organization-access-revoked.v1"
}

// IdentityLinkedV1 is published when an external identity is linked with a user.
type IdentityLinkedV1 struct {
	UserID string `json:"userId"`
	Issuer string `json:"issuer"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (IdentityLinkedV1) IntegrationEventType() string { return "security.identity-linked.v1" }

// IdentityUnlinkedV1 is published when an external identity is unlinked from a user.
type IdentityUnlinkedV1 struct {
	UserID string `json:"userId"`
	Issuer string `json:"issuer"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (IdentityUnlinkedV1) IntegrationEventType() string { return "security.identity-unlinked.v1" }

// RoleDefinedV1 is published when a custom role is defined.
type RoleDefinedV1 struct {
	RoleID      string   `json:"roleId"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (RoleDefinedV1) IntegrationEventType() string { return "security.role-defined.v1" }

// RolePermissionsChangedV1 is published when the permissions of a role change.
type RolePermissionsChangedV1 struct {
	RoleID      string   `json:"roleId"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (RolePermissionsChangedV1) IntegrationEventType() string {
	return "security.role-permissions-changed.v1"
}

// RoleRetiredV1 is published when a custom role is retired.
type RoleRetiredV1 struct {
	RoleID string `json:"roleId"`
	Name   string `json:"name"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (RoleRetiredV1) IntegrationEventType() string { return "security.role-retired.v1" }

// Package domain is the model of the Security bounded context: users and how they authenticate
// (password, external identities), roles and the permissions they carry, access to organizations,
// refresh sessions, the permission catalog and the rules of delegated administration. It is the
// Go port of the C# ErpKernel Security subdomain (see docs/SEGURIDAD.md): authentication says who
// the caller is; this context says what the caller may do and on which organizations.
package domain

import (
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// UserID identifies a security user (same GUIDs as the C# IdSecurityUser).
type UserID struct{ fw.UUID }

// NewUserID returns a new identity.
func NewUserID() UserID { return UserID{fw.NewUUID()} }

// ParseUserID parses a textual identity.
func ParseUserID(s string) (UserID, error) {
	u, err := fw.ParseUUID(s)
	return UserID{u}, err
}

// MustUserID parses a well-known identity.
func MustUserID(s string) UserID { return UserID{fw.MustParseUUID(s)} }

// RoleID identifies a role (same GUIDs as the C# IdSecurityRole for the seeded ones).
type RoleID struct{ fw.UUID }

// NewRoleID returns a new identity.
func NewRoleID() RoleID { return RoleID{fw.NewUUID()} }

// ParseRoleID parses a textual identity.
func ParseRoleID(s string) (RoleID, error) {
	u, err := fw.ParseUUID(s)
	return RoleID{u}, err
}

// MustRoleID parses a well-known identity.
func MustRoleID(s string) RoleID { return RoleID{fw.MustParseUUID(s)} }

// SessionID identifies a refresh session.
type SessionID struct{ fw.UUID }

// NewSessionID returns a new identity.
func NewSessionID() SessionID { return SessionID{fw.NewUUID()} }

// PartyID references the Parties party behind a user, by identity.
type PartyID struct{ fw.UUID }

// OrganizationID references an internal organization (a Parties party), by identity.
type OrganizationID struct{ fw.UUID }

// ParseOrganizationID parses a textual identity.
func ParseOrganizationID(s string) (OrganizationID, error) {
	u, err := fw.ParseUUID(s)
	return OrganizationID{u}, err
}

// Package domain is the model of the Parties bounded context: who takes part in the business
// (people and organizations), the roles they play and the relationships between them
// (UDM Vol. 1, ch. 2). It is written only against the framework's pure domain packages.
package domain

import fw "github.com/jhermoso/karpo-fw-go/pkg/domain"

// PartyID identifies a party. The value is the same GUID used by the C# IdParty.
type PartyID struct{ fw.UUID }

// NewPartyID returns a new identity.
func NewPartyID() PartyID { return PartyID{fw.NewUUID()} }

// ParsePartyID parses a textual identity.
func ParsePartyID(s string) (PartyID, error) { u, err := fw.ParseUUID(s); return PartyID{u}, err }

// PartyRoleID identifies a role played by a party (child entity of the Party aggregate).
type PartyRoleID struct{ fw.UUID }

// NewPartyRoleID returns a new identity.
func NewPartyRoleID() PartyRoleID { return PartyRoleID{fw.NewUUID()} }

// ParsePartyRoleID parses a textual identity.
func ParsePartyRoleID(s string) (PartyRoleID, error) {
	u, err := fw.ParseUUID(s)
	return PartyRoleID{u}, err
}

// RoleTypeID identifies a role type of the catalog.
type RoleTypeID struct{ fw.UUID }

// ParseRoleTypeID parses a textual identity.
func ParseRoleTypeID(s string) (RoleTypeID, error) {
	u, err := fw.ParseUUID(s)
	return RoleTypeID{u}, err
}

// MustRoleTypeID parses a well-known identity (panics on a malformed literal).
func MustRoleTypeID(s string) RoleTypeID { return RoleTypeID{fw.MustParseUUID(s)} }

// RelationshipTypeID identifies a relationship type of the catalog.
type RelationshipTypeID struct{ fw.UUID }

// ParseRelationshipTypeID parses a textual identity.
func ParseRelationshipTypeID(s string) (RelationshipTypeID, error) {
	u, err := fw.ParseUUID(s)
	return RelationshipTypeID{u}, err
}

// MustRelationshipTypeID parses a well-known identity (panics on a malformed literal).
func MustRelationshipTypeID(s string) RelationshipTypeID {
	return RelationshipTypeID{fw.MustParseUUID(s)}
}

// RelationshipID identifies a relationship between two parties.
type RelationshipID struct{ fw.UUID }

// NewRelationshipID returns a new identity.
func NewRelationshipID() RelationshipID { return RelationshipID{fw.NewUUID()} }

// ParseRelationshipID parses a textual identity.
func ParseRelationshipID(s string) (RelationshipID, error) {
	u, err := fw.ParseUUID(s)
	return RelationshipID{u}, err
}

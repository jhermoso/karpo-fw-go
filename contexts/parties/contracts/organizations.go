package contracts

import (
	"context"
	"time"
)

// PartyAffiliatedV1 is published when a party becomes affiliated with an internal organization
// (an active relationship with it): the membership other contexts scope their data by.
type PartyAffiliatedV1 struct {
	PartyID        string    `json:"partyId"`
	Organization   string    `json:"organization"`
	RelationshipID string    `json:"relationshipId"`
	From           time.Time `json:"from"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyAffiliatedV1) IntegrationEventType() string { return "parties.party-affiliated.v1" }

// PartyAffiliationEndedV1 is published when an affiliation ends.
type PartyAffiliationEndedV1 struct {
	PartyID        string    `json:"partyId"`
	Organization   string    `json:"organization"`
	RelationshipID string    `json:"relationshipId"`
	At             time.Time `json:"at"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyAffiliationEndedV1) IntegrationEventType() string {
	return "parties.party-affiliation-ended.v1"
}

// Membership answers which internal organizations each party belongs to (the C#
// IPartyOrganizationMembership): every requested id is a key, with an empty list when the party
// belongs to none.
type Membership interface {
	InternalOrganizations(ctx context.Context, partyIDs []string) (map[string][]string, error)
}

// OrganizationHierarchy walks organization rollups (the C# IOrganizationHierarchy).
type OrganizationHierarchy interface {
	// Descendants returns the organizations and every unit below them.
	Descendants(ctx context.Context, organizationIDs []string) ([]string, error)
	// InternalOrganizationOf returns, for each unit, the nearest ancestor (or itself) that is an
	// internal organization; units without one are absent.
	InternalOrganizationOf(ctx context.Context, unitIDs []string) (map[string]PartyRef, error)
}

// InternalOrganizationCatalog lists the internal organizations (the C# IInternalOrganizationCatalog).
type InternalOrganizationCatalog interface {
	All(ctx context.Context) ([]PartyRef, error)
}

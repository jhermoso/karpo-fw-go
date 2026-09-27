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

// PartyFacilityRoleAssignedV1 is published when a party starts playing a role at a facility
// (RRHH: the work center of an employee).
type PartyFacilityRoleAssignedV1 struct {
	PartyID  string    `json:"partyId"`
	RoleID   string    `json:"roleId"`
	Facility string    `json:"facility"`
	RoleType string    `json:"roleType"`
	From     time.Time `json:"from"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyFacilityRoleAssignedV1) IntegrationEventType() string {
	return "parties.party-facility-role-assigned.v1"
}

// PartyFacilityRoleEndedV1 is published when a facility role ends.
type PartyFacilityRoleEndedV1 struct {
	PartyID  string    `json:"partyId"`
	RoleID   string    `json:"roleId"`
	Facility string    `json:"facility"`
	At       time.Time `json:"at"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyFacilityRoleEndedV1) IntegrationEventType() string {
	return "parties.party-facility-role-ended.v1"
}

// TaxIdentity is the fiscal identification of a party: its tax number (NIF, or the national or
// foreigner identity number of a person) and the province of its fiscal address (the first two
// digits of the Spanish postal code), as the tax forms need them.
type TaxIdentity struct {
	PartyID      string `json:"partyId"`
	Name         string `json:"name"`
	Country      string `json:"country,omitempty"`
	Number       string `json:"number,omitempty"`
	DocumentType string `json:"documentType,omitempty"` // TXID | NIDN | ARNU
	Province     string `json:"province,omitempty"`
}

// TaxIdentities resolves tax identities in batches (at most MaxDirectoryBatch ids). Missing
// parties are absent; a party without a tax document has an empty Number.
type TaxIdentities interface {
	TaxIdentities(ctx context.Context, partyIDs []string) (map[string]TaxIdentity, error)
}

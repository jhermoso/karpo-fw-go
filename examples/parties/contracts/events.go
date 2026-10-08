// Package contracts is the Published Language of the Parties bounded context: the integration
// events other contexts may consume. It is versioned and changes only by adding fields or by
// publishing a new version (v2) alongside the old one; domain events stay internal.
package contracts

import "time"

// Source is the name of the publishing bounded context (Envelope.Source).
const Source = "parties"

// PartyRegisteredV1 is published when a party is registered.
type PartyRegisteredV1 struct {
	PartyID      string    `json:"partyId"`
	Type         string    `json:"type"` // person | organization
	LegalName    string    `json:"legalName"`
	TaxID        string    `json:"taxId"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyRegisteredV1) IntegrationEventType() string { return "parties.party-registered.v1" }

// PartyRenamedV1 is published when the legal name of a party changes.
type PartyRenamedV1 struct {
	PartyID   string `json:"partyId"`
	LegalName string `json:"legalName"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyRenamedV1) IntegrationEventType() string { return "parties.party-renamed.v1" }

// Package contracts is what other bounded contexts may depend on: the Published Language of
// Parties (versioned integration events) and its Open Host Service (the party directory).
// It changes only by adding fields or publishing a new version next to the old one.
package contracts

import (
	"context"
	"time"
)

// Source is the name of the publishing bounded context (Envelope.Source).
const Source = "parties"

// PartyRegisteredV1 is published when a person or an organization is registered.
type PartyRegisteredV1 struct {
	PartyID      string    `json:"partyId"`
	Kind         string    `json:"kind"` // person | organization
	Name         string    `json:"name"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyRegisteredV1) IntegrationEventType() string { return "parties.party-registered.v1" }

// PartyRenamedV1 is published when the display name of a party changes.
type PartyRenamedV1 struct {
	PartyID string `json:"partyId"`
	Name    string `json:"name"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyRenamedV1) IntegrationEventType() string { return "parties.party-renamed.v1" }

// PartyActivationChangedV1 is published when a party is activated or deactivated.
type PartyActivationChangedV1 struct {
	PartyID string `json:"partyId"`
	Active  bool   `json:"active"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyActivationChangedV1) IntegrationEventType() string {
	return "parties.party-activation-changed.v1"
}

// PartyRoleAssignedV1 is published when a party starts playing a role.
type PartyRoleAssignedV1 struct {
	PartyID  string    `json:"partyId"`
	RoleID   string    `json:"roleId"`
	RoleType string    `json:"roleType"` // same GUIDs as the C# WellKnownCatalog
	From     time.Time `json:"from"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyRoleAssignedV1) IntegrationEventType() string { return "parties.party-role-assigned.v1" }

// PartyRoleEndedV1 is published when a party stops playing a role.
type PartyRoleEndedV1 struct {
	PartyID  string    `json:"partyId"`
	RoleID   string    `json:"roleId"`
	RoleType string    `json:"roleType"`
	At       time.Time `json:"at"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (PartyRoleEndedV1) IntegrationEventType() string { return "parties.party-role-ended.v1" }

// RelationshipEstablishedV1 is published when two parties become related.
type RelationshipEstablishedV1 struct {
	RelationshipID string    `json:"relationshipId"`
	Type           string    `json:"type"`
	FromParty      string    `json:"fromParty"`
	ToParty        string    `json:"toParty"`
	FromRole       string    `json:"fromRole"`
	ToRole         string    `json:"toRole"`
	Since          time.Time `json:"since"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (RelationshipEstablishedV1) IntegrationEventType() string {
	return "parties.relationship-established.v1"
}

// RelationshipTerminatedV1 is published when a relationship ends.
type RelationshipTerminatedV1 struct {
	RelationshipID string    `json:"relationshipId"`
	At             time.Time `json:"at"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (RelationshipTerminatedV1) IntegrationEventType() string {
	return "parties.relationship-terminated.v1"
}

// PartyRef is what other contexts need to show a party: identity, name and whether it is active.
type PartyRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// MaxDirectoryBatch is the maximum number of ids per Resolve call (the C# 900).
const MaxDirectoryBatch = 900

// Directory is the Open Host Service of Parties (the C# IPartyDirectory): resolve names in
// batches, never one by one (a JOIN turned into N network calls is the anti-pattern it avoids).
// A party that does not exist is simply absent from the result; a failure is an error, never an
// empty result.
type Directory interface {
	Resolve(ctx context.Context, ids []string) (map[string]PartyRef, error)
	SearchIDsByName(ctx context.Context, text string, limit int) ([]string, error)
}

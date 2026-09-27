package domain

import (
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// PartyRegistered is raised when a person or an organization is registered.
type PartyRegistered struct {
	fw.EventMeta
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// EventType implements domain.Event.
func (PartyRegistered) EventType() string { return "parties.party_registered" }

// PartyRenamed is raised when the display name of a party changes.
type PartyRenamed struct {
	fw.EventMeta
	From string `json:"from"`
	To   string `json:"to"`
}

// EventType implements domain.Event.
func (PartyRenamed) EventType() string { return "parties.party_renamed" }

// PartyActivated is raised when a party is reactivated.
type PartyActivated struct{ fw.EventMeta }

// EventType implements domain.Event.
func (PartyActivated) EventType() string { return "parties.party_activated" }

// PartyDeactivated is raised when a party is deactivated.
type PartyDeactivated struct{ fw.EventMeta }

// EventType implements domain.Event.
func (PartyDeactivated) EventType() string { return "parties.party_deactivated" }

// PartyRoleAssigned is raised when a party starts playing a role.
type PartyRoleAssigned struct {
	fw.EventMeta
	RoleID   string    `json:"roleId"`
	RoleType string    `json:"roleType"`
	From     time.Time `json:"from"`
}

// EventType implements domain.Event.
func (PartyRoleAssigned) EventType() string { return "parties.party_role_assigned" }

// PartyRoleEnded is raised when a party stops playing a role.
type PartyRoleEnded struct {
	fw.EventMeta
	RoleID   string    `json:"roleId"`
	RoleType string    `json:"roleType"`
	At       time.Time `json:"at"`
}

// EventType implements domain.Event.
func (PartyRoleEnded) EventType() string { return "parties.party_role_ended" }

// RelationshipEstablished is raised when two parties become related.
type RelationshipEstablished struct {
	fw.EventMeta
	Type     string    `json:"type"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	FromRole string    `json:"fromRole"`
	ToRole   string    `json:"toRole"`
	Since    time.Time `json:"since"`
}

// EventType implements domain.Event.
func (RelationshipEstablished) EventType() string { return "parties.relationship_established" }

// RelationshipTerminated is raised when a relationship ends.
type RelationshipTerminated struct {
	fw.EventMeta
	At time.Time `json:"at"`
}

// EventType implements domain.Event.
func (RelationshipTerminated) EventType() string { return "parties.relationship_terminated" }

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

// ProspectTrialChanged is raised when the trial of a prospect relationship is granted, extended,
// shortened or withdrawn (TrialUntil nil).
type ProspectTrialChanged struct {
	fw.EventMeta
	Prospect     string     `json:"prospect"`
	Organization string     `json:"organization"`
	TrialUntil   *time.Time `json:"trialUntil,omitempty"`
}

// EventType implements domain.Event.
func (ProspectTrialChanged) EventType() string { return "parties.prospect_trial_changed" }

// OwnershipShareChanged is raised when the stake of a shareholder is recorded, corrected or
// cleared (Share empty). Share is in points with two decimals ("30.00").
type OwnershipShareChanged struct {
	fw.EventMeta
	Shareholder  string `json:"shareholder"`
	Organization string `json:"organization"`
	Share        string `json:"share,omitempty"`
}

// EventType implements domain.Event.
func (OwnershipShareChanged) EventType() string { return "parties.ownership_share_changed" }

// PartyAffiliated is raised when a party becomes affiliated with an internal organization.
type PartyAffiliated struct {
	fw.EventMeta
	Organization string    `json:"organization"`
	Relationship string    `json:"relationship"`
	From         time.Time `json:"from"`
}

// EventType implements domain.Event.
func (PartyAffiliated) EventType() string { return "parties.party_affiliated" }

// PartyAffiliationEnded is raised when an affiliation ends.
type PartyAffiliationEnded struct {
	fw.EventMeta
	Organization string    `json:"organization"`
	Relationship string    `json:"relationship"`
	At           time.Time `json:"at"`
}

// EventType implements domain.Event.
func (PartyAffiliationEnded) EventType() string { return "parties.party_affiliation_ended" }

// PartySharingChanged is raised when a party becomes, or stops being, a shared catalog entry.
type PartySharingChanged struct {
	fw.EventMeta
	Shared bool `json:"shared"`
}

// EventType implements domain.Event.
func (PartySharingChanged) EventType() string { return "parties.party_sharing_changed" }

// IdentificationAdded is raised when a party gets an identity document.
type IdentificationAdded struct {
	fw.EventMeta
	IdentificationID string `json:"identificationId"`
	DocumentType     string `json:"documentType"`
	Country          string `json:"country"`
	Number           string `json:"number"`
	Primary          bool   `json:"primary"`
}

// EventType implements domain.Event.
func (IdentificationAdded) EventType() string { return "parties.identification_added" }

// IdentificationRemoved is raised when an identity document is removed.
type IdentificationRemoved struct {
	fw.EventMeta
	IdentificationID string `json:"identificationId"`
	DocumentType     string `json:"documentType"`
	Country          string `json:"country"`
	Number           string `json:"number"`
}

// EventType implements domain.Event.
func (IdentificationRemoved) EventType() string { return "parties.identification_removed" }

// ContactAdded is raised when a party gets a contact.
type ContactAdded struct {
	fw.EventMeta
	ContactID string    `json:"contactId"`
	Kind      string    `json:"kind"`
	Value     string    `json:"value,omitempty"`
	Address   string    `json:"address,omitempty"`
	Purposes  []string  `json:"purposes"`
	From      time.Time `json:"from"`
}

// EventType implements domain.Event.
func (ContactAdded) EventType() string { return "parties.contact_added" }

// ContactPurposesChanged is raised when the purposes of a contact change.
type ContactPurposesChanged struct {
	fw.EventMeta
	ContactID string   `json:"contactId"`
	Purposes  []string `json:"purposes"`
}

// EventType implements domain.Event.
func (ContactPurposesChanged) EventType() string { return "parties.contact_purposes_changed" }

// ContactEnded is raised when a contact stops being used.
type ContactEnded struct {
	fw.EventMeta
	ContactID string    `json:"contactId"`
	At        time.Time `json:"at"`
}

// EventType implements domain.Event.
func (ContactEnded) EventType() string { return "parties.contact_ended" }

// PartyClassified is raised when a party gets a classification.
type PartyClassified struct {
	fw.EventMeta
	ClassificationID string    `json:"classificationId"`
	Type             string    `json:"type"`
	From             time.Time `json:"from"`
}

// EventType implements domain.Event.
func (PartyClassified) EventType() string { return "parties.party_classified" }

// PartyClassificationEnded is raised when a classification ends.
type PartyClassificationEnded struct {
	fw.EventMeta
	ClassificationID string    `json:"classificationId"`
	Type             string    `json:"type"`
	At               time.Time `json:"at"`
}

// EventType implements domain.Event.
func (PartyClassificationEnded) EventType() string { return "parties.party_classification_ended" }

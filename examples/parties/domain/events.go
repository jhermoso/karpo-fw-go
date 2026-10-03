package domain

import fw "github.com/jhermoso/karpo-fw-go/pkg/domain"

// PartyRegistered is raised when a party is registered.
type PartyRegistered struct {
	fw.EventMeta
	Type      string `json:"type"`
	LegalName string `json:"legalName"`
	TaxID     string `json:"taxId"`
}

// EventType implements domain.Event.
func (PartyRegistered) EventType() string { return "parties.party_registered" }

// PartyRenamed is raised when the legal name changes.
type PartyRenamed struct {
	fw.EventMeta
	From string `json:"from"`
	To   string `json:"to"`
}

// EventType implements domain.Event.
func (PartyRenamed) EventType() string { return "parties.party_renamed" }

// ContactAdded is raised when a contact mechanism is added.
type ContactAdded struct {
	fw.EventMeta
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

// EventType implements domain.Event.
func (ContactAdded) EventType() string { return "parties.contact_added" }

// PartyDeactivated is raised when a party is deactivated.
type PartyDeactivated struct{ fw.EventMeta }

// EventType implements domain.Event.
func (PartyDeactivated) EventType() string { return "parties.party_deactivated" }

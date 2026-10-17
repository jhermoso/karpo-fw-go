package domain

import fw "github.com/jhermoso/karpo-fw-go/pkg/domain"

// PartyRegistered se lanza cuando se da de alta una party.
type PartyRegistered struct {
	fw.EventMeta
	Type      string `json:"type"`
	LegalName string `json:"legalName"`
	TaxID     string `json:"taxId"`
}

// EventType implementa domain.Event.
func (PartyRegistered) EventType() string { return "parties.party_registered" }

// PartyRenamed se lanza cuando cambia la razón social.
type PartyRenamed struct {
	fw.EventMeta
	From string `json:"from"`
	To   string `json:"to"`
}

// EventType implementa domain.Event.
func (PartyRenamed) EventType() string { return "parties.party_renamed" }

// ContactAdded se lanza cuando se añade un medio de contacto.
type ContactAdded struct {
	fw.EventMeta
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

// EventType implementa domain.Event.
func (ContactAdded) EventType() string { return "parties.contact_added" }

// PartyDeactivated se lanza cuando se desactiva una party.
type PartyDeactivated struct{ fw.EventMeta }

// EventType implementa domain.Event.
func (PartyDeactivated) EventType() string { return "parties.party_deactivated" }

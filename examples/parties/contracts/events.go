// Package contracts es el Published Language del contexto delimitado Parties: los eventos de
// integración que otros contextos pueden consumir. Está versionado y sólo cambia añadiendo
// campos o publicando una versión nueva (v2) junto a la antigua; los eventos de dominio se
// quedan dentro.
package contracts

import "time"

// Source es el nombre del contexto delimitado que publica (Envelope.Source).
const Source = "parties"

// PartyRegisteredV1 se publica cuando se da de alta una party.
type PartyRegisteredV1 struct {
	PartyID      string    `json:"partyId"`
	Type         string    `json:"type"` // person | organization
	LegalName    string    `json:"legalName"`
	TaxID        string    `json:"taxId"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyRegisteredV1) IntegrationEventType() string { return "parties.party-registered.v1" }

// PartyRenamedV1 se publica cuando cambia la razón social de una party.
type PartyRenamedV1 struct {
	PartyID   string `json:"partyId"`
	LegalName string `json:"legalName"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyRenamedV1) IntegrationEventType() string { return "parties.party-renamed.v1" }

// Package contracts es aquello de lo que pueden depender los demás contextos delimitados: el
// Published Language de Parties (eventos de integración versionados) y su Open Host Service (el
// directorio de parties). Sólo cambia añadiendo campos o publicando una versión nueva junto a la
// antigua.
package contracts

import (
	"context"
	"time"
)

// Source es el nombre del contexto delimitado que publica (Envelope.Source).
const Source = "parties"

// PartyRegisteredV1 se publica cuando se da de alta una persona o una organización.
type PartyRegisteredV1 struct {
	PartyID      string    `json:"partyId"`
	Kind         string    `json:"kind"` // person | organization
	Name         string    `json:"name"`
	RegisteredAt time.Time `json:"registeredAt"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyRegisteredV1) IntegrationEventType() string { return "parties.party-registered.v1" }

// PartyRenamedV1 se publica cuando cambia el nombre visible de una party.
type PartyRenamedV1 struct {
	PartyID string `json:"partyId"`
	Name    string `json:"name"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyRenamedV1) IntegrationEventType() string { return "parties.party-renamed.v1" }

// PartyActivationChangedV1 se publica cuando una party se activa o se desactiva.
type PartyActivationChangedV1 struct {
	PartyID string `json:"partyId"`
	Active  bool   `json:"active"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyActivationChangedV1) IntegrationEventType() string {
	return "parties.party-activation-changed.v1"
}

// PartyRoleAssignedV1 se publica cuando una party empieza a desempeñar un rol.
type PartyRoleAssignedV1 struct {
	PartyID  string    `json:"partyId"`
	RoleID   string    `json:"roleId"`
	RoleType string    `json:"roleType"` // los mismos GUID que el WellKnownCatalog de C#
	From     time.Time `json:"from"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyRoleAssignedV1) IntegrationEventType() string { return "parties.party-role-assigned.v1" }

// PartyRoleEndedV1 se publica cuando una party deja de desempeñar un rol.
type PartyRoleEndedV1 struct {
	PartyID  string    `json:"partyId"`
	RoleID   string    `json:"roleId"`
	RoleType string    `json:"roleType"`
	At       time.Time `json:"at"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyRoleEndedV1) IntegrationEventType() string { return "parties.party-role-ended.v1" }

// RelationshipEstablishedV1 se publica cuando dos parties pasan a estar relacionadas.
type RelationshipEstablishedV1 struct {
	RelationshipID string    `json:"relationshipId"`
	Type           string    `json:"type"`
	FromParty      string    `json:"fromParty"`
	ToParty        string    `json:"toParty"`
	FromRole       string    `json:"fromRole"`
	ToRole         string    `json:"toRole"`
	Since          time.Time `json:"since"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (RelationshipEstablishedV1) IntegrationEventType() string {
	return "parties.relationship-established.v1"
}

// RelationshipTerminatedV1 se publica cuando termina una relación.
type RelationshipTerminatedV1 struct {
	RelationshipID string    `json:"relationshipId"`
	At             time.Time `json:"at"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (RelationshipTerminatedV1) IntegrationEventType() string {
	return "parties.relationship-terminated.v1"
}

// RelationshipTransferredV1 se publica cuando una relación de negocio (cliente o proveedor de una
// empresa) pasa de una party a otra al fusionarlas, o vuelve al deshacer la fusión
// (docs/DECISIONES-COMUNES.md, D-93). La empresa no cambia; al fusionar (Returning falso), los
// contextos que guardan algo de la pareja (empresa, CedingParty) lo copian a (empresa,
// ReceivingParty) si allí no hay nada. Al deshacer (Returning cierto) no se copia nada: lo que
// tenga la superviviente es suyo, y la que vuelve conserva lo que tenía.
type RelationshipTransferredV1 struct {
	RelationshipID  string    `json:"relationshipId"`  // la nueva
	TransferredFrom string    `json:"transferredFrom"` // la que terminó
	Type            string    `json:"type"`
	TypeCode        string    `json:"typeCode"` // customer | supplier
	Organization    string    `json:"organization"`
	CedingParty     string    `json:"cedingParty"`
	ReceivingParty  string    `json:"receivingParty"`
	Returning       bool      `json:"returning"`
	At              time.Time `json:"at"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (RelationshipTransferredV1) IntegrationEventType() string {
	return "parties.relationship-transferred.v1"
}

// PartyRef es lo que necesitan los demás contextos para mostrar una party: identidad, nombre y si
// está activa.
type PartyRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// MaxDirectoryBatch es el número máximo de ids por llamada a Resolve (el 900 de C#).
const MaxDirectoryBatch = 900

// Directory es el Open Host Service de Parties (el IPartyDirectory de C#): resuelve nombres por
// lotes, nunca de uno en uno (un JOIN convertido en N llamadas de red es el antipatrón que evita).
// Una party que no existe simplemente no aparece en el resultado; un fallo es un error, nunca un
// resultado vacío.
type Directory interface {
	Resolve(ctx context.Context, ids []string) (map[string]PartyRef, error)
	SearchIDsByName(ctx context.Context, text string, limit int) ([]string, error)
}

// PartyIdentificationAddedV1 se publica cuando una party recibe un documento de identidad
// (Facturación necesita los identificadores fiscales; el número es un dato personal: los
// consumidores deben protegerlo).
type PartyIdentificationAddedV1 struct {
	PartyID          string `json:"partyId"`
	IdentificationID string `json:"identificationId"`
	DocumentType     string `json:"documentType"`
	Country          string `json:"country"`
	Number           string `json:"number"`
	Primary          bool   `json:"primary"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyIdentificationAddedV1) IntegrationEventType() string {
	return "parties.party-identification-added.v1"
}

// PartyIdentificationRemovedV1 se publica cuando se quita un documento de identidad.
type PartyIdentificationRemovedV1 struct {
	PartyID          string `json:"partyId"`
	IdentificationID string `json:"identificationId"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyIdentificationRemovedV1) IntegrationEventType() string {
	return "parties.party-identification-removed.v1"
}

// PartyContactAddedV1 se publica cuando una party recibe un contacto. Un e-mail o un teléfono
// llega tal como se guardó, y puede tener el formato erróneo (D-92: se guarda con una incidencia en
// la ficha). Quien lo use para escribir o llamar lo valida antes (vocab.NewEmail, vocab.NewPhone) y,
// si no vale, lo trata como si no hubiera contacto.
type PartyContactAddedV1 struct {
	PartyID   string    `json:"partyId"`
	ContactID string    `json:"contactId"`
	Kind      string    `json:"kind"` // email | phone | fax | web | postal
	Value     string    `json:"value,omitempty"`
	Address   string    `json:"address,omitempty"` // representación en una línea
	Purposes  []string  `json:"purposes"`
	From      time.Time `json:"from"`
	// Owner es la organización interna dueña del contacto (D-122): sólo ella lo usa. Vacío: de todos.
	Owner string `json:"owner,omitempty"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyContactAddedV1) IntegrationEventType() string { return "parties.party-contact-added.v1" }

// PartyContactPurposesChangedV1 se publica cuando cambian los usos de un contacto.
type PartyContactPurposesChangedV1 struct {
	PartyID   string   `json:"partyId"`
	ContactID string   `json:"contactId"`
	Purposes  []string `json:"purposes"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyContactPurposesChangedV1) IntegrationEventType() string {
	return "parties.party-contact-purposes-changed.v1"
}

// PartyContactEndedV1 se publica cuando un contacto deja de usarse.
type PartyContactEndedV1 struct {
	PartyID   string    `json:"partyId"`
	ContactID string    `json:"contactId"`
	At        time.Time `json:"at"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyContactEndedV1) IntegrationEventType() string { return "parties.party-contact-ended.v1" }

// PartyClassifiedV1 se publica cuando una party recibe una clasificación.
type PartyClassifiedV1 struct {
	PartyID          string    `json:"partyId"`
	ClassificationID string    `json:"classificationId"`
	Type             string    `json:"type"`
	From             time.Time `json:"from"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyClassifiedV1) IntegrationEventType() string { return "parties.party-classified.v1" }

// PartyClassificationEndedV1 se publica cuando termina una clasificación.
type PartyClassificationEndedV1 struct {
	PartyID          string    `json:"partyId"`
	ClassificationID string    `json:"classificationId"`
	Type             string    `json:"type"`
	At               time.Time `json:"at"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyClassificationEndedV1) IntegrationEventType() string {
	return "parties.party-classification-ended.v1"
}

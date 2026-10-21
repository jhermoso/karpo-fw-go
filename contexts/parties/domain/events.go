package domain

import (
	"time"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// PartyRegistered se lanza cuando se da de alta una persona o una organización.
type PartyRegistered struct {
	fw.EventMeta
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// EventType implementa domain.Event.
func (PartyRegistered) EventType() string { return "parties.party_registered" }

// PartyRenamed se lanza cuando cambia el nombre visible de una party.
type PartyRenamed struct {
	fw.EventMeta
	From string `json:"from"`
	To   string `json:"to"`
}

// EventType implementa domain.Event.
func (PartyRenamed) EventType() string { return "parties.party_renamed" }

// PartyActivated se lanza cuando se reactiva una party.
type PartyActivated struct{ fw.EventMeta }

// EventType implementa domain.Event.
func (PartyActivated) EventType() string { return "parties.party_activated" }

// PartyDeactivated se lanza cuando se desactiva una party.
type PartyDeactivated struct{ fw.EventMeta }

// EventType implementa domain.Event.
func (PartyDeactivated) EventType() string { return "parties.party_deactivated" }

// PartyRoleAssigned se lanza cuando una party empieza a desempeñar un rol.
type PartyRoleAssigned struct {
	fw.EventMeta
	RoleID   string    `json:"roleId"`
	RoleType string    `json:"roleType"`
	From     time.Time `json:"from"`
}

// EventType implementa domain.Event.
func (PartyRoleAssigned) EventType() string { return "parties.party_role_assigned" }

// PartyMarkedDuplicate se lanza cuando una party se marca como duplicado de otra.
type PartyMarkedDuplicate struct{ fw.EventMeta }

// EventType implementa domain.Event.
func (PartyMarkedDuplicate) EventType() string { return "parties.party_marked_duplicate" }

// PartyDuplicateCleared se lanza cuando una party deja de ser un duplicado.
type PartyDuplicateCleared struct{ fw.EventMeta }

// EventType implementa domain.Event.
func (PartyDuplicateCleared) EventType() string { return "parties.party_duplicate_cleared" }

// PartyRoleAnnulled se lanza cuando se retira un rol dado por error: se quita, o se termina y se
// marca si algo dependía de él.
type PartyRoleAnnulled struct {
	fw.EventMeta
	RoleID   string    `json:"roleId"`
	RoleType string    `json:"roleType"`
	At       time.Time `json:"at"`
	Removed  bool      `json:"removed"`
}

// EventType implementa domain.Event.
func (PartyRoleAnnulled) EventType() string { return "parties.party_role_annulled" }

// PartyRoleEnded se lanza cuando una party deja de desempeñar un rol.
type PartyRoleEnded struct {
	fw.EventMeta
	RoleID   string    `json:"roleId"`
	RoleType string    `json:"roleType"`
	At       time.Time `json:"at"`
}

// EventType implementa domain.Event.
func (PartyRoleEnded) EventType() string { return "parties.party_role_ended" }

// SelfEmployedDetailsSet se lanza cuando cambia el perfil de actividad de un rol Self Employed
// (docs/DECISIONES-COMUNES.md, D-85). Los campos vacíos son datos que el perfil no tiene.
type SelfEmployedDetailsSet struct {
	fw.EventMeta
	RoleID       string `json:"roleId"`
	TradeName    string `json:"tradeName,omitempty"`
	ActivityCode string `json:"activityCode,omitempty"`
	RegisteredOn string `json:"registeredOn,omitempty"`
}

// EventType implementa domain.Event.
func (SelfEmployedDetailsSet) EventType() string { return "parties.self_employed_details_set" }

// RelationshipEstablished se lanza cuando dos parties pasan a estar relacionadas.
type RelationshipEstablished struct {
	fw.EventMeta
	Type     string    `json:"type"`
	From     string    `json:"from"`
	To       string    `json:"to"`
	FromRole string    `json:"fromRole"`
	ToRole   string    `json:"toRole"`
	Since    time.Time `json:"since"`
}

// EventType implementa domain.Event.
func (RelationshipEstablished) EventType() string { return "parties.relationship_established" }

// RelationshipTerminated se lanza cuando termina una relación.
type RelationshipTerminated struct {
	fw.EventMeta
	At time.Time `json:"at"`
}

// EventType implementa domain.Event.
func (RelationshipTerminated) EventType() string { return "parties.relationship_terminated" }

// RelationshipTransferred se lanza cuando una relación de negocio pasa de una party a otra por una
// fusión, o vuelve al deshacerla (docs/DECISIONES-COMUNES.md, D-93): la relación del evento es la
// nueva, que continúa a Previous con la misma organización.
type RelationshipTransferred struct {
	fw.EventMeta
	Previous     string    `json:"previous"`
	Type         string    `json:"type"`
	TypeCode     string    `json:"typeCode,omitempty"`
	Organization string    `json:"organization"` // el lado que no cambia: la empresa
	Ceding       string    `json:"ceding"`       // la party que tenía la relación
	Receiving    string    `json:"receiving"`    // la party que la tiene ahora
	Returning    bool      `json:"returning"`    // vuelve al deshacer la fusión
	At           time.Time `json:"at"`
}

// EventType implementa domain.Event.
func (RelationshipTransferred) EventType() string { return "parties.relationship_transferred" }

// ProspectTrialChanged se lanza cuando la prueba de una relación de prospect se concede, se
// amplía, se acorta o se retira (TrialUntil nil).
type ProspectTrialChanged struct {
	fw.EventMeta
	Prospect     string     `json:"prospect"`
	Organization string     `json:"organization"`
	TrialUntil   *time.Time `json:"trialUntil,omitempty"`
}

// EventType implementa domain.Event.
func (ProspectTrialChanged) EventType() string { return "parties.prospect_trial_changed" }

// OwnershipShareChanged se lanza cuando la participación de un accionista se registra, se corrige
// o se borra (Share vacío). Share va en puntos con dos decimales ("30.00").
type OwnershipShareChanged struct {
	fw.EventMeta
	Shareholder  string `json:"shareholder"`
	Organization string `json:"organization"`
	Share        string `json:"share,omitempty"`
}

// EventType implementa domain.Event.
func (OwnershipShareChanged) EventType() string { return "parties.ownership_share_changed" }

// PartyAffiliated se lanza cuando una party queda afiliada a una organización interna.
type PartyAffiliated struct {
	fw.EventMeta
	Organization string    `json:"organization"`
	Relationship string    `json:"relationship"`
	From         time.Time `json:"from"`
}

// EventType implementa domain.Event.
func (PartyAffiliated) EventType() string { return "parties.party_affiliated" }

// PartyAffiliationEnded se lanza cuando termina una afiliación.
type PartyAffiliationEnded struct {
	fw.EventMeta
	Organization string    `json:"organization"`
	Relationship string    `json:"relationship"`
	At           time.Time `json:"at"`
}

// EventType implementa domain.Event.
func (PartyAffiliationEnded) EventType() string { return "parties.party_affiliation_ended" }

// PartySharingChanged se lanza cuando una party pasa a ser, o deja de ser, una entrada del catálogo compartido.
type PartySharingChanged struct {
	fw.EventMeta
	Shared bool `json:"shared"`
}

// EventType implementa domain.Event.
func (PartySharingChanged) EventType() string { return "parties.party_sharing_changed" }

// IdentificationAdded se lanza cuando una party recibe un documento de identidad.
type IdentificationAdded struct {
	fw.EventMeta
	IdentificationID string `json:"identificationId"`
	DocumentType     string `json:"documentType"`
	Country          string `json:"country"`
	Number           string `json:"number"`
	Primary          bool   `json:"primary"`
}

// EventType implementa domain.Event.
func (IdentificationAdded) EventType() string { return "parties.identification_added" }

// IdentificationRemoved se lanza cuando se quita un documento de identidad.
type IdentificationRemoved struct {
	fw.EventMeta
	IdentificationID string `json:"identificationId"`
	DocumentType     string `json:"documentType"`
	Country          string `json:"country"`
	Number           string `json:"number"`
}

// EventType implementa domain.Event.
func (IdentificationRemoved) EventType() string { return "parties.identification_removed" }

// ContactAdded se lanza cuando una party recibe un contacto.
type ContactAdded struct {
	fw.EventMeta
	ContactID string    `json:"contactId"`
	Kind      string    `json:"kind"`
	Value     string    `json:"value,omitempty"`
	Address   string    `json:"address,omitempty"`
	Purposes  []string  `json:"purposes"`
	From      time.Time `json:"from"`
}

// EventType implementa domain.Event.
func (ContactAdded) EventType() string { return "parties.contact_added" }

// ContactPurposesChanged se lanza cuando cambian los propósitos de un contacto.
type ContactPurposesChanged struct {
	fw.EventMeta
	ContactID string   `json:"contactId"`
	Purposes  []string `json:"purposes"`
}

// EventType implementa domain.Event.
func (ContactPurposesChanged) EventType() string { return "parties.contact_purposes_changed" }

// ContactEnded se lanza cuando se deja de usar un contacto.
type ContactEnded struct {
	fw.EventMeta
	ContactID string    `json:"contactId"`
	At        time.Time `json:"at"`
}

// EventType implementa domain.Event.
func (ContactEnded) EventType() string { return "parties.contact_ended" }

// PartyClassified se lanza cuando una party recibe una clasificación.
type PartyClassified struct {
	fw.EventMeta
	ClassificationID string    `json:"classificationId"`
	Type             string    `json:"type"`
	From             time.Time `json:"from"`
}

// EventType implementa domain.Event.
func (PartyClassified) EventType() string { return "parties.party_classified" }

// PartyClassificationEnded se lanza cuando termina una clasificación.
type PartyClassificationEnded struct {
	fw.EventMeta
	ClassificationID string    `json:"classificationId"`
	Type             string    `json:"type"`
	At               time.Time `json:"at"`
}

// EventType implementa domain.Event.
func (PartyClassificationEnded) EventType() string { return "parties.party_classification_ended" }

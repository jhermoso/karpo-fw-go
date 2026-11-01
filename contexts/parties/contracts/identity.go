package contracts

import "time"

// PartyIdentityCorrectedV1 se publica cuando alguien corrige la identidad de una party: lo que es
// verdad para todas las empresas (nombre o razón social, documentos, forma jurídica, datos de
// persona, idioma, contactos de todos; docs/DECISIONES-COMUNES.md, D-122). Es el aviso a las
// empresas que se relacionan con ella: Organizations son aquellas a las que está afiliada ahora.
type PartyIdentityCorrectedV1 struct {
	PartyID       string    `json:"partyId"`
	Fields        []string  `json:"fields"` // name, identifications, tradeName, legalForm, gender, birthDate, maritalStatus, language, contacts
	Organizations []string  `json:"organizations"`
	CorrectedBy   string    `json:"correctedBy"`             // el nombre de quien lo hizo
	CorrectedByID string    `json:"correctedById,omitempty"` // su party, si la tiene
	CorrectedAt   time.Time `json:"correctedAt"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (PartyIdentityCorrectedV1) IntegrationEventType() string { return "parties.identity-corrected.v1" }

// DataProtectionConsentRecordedV1 se publica cuando una empresa registra un consentimiento de
// protección de datos de un tercero en su pareja (D-122).
type DataProtectionConsentRecordedV1 struct {
	RelationshipID string    `json:"relationshipId"`
	ConsentID      string    `json:"consentId"`
	Organization   string    `json:"organization"` // la empresa, responsable del tratamiento
	PartyID        string    `json:"partyId"`      // el tercero
	LegalBasis     string    `json:"legalBasis"`   // consent | contract | legal-obligation | vital-interests | public-task | legitimate-interests
	Purpose        string    `json:"purpose"`
	GrantedAt      time.Time `json:"grantedAt"`
	Objection      bool      `json:"objection"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (DataProtectionConsentRecordedV1) IntegrationEventType() string {
	return "parties.data-protection-consent-recorded.v1"
}

// DataProtectionConsentWithdrawnV1 se publica cuando se retira un consentimiento.
type DataProtectionConsentWithdrawnV1 struct {
	RelationshipID string    `json:"relationshipId"`
	ConsentID      string    `json:"consentId"`
	Organization   string    `json:"organization"`
	PartyID        string    `json:"partyId"`
	Purpose        string    `json:"purpose"`
	WithdrawnAt    time.Time `json:"withdrawnAt"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (DataProtectionConsentWithdrawnV1) IntegrationEventType() string {
	return "parties.data-protection-consent-withdrawn.v1"
}

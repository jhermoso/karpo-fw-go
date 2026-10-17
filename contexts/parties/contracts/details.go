package contracts

import (
	"context"
	"time"
)

// ProspectTrialChangedV1 se publica cuando la prueba gratuita que una organización interna
// concede a un prospect se concede, se amplía, se acorta o se retira (TrialUntil ausente). El fin
// de la relación misma es RelationshipTerminatedV1: una prueba sólo está en vigor mientras lo
// está la relación de prospect.
type ProspectTrialChangedV1 struct {
	RelationshipID string     `json:"relationshipId"`
	Prospect       string     `json:"prospect"`
	Organization   string     `json:"organization"`
	TrialUntil     *time.Time `json:"trialUntil,omitempty"`
	ChangedAt      time.Time  `json:"changedAt"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (ProspectTrialChangedV1) IntegrationEventType() string {
	return "parties.prospect-trial-changed.v1"
}

// OwnershipShareChangedV1 se publica cuando la participación directa de un accionista en una
// organización se anota, se corrige o se borra (Share ausente). Share va en puntos con dos
// decimales («30.00» es el 30 %).
type OwnershipShareChangedV1 struct {
	RelationshipID string    `json:"relationshipId"`
	Shareholder    string    `json:"shareholder"`
	Organization   string    `json:"organization"`
	Share          string    `json:"share,omitempty"`
	ChangedAt      time.Time `json:"changedAt"`
}

// IntegrationEventType implementa application.IntegrationEvent.
func (OwnershipShareChangedV1) IntegrationEventType() string {
	return "parties.ownership-share-changed.v1"
}

// Trial es la prueba gratuita de un prospect con una organización interna.
type Trial struct {
	PartyID        string     `json:"partyId"`
	RelationshipID string     `json:"relationshipId"`
	Since          time.Time  `json:"since"`
	Until          *time.Time `json:"until,omitempty"` // ausente: no hay prueba concedida
	InForce        bool       `json:"inForce"`
}

// Collaborator es a quién pertenece un código de promoción.
type Collaborator struct {
	PartyID        string `json:"partyId"`
	RelationshipID string `json:"relationshipId"`
	PromotionCode  string `json:"promotionCode"`
}

// Collaborators dice de quién es un código de promoción: el colaborador de una organización
// interna que lo tiene ahora. Los códigos se comparan sin distinguir mayúsculas.
type Collaborators interface {
	ByPromotionCode(ctx context.Context, organization, code string) (Collaborator, bool, error)
}

// Trials responde si la prueba de un prospect sigue en vigor (lo que pregunta un contexto de
// suscripciones antes de dejar trabajar a un prospect), en lotes de como mucho MaxDirectoryBatch
// ids. Una party sin relación de prospect vigente con la organización no aparece en el resultado.
type Trials interface {
	Trials(ctx context.Context, organization string, partyIDs []string) (map[string]Trial, error)
}

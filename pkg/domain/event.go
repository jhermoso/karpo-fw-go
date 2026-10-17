package domain

import "time"

// Event es un hecho ocurrido en el dominio. Los eventos concretos son tipos valor inmutables que
// incrustan EventMeta y declaran su nombre de tipo estable con un método de receptor valor:
//
//	type PartyRegistered struct {
//		domain.EventMeta
//		TaxID string `json:"taxId"`
//	}
//	func (PartyRegistered) EventType() string { return "parties.party_registered" }
//
// El propio struct del evento es la carga; el outbox lo serializa como JSON.
type Event interface {
	// EventType es el nombre estable que se usa para el enrutado, la serialización y las suscripciones.
	EventType() string
	// Meta devuelve los metadatos del sobre (id, marca de tiempo, agregado de origen).
	Meta() EventMeta
}

// EventMeta es el sobre de metadatos que comparten todos los eventos de dominio.
type EventMeta struct {
	EventID          string    `json:"eventId"`
	OccurredAt       time.Time `json:"occurredAt"`
	AggregateType    string    `json:"aggregateType,omitempty"`
	AggregateID      string    `json:"aggregateId,omitempty"`
	AggregateVersion int64     `json:"aggregateVersion,omitempty"`
}

// Meta devuelve m. Incrustar EventMeta da a los eventos concretos su método Meta.
func (m EventMeta) Meta() EventMeta { return m }

// NewEventMeta devuelve metadatos con un id nuevo y la hora actual del dominio.
// Dentro de un agregado es preferible BaseAggregateRoot.NewEventMeta, que rellena además los campos del agregado.
func NewEventMeta() EventMeta {
	return EventMeta{EventID: NewUUID().String(), OccurredAt: Now()}
}

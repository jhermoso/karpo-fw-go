package domain

import "time"

// Event is a fact that happened in the domain. Concrete events are immutable value types that
// embed EventMeta and declare their stable type name with a method on the value receiver:
//
//	type PartyRegistered struct {
//		domain.EventMeta
//		TaxID string `json:"taxId"`
//	}
//	func (PartyRegistered) EventType() string { return "parties.party_registered" }
//
// The event struct itself is the payload; it is serialized as JSON by the outbox.
type Event interface {
	// EventType is the stable name used for routing, serialization and subscriptions.
	EventType() string
	// Meta returns the envelope metadata (id, timestamp, originating aggregate).
	Meta() EventMeta
}

// EventMeta is the metadata envelope shared by all domain events.
type EventMeta struct {
	EventID          string    `json:"eventId"`
	OccurredAt       time.Time `json:"occurredAt"`
	AggregateType    string    `json:"aggregateType,omitempty"`
	AggregateID      string    `json:"aggregateId,omitempty"`
	AggregateVersion int64     `json:"aggregateVersion,omitempty"`
}

// Meta returns m. Embedding EventMeta gives concrete events their Meta method.
func (m EventMeta) Meta() EventMeta { return m }

// NewEventMeta returns metadata with a fresh id and the current domain time.
// Inside an aggregate prefer BaseAggregateRoot.NewEventMeta, which also fills the aggregate fields.
func NewEventMeta() EventMeta {
	return EventMeta{EventID: NewUUID().String(), OccurredAt: Now()}
}

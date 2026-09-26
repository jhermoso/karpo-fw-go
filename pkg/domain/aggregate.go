package domain

import "fmt"

// AggregateRoot is the consistency boundary of a cluster of entities and value objects.
// Only aggregate roots have repositories.
//
// The interface is sealed: its unexported methods can only be satisfied by embedding
// BaseAggregateRoot, which guarantees every aggregate carries a version (optimistic
// concurrency) and an event buffer.
type AggregateRoot[ID Identifier] interface {
	Entity[ID]
	// AggregateType is the stable, persisted name of the aggregate kind (e.g. "parties.party").
	AggregateType() string
	// Version is the persisted version: 0 means "never persisted".
	Version() int64
	// PendingEvents returns the domain events raised since the aggregate was loaded or last saved.
	PendingEvents() []Event
	// ClearEvents discards the pending events once they are safely recorded/dispatched.
	ClearEvents()

	aggregateRoot()
	setVersion(v int64)
}

// BaseAggregateRoot is the embeddable implementation of AggregateRoot. Embed it by value and
// use a pointer receiver for the aggregate type (e.g. *Party implements AggregateRoot[PartyID]).
type BaseAggregateRoot[ID Identifier] struct {
	BaseEntity[ID]
	kind    string
	version int64
	events  []Event
}

// NewBaseAggregateRoot validates the identity and kind of a new or reconstituted aggregate.
func NewBaseAggregateRoot[ID Identifier](kind string, id ID) (BaseAggregateRoot[ID], error) {
	if kind == "" {
		return BaseAggregateRoot[ID]{}, fmt.Errorf("%w: aggregate kind must not be empty", ErrInvalidIdentity)
	}
	base, err := NewBaseEntity(id)
	if err != nil {
		return BaseAggregateRoot[ID]{}, err
	}
	return BaseAggregateRoot[ID]{BaseEntity: base, kind: kind}, nil
}

// AggregateType returns the aggregate kind.
func (a *BaseAggregateRoot[ID]) AggregateType() string { return a.kind }

// Version returns the persisted version (0 when new).
func (a *BaseAggregateRoot[ID]) Version() int64 { return a.version }

// IsNew reports whether the aggregate has never been persisted.
func (a *BaseAggregateRoot[ID]) IsNew() bool { return a.version == 0 }

// Raise records a domain event. Call it from aggregate behaviour methods after the state change.
func (a *BaseAggregateRoot[ID]) Raise(evt Event) {
	if evt != nil {
		a.events = append(a.events, evt)
	}
}

// NewEventMeta returns event metadata pre-filled with this aggregate's identity, kind and the
// version the aggregate will have once the pending changes are saved.
func (a *BaseAggregateRoot[ID]) NewEventMeta() EventMeta {
	m := NewEventMeta()
	m.AggregateType = a.kind
	m.AggregateID = a.ID().String()
	m.AggregateVersion = a.version + 1
	return m
}

// PendingEvents returns a copy of the events raised and not yet cleared.
func (a *BaseAggregateRoot[ID]) PendingEvents() []Event {
	return append([]Event(nil), a.events...)
}

// ClearEvents empties the pending events.
func (a *BaseAggregateRoot[ID]) ClearEvents() { a.events = nil }

func (a *BaseAggregateRoot[ID]) aggregateRoot()     {}
func (a *BaseAggregateRoot[ID]) setVersion(v int64) { a.version = v }

// MarkPersisted sets the persisted version of an aggregate. It is reserved for repository
// implementations (after an insert/update, or when reconstituting from storage).
func MarkPersisted[ID Identifier](agg AggregateRoot[ID], version int64) {
	agg.setVersion(version)
}

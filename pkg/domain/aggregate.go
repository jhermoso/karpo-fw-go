package domain

import (
	"github.com/jhermoso/karpo-fw-go/pkg/events"
)

// AggregateRoot represents the entry point and consistency boundary for an aggregate.
// It is an Entity that tracks domain events raised during business state mutations.
type AggregateRoot[ID comparable] interface {
	Entity[ID]

	// DomainEvents returns the uncommitted domain events emitted by this aggregate.
	DomainEvents() []events.Event

	// AddDomainEvent appends a new domain event to be dispatched when the aggregate is saved.
	AddDomainEvent(evt events.Event)

	// ClearDomainEvents clears all accumulated events after they have been persisted/dispatched.
	ClearDomainEvents()
}

// BaseAggregateRoot provides an embeddable implementation of AggregateRoot.
type BaseAggregateRoot[ID comparable] struct {
	BaseEntity[ID]
	events []events.Event
}

// NewBaseAggregateRoot initializes a BaseAggregateRoot with the specified ID.
func NewBaseAggregateRoot[ID comparable](id ID) BaseAggregateRoot[ID] {
	return BaseAggregateRoot[ID]{
		BaseEntity: NewBaseEntity(id),
		events:     make([]events.Event, 0),
	}
}

// DomainEvents returns a copy of the pending domain events.
func (a *BaseAggregateRoot[ID]) DomainEvents() []events.Event {
	return append([]events.Event(nil), a.events...)
}

// AddDomainEvent registers a new domain event on the aggregate.
func (a *BaseAggregateRoot[ID]) AddDomainEvent(evt events.Event) {
	if evt != nil {
		a.events = append(a.events, evt)
	}
}

// ClearDomainEvents empties the list of pending domain events.
func (a *BaseAggregateRoot[ID]) ClearDomainEvents() {
	a.events = make([]events.Event, 0)
}

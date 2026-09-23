// Package domain defines tactical Domain-Driven Design (DDD) primitives.
// It contains core abstractions for Entities, Value Objects, Aggregate Roots, and Specifications.
package domain

// Entity represents a domain concept defined by its persistent identity rather than its attributes.
type Entity[ID comparable] interface {
	// ID returns the unique identity of this entity.
	ID() ID

	// Equals checks if two entities share the same identity.
	Equals(other Entity[ID]) bool
}

// BaseEntity provides a generic, embeddable foundation implementing identity-based equality.
type BaseEntity[ID comparable] struct {
	id ID
}

// NewBaseEntity initializes a BaseEntity with the specified ID.
func NewBaseEntity[ID comparable](id ID) BaseEntity[ID] {
	return BaseEntity[ID]{id: id}
}

// ID returns the entity's identifier.
func (e BaseEntity[ID]) ID() ID {
	return e.id
}

// Equals compares two entities based strictly on their identifier.
func (e BaseEntity[ID]) Equals(other Entity[ID]) bool {
	if other == nil {
		return false
	}
	return e.id == other.ID()
}

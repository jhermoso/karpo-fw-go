package domain

// ValueObject represents a descriptive aspect of the domain with no conceptual identity.
// Value objects are conceptually immutable and compared exclusively by their attribute values.
type ValueObject[T any] interface {
	// Equals checks if two value objects have identical attributes.
	Equals(other T) bool
}

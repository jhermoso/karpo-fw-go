package domain

// Specification encapsulates a business rule that can be evaluated against a candidate of type T.
// Specifications can be composed using boolean operators (And, Or, Not).
type Specification[T any] interface {
	// IsSatisfiedBy checks if candidate complies with this specification.
	IsSatisfiedBy(candidate T) bool

	// And returns a specification requiring both this and other to be satisfied.
	And(other Specification[T]) Specification[T]

	// Or returns a specification requiring either this or other to be satisfied.
	Or(other Specification[T]) Specification[T]

	// Not returns a specification that negates this specification.
	Not() Specification[T]
}

type baseSpec[T any] struct {
	predicate func(candidate T) bool
}

// NewSpec creates a Specification from a simple predicate function.
func NewSpec[T any](predicate func(candidate T) bool) Specification[T] {
	return &baseSpec[T]{predicate: predicate}
}

func (s *baseSpec[T]) IsSatisfiedBy(candidate T) bool {
	return s.predicate(candidate)
}

func (s *baseSpec[T]) And(other Specification[T]) Specification[T] {
	return NewSpec(func(candidate T) bool {
		return s.IsSatisfiedBy(candidate) && other.IsSatisfiedBy(candidate)
	})
}

func (s *baseSpec[T]) Or(other Specification[T]) Specification[T] {
	return NewSpec(func(candidate T) bool {
		return s.IsSatisfiedBy(candidate) || other.IsSatisfiedBy(candidate)
	})
}

func (s *baseSpec[T]) Not() Specification[T] {
	return NewSpec(func(candidate T) bool {
		return !s.IsSatisfiedBy(candidate)
	})
}

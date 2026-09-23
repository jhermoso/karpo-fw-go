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

// PageRequest specifies pagination and sorting parameters for queries.
type PageRequest struct {
	PageNumber int
	PageSize   int
	OrderBy    string
	Descending bool
}

// DefaultPageRequest returns a default 1-indexed first page with 20 items.
func DefaultPageRequest() PageRequest {
	return PageRequest{
		PageNumber: 1,
		PageSize:   20,
	}
}

// PagedResult represents a paginated subset of results with metadata.
type PagedResult[T any] struct {
	Items      []T
	TotalCount int
	PageNumber int
	PageSize   int
	TotalPages int
}

// NewPagedResult constructs a PagedResult and calculates TotalPages.
func NewPagedResult[T any](items []T, totalCount, pageNumber, pageSize int) PagedResult[T] {
	if pageSize <= 0 {
		pageSize = 20
	}
	totalPages := (totalCount + pageSize - 1) / pageSize
	if totalPages < 1 && totalCount > 0 {
		totalPages = 1
	}
	return PagedResult[T]{
		Items:      items,
		TotalCount: totalCount,
		PageNumber: pageNumber,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

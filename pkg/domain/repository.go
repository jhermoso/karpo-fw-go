package domain

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

// ReadRepository defines read-only query operations for an entity or aggregate root T.
// This interface represents the Query side of CQRS.
type ReadRepository[ID comparable, T any] interface {
	// FindByID retrieves an entity by its unique identifier.
	FindByID(ctx context.Context, id ID) result.Result[T]

	// FindAll retrieves all entities in this collection.
	FindAll(ctx context.Context) result.Result[[]T]

	// FindMatching retrieves all entities that satisfy the given domain specification.
	FindMatching(ctx context.Context, spec Specification[T]) result.Result[[]T]

	// FindPaged retrieves a paginated subset of entities matching spec according to pageReq.
	FindPaged(ctx context.Context, spec Specification[T], pageReq PageRequest) result.Result[PagedResult[T]]

	// Count returns the number of entities that satisfy spec (or all if spec is nil).
	Count(ctx context.Context, spec Specification[T]) result.Result[int]
}

// WriteRepository defines state-mutation persistence operations for an aggregate root T.
// This interface represents the Command side of CQRS.
type WriteRepository[ID comparable, T any] interface {
	// Save persists an entity (insert or update).
	Save(ctx context.Context, entity T) result.Result[T]

	// Delete removes an entity by its identifier.
	Delete(ctx context.Context, id ID) result.Result[bool]
}

// Repository combines ReadRepository and WriteRepository for aggregates managing their full lifecycle together.
type Repository[ID comparable, T any] interface {
	ReadRepository[ID, T]
	WriteRepository[ID, T]
}

// UnitOfWork coordinates transaction boundaries across repository operations.
type UnitOfWork interface {
	// Do executes fn within an atomic transactional boundary.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

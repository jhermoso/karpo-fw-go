// Package persistence provides domain-driven repository contracts and unit of work abstractions.
package persistence

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

// Repository defines the canonical CRUD and query contracts for an Aggregate Root T identified by ID.
type Repository[ID comparable, T any] interface {
	// FindByID retrieves an entity by its unique identifier.
	FindByID(ctx context.Context, id ID) result.Result[T]

	// FindAll retrieves all entities in this collection.
	FindAll(ctx context.Context) result.Result[[]T]

	// Save persists an entity (insert or update).
	Save(ctx context.Context, entity T) result.Result[T]

	// Delete removes an entity by ID.
	Delete(ctx context.Context, id ID) result.Result[bool]
}

// UnitOfWork coordinates transactions across multiple repository operations.
type UnitOfWork interface {
	// Do executes fn within a transactional boundary. If fn returns an error, the transaction is rolled back.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

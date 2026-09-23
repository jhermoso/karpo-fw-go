// Package memory provides an in-memory repository implementation for testing and development.
package memory

import (
	"context"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/persistence"
	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

// Repository is a thread-safe in-memory generic repository.
type Repository[ID comparable, T any] struct {
	mu     sync.RWMutex
	store  map[ID]T
	idFunc func(T) ID
}

// NewRepository creates a new generic in-memory repository.
func NewRepository[ID comparable, T any](idFunc func(T) ID) *Repository[ID, T] {
	return &Repository[ID, T]{
		store:  make(map[ID]T),
		idFunc: idFunc,
	}
}

func (r *Repository[ID, T]) FindByID(_ context.Context, id ID) result.Result[T] {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entity, found := r.store[id]
	if !found {
		return result.FailMsg[T]("entity with id '%v' not found", id)
	}
	return result.Ok(entity)
}

func (r *Repository[ID, T]) FindAll(_ context.Context) result.Result[[]T] {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]T, 0, len(r.store))
	for _, v := range r.store {
		items = append(items, v)
	}
	return result.Ok(items)
}

func (r *Repository[ID, T]) Save(_ context.Context, entity T) result.Result[T] {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := r.idFunc(entity)
	r.store[id] = entity
	return result.Ok(entity)
}

func (r *Repository[ID, T]) Delete(_ context.Context, id ID) result.Result[bool] {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, found := r.store[id]; !found {
		return result.FailMsg[bool]("entity with id '%v' not found", id)
	}
	delete(r.store, id)
	return result.Ok(true)
}

// MemoryUnitOfWork is a no-op UnitOfWork for in-memory operations.
type MemoryUnitOfWork struct{}

func (u *MemoryUnitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

var _ persistence.Repository[string, any] = (*Repository[string, any])(nil)
var _ persistence.UnitOfWork = (*MemoryUnitOfWork)(nil)

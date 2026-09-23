// Package memory provides an in-memory repository implementation for testing and development.
package memory

import (
	"context"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
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

func (r *Repository[ID, T]) FindMatching(_ context.Context, spec domain.Specification[T]) result.Result[[]T] {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matches := make([]T, 0)
	for _, v := range r.store {
		if spec == nil || spec.IsSatisfiedBy(v) {
			matches = append(matches, v)
		}
	}
	return result.Ok(matches)
}

func (r *Repository[ID, T]) FindPaged(ctx context.Context, spec domain.Specification[T], pageReq domain.PageRequest) result.Result[domain.PagedResult[T]] {
	allRes := r.FindMatching(ctx, spec)
	if allRes.IsFailure() {
		return result.Fail[domain.PagedResult[T]](allRes.Error())
	}
	matches := allRes.MustValue()
	totalCount := len(matches)

	pageSize := pageReq.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	pageNumber := pageReq.PageNumber
	if pageNumber <= 0 {
		pageNumber = 1
	}

	start := (pageNumber - 1) * pageSize
	if start >= totalCount {
		return result.Ok(domain.NewPagedResult([]T{}, totalCount, pageNumber, pageSize))
	}

	end := start + pageSize
	if end > totalCount {
		end = totalCount
	}

	pagedItems := matches[start:end]
	return result.Ok(domain.NewPagedResult(pagedItems, totalCount, pageNumber, pageSize))
}

func (r *Repository[ID, T]) Count(_ context.Context, spec domain.Specification[T]) result.Result[int] {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, v := range r.store {
		if spec == nil || spec.IsSatisfiedBy(v) {
			count++
		}
	}
	return result.Ok(count)
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
var _ domain.ReadRepository[string, any] = (*Repository[string, any])(nil)
var _ domain.WriteRepository[string, any] = (*Repository[string, any])(nil)
var _ domain.Repository[string, any] = (*Repository[string, any])(nil)
var _ persistence.UnitOfWork = (*MemoryUnitOfWork)(nil)
var _ domain.UnitOfWork = (*MemoryUnitOfWork)(nil)

package memory

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

type entry[T any] struct {
	agg     T
	version int64
}

// Repository is a thread-safe in-memory implementation of domain.Repository.
type Repository[ID domain.Identifier, T domain.AggregateRoot[ID]] struct {
	store *Store
	kind  string
	clone func(T) T

	mu   sync.RWMutex
	rows map[ID]entry[T]
}

// RepoOption configures a memory Repository.
type RepoOption[T any] func(*repoOptions[T])

type repoOptions[T any] struct {
	clone func(T) T
}

// WithClone sets the function used to isolate stored aggregates from callers; it must return a
// deep copy. By default the repository makes a shallow copy of the aggregate struct, which is
// enough when aggregates replace (rather than mutate in place) their slices, maps and pointers.
func WithClone[T any](fn func(T) T) RepoOption[T] {
	return func(o *repoOptions[T]) { o.clone = fn }
}

// NewRepository creates a repository on store.
func NewRepository[ID domain.Identifier, T domain.AggregateRoot[ID]](store *Store, opts ...RepoOption[T]) *Repository[ID, T] {
	var o repoOptions[T]
	for _, opt := range opts {
		opt(&o)
	}
	if o.clone == nil {
		o.clone = shallowClone[T]
	}
	return &Repository[ID, T]{
		store: store,
		kind:  strings.TrimPrefix(reflect.TypeFor[T]().String(), "*"),
		clone: o.clone,
		rows:  make(map[ID]entry[T]),
	}
}

// shallowClone copies the struct pointed to by an aggregate pointer (including unexported
// fields); non-pointer aggregates are returned as is.
func shallowClone[T any](t T) T {
	v := reflect.ValueOf(t)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return t
	}
	c := reflect.New(v.Elem().Type())
	c.Elem().Set(v.Elem())
	return c.Interface().(T)
}

func (r *Repository[ID, T]) check(ctx context.Context) error {
	if r.store.closed.Load() {
		return ErrClosed
	}
	return ctx.Err()
}

// Get loads an aggregate by identity.
func (r *Repository[ID, T]) Get(ctx context.Context, id ID) (T, error) {
	var zero T
	if err := r.check(ctx); err != nil {
		return zero, err
	}
	r.mu.RLock()
	e, ok := r.rows[id]
	r.mu.RUnlock()
	if !ok {
		return zero, domain.NotFound(r.kind, id)
	}
	out := r.clone(e.agg)
	domain.MarkPersisted(out, e.version)
	return out, nil
}

// Save inserts or updates agg with optimistic concurrency.
func (r *Repository[ID, T]) Save(ctx context.Context, agg T) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	id, v := agg.ID(), agg.Version()

	r.mu.Lock()
	prev, exists := r.rows[id]
	switch {
	case v == 0 && exists:
		r.mu.Unlock()
		return domain.Conflict(r.kind, id, v, "identity already exists")
	case v != 0 && !exists:
		r.mu.Unlock()
		return domain.Conflict(r.kind, id, v, "aggregate no longer exists")
	case v != 0 && prev.version != v:
		r.mu.Unlock()
		return domain.Conflict(r.kind, id, v, "stale version")
	}
	domain.MarkPersisted(agg, v+1)
	stored := r.clone(agg)
	stored.ClearEvents() // pending events belong to the caller's instance, never to the store
	r.rows[id] = entry[T]{agg: stored, version: v + 1}
	r.mu.Unlock()

	r.store.onRollback(ctx, func() {
		r.mu.Lock()
		if exists {
			r.rows[id] = prev
		} else {
			delete(r.rows, id)
		}
		r.mu.Unlock()
		domain.MarkPersisted(agg, v)
	})
	return nil
}

// Delete removes agg with optimistic concurrency.
func (r *Repository[ID, T]) Delete(ctx context.Context, agg T) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	id, v := agg.ID(), agg.Version()

	r.mu.Lock()
	prev, exists := r.rows[id]
	switch {
	case !exists:
		r.mu.Unlock()
		return domain.NotFound(r.kind, id)
	case prev.version != v:
		r.mu.Unlock()
		return domain.Conflict(r.kind, id, v, "stale version")
	}
	delete(r.rows, id)
	r.mu.Unlock()

	r.store.onRollback(ctx, func() {
		r.mu.Lock()
		r.rows[id] = prev
		r.mu.Unlock()
	})
	return nil
}

func (r *Repository[ID, T]) matching(s spec.Specification[T], order []spec.Order[T]) []T {
	r.mu.RLock()
	out := make([]T, 0, len(r.rows))
	for _, e := range r.rows {
		if s == nil || s.IsSatisfiedBy(e.agg) {
			c := r.clone(e.agg)
			domain.MarkPersisted(c, e.version)
			out = append(out, c)
		}
	}
	r.mu.RUnlock()
	slices.SortFunc(out, func(a, b T) int {
		if c := spec.CompareAll(order, a, b); c != 0 {
			return c
		}
		return strings.Compare(a.ID().String(), b.ID().String())
	})
	return out
}

// Find returns the aggregates satisfying s, sorted by order then identity.
func (r *Repository[ID, T]) Find(ctx context.Context, s spec.Specification[T], order ...spec.Order[T]) ([]T, error) {
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	return r.matching(s, order), nil
}

// FindPage returns one page of matches.
func (r *Repository[ID, T]) FindPage(ctx context.Context, s spec.Specification[T], page domain.PageRequest[T]) (domain.Page[T], error) {
	if err := r.check(ctx); err != nil {
		return domain.Page[T]{}, err
	}
	page = page.Normalize()
	all := r.matching(s, page.Sort)
	start := min(page.Offset(), len(all))
	end := min(start+page.Size, len(all))
	return domain.NewPage(all[start:end], int64(len(all)), page.Number, page.Size), nil
}

// Count returns the number of matches.
func (r *Repository[ID, T]) Count(ctx context.Context, s spec.Specification[T]) (int64, error) {
	if err := r.check(ctx); err != nil {
		return 0, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var n int64
	for _, e := range r.rows {
		if s == nil || s.IsSatisfiedBy(e.agg) {
			n++
		}
	}
	return n, nil
}

// Exists reports whether any aggregate matches.
func (r *Repository[ID, T]) Exists(ctx context.Context, s spec.Specification[T]) (bool, error) {
	if err := r.check(ctx); err != nil {
		return false, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.rows {
		if s == nil || s.IsSatisfiedBy(e.agg) {
			return true, nil
		}
	}
	return false, nil
}

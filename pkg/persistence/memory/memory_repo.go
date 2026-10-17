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

// Repository es una implementación en memoria de domain.Repository, segura entre goroutines.
type Repository[ID domain.Identifier, T domain.AggregateRoot[ID]] struct {
	store *Store
	kind  string
	clone func(T) T

	mu   sync.RWMutex
	rows map[ID]entry[T]
}

// RepoOption configura un Repository en memoria.
type RepoOption[T any] func(*repoOptions[T])

type repoOptions[T any] struct {
	clone func(T) T
}

// WithClone fija la función que aísla los agregados guardados de quienes llaman al repositorio; debe devolver
// una copia profunda. Por defecto el repositorio hace una copia superficial del struct del
// agregado, que basta cuando los agregados sustituyen (en vez de modificar en el sitio) sus
// slices, mapas y punteros.
func WithClone[T any](fn func(T) T) RepoOption[T] {
	return func(o *repoOptions[T]) { o.clone = fn }
}

// NewRepository crea un repositorio sobre store.
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

// shallowClone copia el struct al que apunta un puntero a agregado (incluidos los campos no
// exportados); los agregados que no son punteros se devuelven tal cual.
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

// Get carga un agregado por su identidad.
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

// Save inserta o actualiza agg con concurrencia optimista.
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
	stored.ClearEvents() // los eventos pendientes son de la instancia de quien llama, nunca del almacén
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

// Delete elimina agg con concurrencia optimista.
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

// Find devuelve los agregados que cumplen s, ordenados por order y después por identidad.
func (r *Repository[ID, T]) Find(ctx context.Context, s spec.Specification[T], order ...spec.Order[T]) ([]T, error) {
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	return r.matching(s, order), nil
}

// FindPage devuelve una página de coincidencias.
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

// Count devuelve el número de coincidencias.
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

// Exists dice si algún agregado coincide.
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

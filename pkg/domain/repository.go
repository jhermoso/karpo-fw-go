package domain

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// ReadRepository is the query side of an aggregate repository.
//
// Contract every implementation must honour (verified by pkg/testing/repotest):
//   - specifications, ordering and paging are executed by the store, never by loading the whole
//     collection and filtering in memory; an expression the store cannot translate fails with
//     ErrUnsupported;
//   - a nil specification matches every aggregate;
//   - returned aggregates carry their persisted Version;
//   - Get returns an error matching ErrNotFound when the aggregate does not exist.
type ReadRepository[ID Identifier, T AggregateRoot[ID]] interface {
	// Get loads an aggregate by identity.
	Get(ctx context.Context, id ID) (T, error)

	// Find returns the aggregates satisfying s, sorted by order (then by identity).
	Find(ctx context.Context, s spec.Specification[T], order ...spec.Order[T]) ([]T, error)

	// FindPage returns one page of the aggregates satisfying s plus the total count.
	FindPage(ctx context.Context, s spec.Specification[T], page PageRequest[T]) (Page[T], error)

	// Count returns how many aggregates satisfy s.
	Count(ctx context.Context, s spec.Specification[T]) (int64, error)

	// Exists reports whether at least one aggregate satisfies s.
	Exists(ctx context.Context, s spec.Specification[T]) (bool, error)
}

// WriteRepository is the command side of an aggregate repository.
//
// Contract:
//   - Save inserts when agg.Version() == 0 and updates otherwise, using optimistic concurrency:
//     the stored version must equal agg.Version(), else an error matching ErrConflict is returned.
//     Inserting an identity that already exists also returns ErrConflict. On success the
//     aggregate version is incremented (see MarkPersisted);
//   - Save persists the whole aggregate (root and children) atomically;
//   - Delete removes the aggregate with the same optimistic concurrency check;
//   - both join the unit of work present in ctx, if any.
//
// Repositories never dispatch domain events: that is the application layer's job
// (application.Orchestrator + outbox), so events and state are committed together.
type WriteRepository[ID Identifier, T AggregateRoot[ID]] interface {
	Save(ctx context.Context, agg T) error
	Delete(ctx context.Context, agg T) error
}

// Repository is the full repository contract for an aggregate root.
type Repository[ID Identifier, T AggregateRoot[ID]] interface {
	ReadRepository[ID, T]
	WriteRepository[ID, T]
}

// UnitOfWork defines an atomic boundary. Do runs fn in a transaction bound to the returned
// context: every repository call made with that context joins it. The transaction commits when
// fn returns nil and rolls back when it returns an error or panics. Nested calls join the outer
// unit of work.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// UnitOfWorkFunc adapts a function into a UnitOfWork.
type UnitOfWorkFunc func(ctx context.Context, fn func(ctx context.Context) error) error

// Do calls f.
func (f UnitOfWorkFunc) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return f(ctx, fn)
}

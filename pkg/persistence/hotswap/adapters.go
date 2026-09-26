package hotswap

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Repository returns a domain.Repository that always delegates to the repository of the backend
// currently serving the operation (pinned unit of work, or the current backend).
//
//	parties := hotswap.Repository(sw, func(b hotswap.Backend) (domain.Repository[PartyID, *Party], error) {
//		switch db := b.(type) {
//		case *sqlrepo.DB:
//			return sqlrepo.NewRepository(db, partyMapping)
//		case *memory.Store:
//			return memory.NewRepository[PartyID, *Party](db), nil
//		}
//		return nil, fmt.Errorf("unsupported backend %T", b)
//	})
func Repository[ID domain.Identifier, T domain.AggregateRoot[ID]](
	s *Switch, factory func(Backend) (domain.Repository[ID, T], error),
) domain.Repository[ID, T] {
	return &repository[ID, T]{b: Bind(s, factory)}
}

type repository[ID domain.Identifier, T domain.AggregateRoot[ID]] struct {
	b *Binding[domain.Repository[ID, T]]
}

func (r *repository[ID, T]) Get(ctx context.Context, id ID) (out T, err error) {
	err = r.b.With(ctx, func(ctx context.Context, repo domain.Repository[ID, T]) error {
		out, err = repo.Get(ctx, id)
		return err
	})
	return out, err
}

func (r *repository[ID, T]) Find(ctx context.Context, s spec.Specification[T], order ...spec.Order[T]) (out []T, err error) {
	err = r.b.With(ctx, func(ctx context.Context, repo domain.Repository[ID, T]) error {
		out, err = repo.Find(ctx, s, order...)
		return err
	})
	return out, err
}

func (r *repository[ID, T]) FindPage(ctx context.Context, s spec.Specification[T], page domain.PageRequest[T]) (out domain.Page[T], err error) {
	err = r.b.With(ctx, func(ctx context.Context, repo domain.Repository[ID, T]) error {
		out, err = repo.FindPage(ctx, s, page)
		return err
	})
	return out, err
}

func (r *repository[ID, T]) Count(ctx context.Context, s spec.Specification[T]) (n int64, err error) {
	err = r.b.With(ctx, func(ctx context.Context, repo domain.Repository[ID, T]) error {
		n, err = repo.Count(ctx, s)
		return err
	})
	return n, err
}

func (r *repository[ID, T]) Exists(ctx context.Context, s spec.Specification[T]) (ok bool, err error) {
	err = r.b.With(ctx, func(ctx context.Context, repo domain.Repository[ID, T]) error {
		ok, err = repo.Exists(ctx, s)
		return err
	})
	return ok, err
}

func (r *repository[ID, T]) Save(ctx context.Context, agg T) error {
	return r.b.With(ctx, func(ctx context.Context, repo domain.Repository[ID, T]) error {
		return repo.Save(ctx, agg)
	})
}

func (r *repository[ID, T]) Delete(ctx context.Context, agg T) error {
	return r.b.With(ctx, func(ctx context.Context, repo domain.Repository[ID, T]) error {
		return repo.Delete(ctx, agg)
	})
}

// Outbox returns an application.OutboxStore that follows the Switch.
func Outbox(s *Switch, factory func(Backend) (application.OutboxStore, error)) application.OutboxStore {
	return &outbox{b: Bind(s, factory)}
}

type outbox struct {
	b *Binding[application.OutboxStore]
}

func (o *outbox) Append(ctx context.Context, msgs ...application.OutboxMessage) error {
	return o.b.With(ctx, func(ctx context.Context, x application.OutboxStore) error { return x.Append(ctx, msgs...) })
}

func (o *outbox) Pending(ctx context.Context, limit, maxAttempts int) (out []application.OutboxMessage, err error) {
	err = o.b.With(ctx, func(ctx context.Context, x application.OutboxStore) error {
		out, err = x.Pending(ctx, limit, maxAttempts)
		return err
	})
	return out, err
}

func (o *outbox) MarkProcessed(ctx context.Context, id string) error {
	return o.b.With(ctx, func(ctx context.Context, x application.OutboxStore) error { return x.MarkProcessed(ctx, id) })
}

func (o *outbox) MarkFailed(ctx context.Context, id string, cause error) error {
	return o.b.With(ctx, func(ctx context.Context, x application.OutboxStore) error { return x.MarkFailed(ctx, id, cause) })
}

var (
	_ domain.UnitOfWork       = (*Switch)(nil)
	_ application.OutboxStore = (*outbox)(nil)
)

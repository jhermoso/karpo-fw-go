package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/events"
	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

// Orchestrator coordinates the lifecycle of an Aggregate Root: loading, mutation, transactional persistence,
// and domain event dispatching.
type Orchestrator[ID comparable, T domain.AggregateRoot[ID]] struct {
	repo       domain.Repository[ID, T]
	uow        domain.UnitOfWork
	dispatcher events.Dispatcher
}

// NewOrchestrator creates a new Orchestrator instance.
func NewOrchestrator[ID comparable, T domain.AggregateRoot[ID]](
	repo domain.Repository[ID, T],
	uow domain.UnitOfWork,
	dispatcher events.Dispatcher,
) *Orchestrator[ID, T] {
	return &Orchestrator[ID, T]{
		repo:       repo,
		uow:        uow,
		dispatcher: dispatcher,
	}
}

// Create persists a newly instantiated aggregate, publishes its domain events, and clears them.
func (o *Orchestrator[ID, T]) Create(ctx context.Context, agg T) result.Result[T] {
	var saved T
	err := o.uow.Do(ctx, func(c context.Context) error {
		saveRes := o.repo.Save(c, agg)
		if saveRes.IsFailure() {
			return saveRes.Error()
		}
		saved = saveRes.MustValue()

		// Dispatch domain events
		for _, evt := range agg.DomainEvents() {
			if o.dispatcher != nil {
				if err := o.dispatcher.Publish(c, evt); err != nil {
					return err
				}
			}
		}
		agg.ClearDomainEvents()
		return nil
	})

	if err != nil {
		return result.Fail[T](err)
	}

	return result.Ok(saved)
}

// Mutate loads an aggregate by ID, executes the business mutation function, saves the aggregate within
// a UnitOfWork transaction, and publishes accumulated domain events.
func (o *Orchestrator[ID, T]) Mutate(
	ctx context.Context,
	id ID,
	mutateFn func(agg T) result.Result[any],
) result.Result[any] {
	findRes := o.repo.FindByID(ctx, id)
	if findRes.IsFailure() {
		return result.Fail[any](findRes.Error())
	}
	agg := findRes.MustValue()

	mutationRes := mutateFn(agg)
	if mutationRes.IsFailure() {
		return mutationRes
	}

	err := o.uow.Do(ctx, func(c context.Context) error {
		saveRes := o.repo.Save(c, agg)
		if saveRes.IsFailure() {
			return saveRes.Error()
		}

		for _, evt := range agg.DomainEvents() {
			if o.dispatcher != nil {
				if err := o.dispatcher.Publish(c, evt); err != nil {
					return err
				}
			}
		}
		agg.ClearDomainEvents()
		return nil
	})

	if err != nil {
		return result.Fail[any](err)
	}

	return mutationRes
}

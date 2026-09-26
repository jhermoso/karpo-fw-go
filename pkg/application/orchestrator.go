package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// EventRecorder durably records domain events inside the current unit of work
// (typically the transactional outbox, see Outbox).
type EventRecorder interface {
	Record(ctx context.Context, events []domain.Event) error
}

// Publisher delivers a domain event to subscribers. events.Dispatcher satisfies it.
type Publisher interface {
	Publish(ctx context.Context, evt domain.Event) error
}

// ErrEventsNotPublished reports that the state change was committed but some events could not be
// published in-process after the commit. The operation itself succeeded; use the outbox when
// event delivery must be guaranteed.
var ErrEventsNotPublished = errors.New("state committed but events not published")

// Orchestrator coordinates the lifecycle of an aggregate inside a unit of work:
// load -> execute behaviour -> save with optimistic concurrency -> record events -> commit.
// It is the Go counterpart of the C# OrchestratorService, fixing two issues of the naive
// approach: the aggregate is loaded inside the transaction, and events are recorded in the same
// transaction as the state (outbox) or published only after the commit.
type Orchestrator[ID domain.Identifier, T domain.AggregateRoot[ID]] struct {
	repo      domain.Repository[ID, T]
	uow       domain.UnitOfWork
	recorder  EventRecorder
	publisher Publisher
}

// OrchestratorOption configures an Orchestrator.
type OrchestratorOption func(*orchestratorConfig)

type orchestratorConfig struct {
	recorder  EventRecorder
	publisher Publisher
}

// WithOutbox records the aggregate's events through r inside the same unit of work as the state
// change (at-least-once delivery via an OutboxRelay).
func WithOutbox(r EventRecorder) OrchestratorOption {
	return func(c *orchestratorConfig) { c.recorder = r }
}

// WithPublisher publishes the aggregate's events in-process after a successful commit
// (best effort: failures return ErrEventsNotPublished but the state stays committed).
func WithPublisher(p Publisher) OrchestratorOption {
	return func(c *orchestratorConfig) { c.publisher = p }
}

// NewOrchestrator creates an Orchestrator.
func NewOrchestrator[ID domain.Identifier, T domain.AggregateRoot[ID]](
	repo domain.Repository[ID, T], uow domain.UnitOfWork, opts ...OrchestratorOption,
) *Orchestrator[ID, T] {
	var cfg orchestratorConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Orchestrator[ID, T]{repo: repo, uow: uow, recorder: cfg.recorder, publisher: cfg.publisher}
}

// Repository exposes the underlying repository (for queries).
func (o *Orchestrator[ID, T]) Repository() domain.Repository[ID, T] { return o.repo }

// Create persists a new aggregate and its events.
func (o *Orchestrator[ID, T]) Create(ctx context.Context, agg T) error {
	var evts []domain.Event
	err := o.uow.Do(ctx, func(ctx context.Context) error {
		var err error
		evts, err = o.commit(ctx, agg, false)
		return err
	})
	if err != nil {
		return err
	}
	return o.afterCommit(ctx, agg, evts)
}

// Update loads the aggregate, applies fn and saves it. It returns the updated aggregate.
func (o *Orchestrator[ID, T]) Update(ctx context.Context, id ID, fn func(ctx context.Context, agg T) error) (T, error) {
	return Execute(ctx, o, id, func(ctx context.Context, agg T) (T, error) {
		return agg, fn(ctx, agg)
	})
}

// Delete loads the aggregate, lets fn check rules and raise events (fn may be nil) and deletes it.
func (o *Orchestrator[ID, T]) Delete(ctx context.Context, id ID, fn func(ctx context.Context, agg T) error) error {
	var (
		agg  T
		evts []domain.Event
	)
	err := o.uow.Do(ctx, func(ctx context.Context) error {
		loaded, err := o.repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if fn != nil {
			if err := fn(ctx, loaded); err != nil {
				return err
			}
		}
		agg = loaded
		evts, err = o.commit(ctx, loaded, true)
		return err
	})
	if err != nil {
		return err
	}
	return o.afterCommit(ctx, agg, evts)
}

// Execute loads the aggregate id, runs fn (the business behaviour) and saves the aggregate, all
// inside one unit of work, returning fn's typed result. It is a function rather than a method
// because Go methods cannot declare their own type parameters.
func Execute[ID domain.Identifier, T domain.AggregateRoot[ID], R any](
	ctx context.Context, o *Orchestrator[ID, T], id ID, fn func(ctx context.Context, agg T) (R, error),
) (R, error) {
	var (
		result R
		agg    T
		evts   []domain.Event
	)
	err := o.uow.Do(ctx, func(ctx context.Context) error {
		loaded, err := o.repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if result, err = fn(ctx, loaded); err != nil {
			return err
		}
		agg = loaded
		evts, err = o.commit(ctx, loaded, false)
		return err
	})
	if err != nil {
		var zero R
		return zero, err
	}
	return result, o.afterCommit(ctx, agg, evts)
}

func (o *Orchestrator[ID, T]) commit(ctx context.Context, agg T, remove bool) ([]domain.Event, error) {
	evts := agg.PendingEvents()
	var err error
	if remove {
		err = o.repo.Delete(ctx, agg)
	} else {
		err = o.repo.Save(ctx, agg)
	}
	if err != nil {
		return nil, err
	}
	if len(evts) > 0 && o.recorder != nil {
		if err := o.recorder.Record(ctx, evts); err != nil {
			return nil, fmt.Errorf("recording events: %w", err)
		}
	}
	return evts, nil
}

func (o *Orchestrator[ID, T]) afterCommit(ctx context.Context, agg T, evts []domain.Event) error {
	agg.ClearEvents()
	if o.publisher == nil || len(evts) == 0 {
		return nil
	}
	var errs []error
	for _, evt := range evts {
		if err := o.publisher.Publish(ctx, evt); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrEventsNotPublished, errors.Join(errs...))
	}
	return nil
}

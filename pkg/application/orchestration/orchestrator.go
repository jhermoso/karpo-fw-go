// Package orchestration implements the aggregate Orchestrator (the Go counterpart of the C#
// OrchestratorService): load -> behaviour -> stamp audit -> save with optimistic concurrency ->
// record events and audit log, all inside one unit of work.
package orchestration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

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
	recorder  application.EventRecorder
	publisher application.Publisher
	audit     application.AuditLog
}

// Option configures an Orchestrator.
type Option func(*config)

type config struct {
	recorder  application.EventRecorder
	publisher application.Publisher
	audit     application.AuditLog
}

// WithAuditLog appends an application.AuditRecord for every create, update and delete, in the
// same unit of work as the state change: actor, channel and import run from the context, the
// raised events and, for aggregates implementing traits.Snapshotter, the changed fields.
func WithAuditLog(log application.AuditLog) Option {
	return func(c *config) { c.audit = log }
}

// WithOutbox records the aggregate's events through r inside the same unit of work as the state
// change (at-least-once delivery via an OutboxRelay).
func WithOutbox(r application.EventRecorder) Option {
	return func(c *config) { c.recorder = r }
}

// WithPublisher publishes the aggregate's events in-process after a successful commit
// (best effort: failures return ErrEventsNotPublished but the state stays committed).
func WithPublisher(p application.Publisher) Option {
	return func(c *config) { c.publisher = p }
}

// New creates an Orchestrator.
func New[ID domain.Identifier, T domain.AggregateRoot[ID]](
	repo domain.Repository[ID, T], uow domain.UnitOfWork, opts ...Option,
) *Orchestrator[ID, T] {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Orchestrator[ID, T]{repo: repo, uow: uow, recorder: cfg.recorder, publisher: cfg.publisher, audit: cfg.audit}
}

// Repository exposes the underlying repository (for queries).
func (o *Orchestrator[ID, T]) Repository() domain.Repository[ID, T] { return o.repo }

// Create persists a new aggregate and its events. Aggregates embedding traits.Audited are
// stamped with the actor of the context (application.ActorFrom) before being saved.
func (o *Orchestrator[ID, T]) Create(ctx context.Context, agg T) error {
	var evts []domain.Event
	err := o.uow.Do(ctx, func(ctx context.Context) error {
		var err error
		evts, err = o.commit(ctx, agg, application.AuditCreated, nil)
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
		before := snapshot(loaded)
		if fn != nil {
			if err := fn(ctx, loaded); err != nil {
				return err
			}
		}
		agg = loaded
		evts, err = o.commit(ctx, loaded, application.AuditDeleted, before)
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
		before := snapshot(loaded)
		if result, err = fn(ctx, loaded); err != nil {
			return err
		}
		agg = loaded
		evts, err = o.commit(ctx, loaded, application.AuditUpdated, before)
		return err
	})
	if err != nil {
		var zero R
		return zero, err
	}
	return result, o.afterCommit(ctx, agg, evts)
}

func (o *Orchestrator[ID, T]) commit(ctx context.Context, agg T, op string, before map[string]any) ([]domain.Event, error) {
	evts := agg.PendingEvents()
	actor := application.ActorFrom(ctx)
	now := domain.Now()
	var err error
	if op == application.AuditDeleted {
		err = o.repo.Delete(ctx, agg)
	} else {
		if a, ok := any(agg).(traits.Auditable); ok {
			traits.Stamp(a, actor, now)
		}
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
	if o.audit != nil {
		if err := o.audit.Append(ctx, auditRecord(ctx, agg, op, actor, now, before, evts)); err != nil {
			return nil, fmt.Errorf("recording audit: %w", err)
		}
	}
	return evts, nil
}

func snapshot(agg any) map[string]any {
	if s, ok := agg.(traits.Snapshotter); ok {
		return s.AuditSnapshot()
	}
	return nil
}

func auditRecord[ID domain.Identifier, T domain.AggregateRoot[ID]](
	ctx context.Context, agg T, op string, actor vocab.Actor, at time.Time, before map[string]any, evts []domain.Event,
) application.AuditRecord {
	r := application.AuditRecord{
		ID:               domain.NewUUID().String(),
		AggregateType:    agg.AggregateType(),
		AggregateID:      agg.ID().String(),
		AggregateVersion: agg.Version(),
		Operation:        op,
		Actor:            actor,
		Channel:          application.Channel(ctx),
		CorrelationID:    application.CorrelationID(ctx),
		At:               at,
	}
	if p, ok := application.ImportProvenanceFrom(ctx); ok {
		r.Import = &p
	}
	var after map[string]any
	if op != application.AuditDeleted {
		after = snapshot(agg)
	}
	if before != nil || after != nil {
		r.Changes = traits.Diff(before, after)
	}
	for _, e := range evts {
		r.Events = append(r.Events, e.EventType())
	}
	return r
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

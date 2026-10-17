// Package orchestration implementa el Orchestrator de agregados (la contrapartida en Go del
// OrchestratorService de C#): cargar -> comportamiento -> sellar auditoría -> guardar con concurrencia
// optimista -> registrar eventos y log de auditoría, todo dentro de una unidad de trabajo.
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

// ErrEventsNotPublished informa de que el cambio de estado se confirmó pero algunos eventos no se
// pudieron publicar en proceso tras el commit. La operación en sí salió bien; cuando la entrega de
// eventos debe estar garantizada se usa el outbox.
var ErrEventsNotPublished = errors.New("state committed but events not published")

// Orchestrator coordina el ciclo de vida de un agregado dentro de una unidad de trabajo:
// cargar -> ejecutar comportamiento -> guardar con concurrencia optimista -> registrar eventos -> commit.
// Es la contrapartida en Go del OrchestratorService de C# y corrige dos problemas del enfoque
// ingenuo: el agregado se carga dentro de la transacción, y los eventos se registran en la misma
// transacción que el estado (outbox) o se publican sólo después del commit.
type Orchestrator[ID domain.Identifier, T domain.AggregateRoot[ID]] struct {
	repo      domain.Repository[ID, T]
	uow       domain.UnitOfWork
	recorder  application.EventRecorder
	publisher application.Publisher
	audit     application.AuditLog
}

// Option configura un Orchestrator.
type Option func(*config)

type config struct {
	recorder  application.EventRecorder
	publisher application.Publisher
	audit     application.AuditLog
}

// WithAuditLog añade un application.AuditRecord por cada alta, modificación y baja, en la misma
// unidad de trabajo que el cambio de estado: actor, canal y ejecución de importación tomados del
// contexto, los eventos lanzados y, para los agregados que implementan traits.Snapshotter, los campos cambiados.
func WithAuditLog(log application.AuditLog) Option {
	return func(c *config) { c.audit = log }
}

// WithOutbox registra los eventos del agregado mediante r dentro de la misma unidad de trabajo que
// el cambio de estado (entrega al menos una vez mediante un OutboxRelay).
func WithOutbox(r application.EventRecorder) Option {
	return func(c *config) { c.recorder = r }
}

// WithPublisher publica los eventos del agregado en proceso tras un commit correcto
// (sin garantías: los fallos devuelven ErrEventsNotPublished pero el estado queda confirmado).
func WithPublisher(p application.Publisher) Option {
	return func(c *config) { c.publisher = p }
}

// New crea un Orchestrator.
func New[ID domain.Identifier, T domain.AggregateRoot[ID]](
	repo domain.Repository[ID, T], uow domain.UnitOfWork, opts ...Option,
) *Orchestrator[ID, T] {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Orchestrator[ID, T]{repo: repo, uow: uow, recorder: cfg.recorder, publisher: cfg.publisher, audit: cfg.audit}
}

// Repository expone el repositorio subyacente (para consultas).
func (o *Orchestrator[ID, T]) Repository() domain.Repository[ID, T] { return o.repo }

// Create persiste un agregado nuevo y sus eventos. Los agregados que incrustan traits.Audited se
// sellan con el actor del contexto (application.ActorFrom) antes de guardarse.
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

// Update carga el agregado, le aplica fn y lo guarda. Devuelve el agregado actualizado.
func (o *Orchestrator[ID, T]) Update(ctx context.Context, id ID, fn func(ctx context.Context, agg T) error) (T, error) {
	return Execute(ctx, o, id, func(ctx context.Context, agg T) (T, error) {
		return agg, fn(ctx, agg)
	})
}

// Delete carga el agregado, deja que fn compruebe reglas y lance eventos (fn puede ser nil) y lo borra.
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

// Execute carga el agregado id, ejecuta fn (el comportamiento de negocio) y guarda el agregado, todo
// dentro de una unidad de trabajo, y devuelve el resultado tipado de fn. Es una función y no un método
// porque los métodos de Go no pueden declarar sus propios parámetros de tipo.
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

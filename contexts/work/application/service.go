// Package application holds the Work use cases (with permissions and company scope) and the
// translation to the Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/work/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/work/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Works    domain.WorkRepository
	Times    domain.TimeEntryRepository
	UoW      fw.UnitOfWork
	Recorder app.EventRecorder
	Audit    app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	Open        app.CommandHandler[OpenWork, WorkDTO]
	Change      app.CommandHandler[ChangeWork, WorkDTO]
	Assign      app.CommandHandler[AssignPerson, WorkDTO]
	Release     app.CommandHandler[ReleasePerson, WorkDTO]
	Progress    app.CommandHandler[ProgressWork, WorkDTO]
	GetWork     app.QueryHandler[GetWork, WorkDTO]
	SearchWorks app.QueryHandler[SearchWorks, fw.Page[WorkDTO]]

	Record     app.CommandHandler[RecordTime, TimeDTO]
	Correct    app.CommandHandler[CorrectTime, TimeDTO]
	Withdraw   app.CommandHandler[WithdrawTime, struct{}]
	Approve    app.CommandHandler[ApproveTime, TimeDTO]
	SearchTime app.QueryHandler[SearchTime, fw.Page[TimeDTO]]
	Timesheet  app.QueryHandler[GetTimesheet, TimesheetDTO]
}

type service struct {
	Deps
	works *orchestration.Orchestrator[domain.WorkID, *domain.Work]
	times *orchestration.Orchestrator[domain.TimeEntryID, *domain.TimeEntry]
}

type scope struct {
	global bool
	ac     *authz.Context
	orgs   []domain.OrganizationID
}

func scopeOf(ctx context.Context) scope {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return scope{}
	}
	s := scope{global: ac.GlobalAdmin, ac: ac}
	for _, u := range ac.EffectiveOrganizations {
		s.orgs = append(s.orgs, domain.OrganizationID{UUID: u})
	}
	return s
}

// check returns a uniform 404 outside the company's scope and 403 when it is read-only.
func (s scope) check(kind string, id fmt.Stringer, org domain.OrganizationID, write bool) error {
	if !s.global && !slices.Contains(s.orgs, org) {
		return fw.NotFound(kind, id)
	}
	if write && !s.global && (s.ac == nil || !s.ac.CanWrite(org.UUID)) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

func within[T any](s scope, field spec.Field[T, domain.OrganizationID]) spec.Spec[T] {
	switch {
	case s.global:
		return spec.All[T]()
	case len(s.orgs) == 0:
		return spec.None[T]()
	}
	return field.In(s.orgs...)
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

func optionalID(v *fw.Validation, field, s string) fw.UUID {
	if s == "" {
		return fw.UUID{}
	}
	return parseID(v, field, s)
}

func parseDecimal(v *fw.Validation, field, s string) vocab.Decimal {
	if s == "" {
		return vocab.DecimalFromInt(0)
	}
	d, err := vocab.ParseDecimal(s)
	v.Require(err == nil, field, "format", field+" must be a decimal number")
	return d
}

func fixed(d vocab.Decimal) string { return d.StringFixed(2) }

func optID(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

func dateText(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func orToday(d vocab.Date) vocab.Date {
	if d.IsZero() {
		return vocab.DateOf(fw.Now())
	}
	return d
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func changing[In, Out any](uow fw.UnitOfWork, p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return guard(p, fn, pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond), pipeline.Transactional[In, Out](uow))
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	s := service{Deps: d, works: orchestration.New[domain.WorkID, *domain.Work](d.Works, d.UoW, opts...),
		times: orchestration.New[domain.TimeEntryID, *domain.TimeEntry](d.Times, d.UoW, opts...)}
	svc := &Service{}
	s.wireWorks(svc)
	s.wireTime(svc)
	return svc
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.WorkCompleted) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.WorkCompletedV1{WorkID: e.AggregateID, Company: e.Company, Code: e.Code, Kind: e.Kind, Customer: e.Customer,
			Started: e.Started, Finished: e.Finished, Hours: e.Hours, Cost: e.Cost}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.TimeApproved) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.TimeApprovedV1{EntryID: e.AggregateID, WorkID: e.Work, Company: e.Company, Person: e.Person, Date: e.Date,
			Hours: e.Hours, Cost: e.Cost, Billable: e.Billable}}, nil
	})
	return r
}

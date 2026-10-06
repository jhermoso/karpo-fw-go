package application

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/work/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PlanInput is the editable plan of a piece of work.
type PlanInput struct {
	Name           string     `json:"name"`
	Description    string     `json:"description,omitempty"`
	Purpose        string     `json:"purpose,omitempty"`
	Customer       string     `json:"customer,omitempty"`
	Facility       string     `json:"facility,omitempty"`
	Asset          string     `json:"asset,omitempty"`
	PlannedStart   vocab.Date `json:"plannedStart,omitzero"`
	PlannedEnd     vocab.Date `json:"plannedEnd,omitzero"`
	EstimatedHours string     `json:"estimatedHours,omitempty"`
	Budget         string     `json:"budget,omitempty"`
}

func (p PlanInput) plan(v *fw.Validation) domain.Plan {
	return domain.Plan{Name: p.Name, Description: p.Description, Purpose: p.Purpose, Customer: domain.PartyID{UUID: optionalID(v, "customer", p.Customer)},
		Facility: optionalID(v, "facility", p.Facility), Asset: optionalID(v, "asset", p.Asset), PlannedStart: p.PlannedStart, PlannedEnd: p.PlannedEnd,
		EstimatedHours: parseDecimal(v, "estimatedHours", p.EstimatedHours), Budget: parseDecimal(v, "budget", p.Budget)}
}

// OpenWork creates a piece of work of a company, optionally as part of another.
type OpenWork struct {
	Company string `json:"company"`
	Code    string `json:"code"`
	Kind    string `json:"kind"`
	Parent  string `json:"parent,omitempty"`
	PlanInput
	On vocab.Date `json:"on,omitzero"` // today by default
}

// ChangeWork replaces the plan of an open piece of work.
type ChangeWork struct {
	ID domain.WorkID `json:"-"`
	PlanInput
}

// AssignPerson puts a person on the work at an hourly rate.
type AssignPerson struct {
	ID     domain.WorkID `json:"-"`
	Person string        `json:"person"`
	Role   string        `json:"role,omitempty"`
	Rate   string        `json:"rate,omitempty"`
	From   vocab.Date    `json:"from,omitzero"` // today by default
}

// ReleasePerson takes a person off the work after a day.
type ReleasePerson struct {
	ID     domain.WorkID `json:"-"`
	Person string        `json:"person"`
	Until  vocab.Date    `json:"until,omitzero"` // today by default
}

// ProgressWork moves the status: scheduled, in-progress, on-hold, completed or cancelled.
type ProgressWork struct {
	ID     domain.WorkID `json:"-"`
	To     string        `json:"to"`
	On     vocab.Date    `json:"on,omitzero"` // today by default
	Reason string        `json:"reason,omitempty"`
}

// GetWork loads a piece of work with its people, history and hours.
type GetWork struct{ ID domain.WorkID }

// SearchWorks searches the work of the caller's scope.
type SearchWorks struct {
	Company, Kind, Status, Parent, Customer string
	Page, Size                              int
}

// AssignmentDTO is the transport form of an assignment.
type AssignmentDTO struct {
	Person string `json:"person"`
	Role   string `json:"role,omitempty"`
	Rate   string `json:"rate"`
	From   string `json:"from"`
	Until  string `json:"until,omitempty"`
}

// StepDTO is the transport form of a change of status.
type StepDTO struct {
	Status string `json:"status"`
	On     string `json:"on"`
}

// WorkDTO is the transport form of a piece of work. Hours and Cost are the approved time recorded
// against it; PendingHours the time not approved yet.
type WorkDTO struct {
	ID             string          `json:"id"`
	Company        string          `json:"company"`
	Code           string          `json:"code"`
	Kind           string          `json:"kind"`
	Parent         string          `json:"parent,omitempty"`
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	Purpose        string          `json:"purpose,omitempty"`
	Customer       string          `json:"customer,omitempty"`
	Facility       string          `json:"facility,omitempty"`
	Asset          string          `json:"asset,omitempty"`
	PlannedStart   string          `json:"plannedStart,omitempty"`
	PlannedEnd     string          `json:"plannedEnd,omitempty"`
	EstimatedHours string          `json:"estimatedHours"`
	Budget         string          `json:"budget"`
	Status         string          `json:"status"`
	Started        string          `json:"started,omitempty"`
	Finished       string          `json:"finished,omitempty"`
	CancelReason   string          `json:"cancelReason,omitempty"`
	Hours          string          `json:"hours,omitempty"`
	Cost           string          `json:"cost,omitempty"`
	PendingHours   string          `json:"pendingHours,omitempty"`
	Assignments    []AssignmentDTO `json:"assignments,omitempty"`
	History        []StepDTO       `json:"history,omitempty"`
	Version        int64           `json:"version"`
}

func workDTO(w *domain.Work, detail bool) WorkDTO {
	s := w.State()
	d := WorkDTO{ID: w.ID().String(), Company: s.Company.String(), Code: s.Code, Kind: string(s.Kind), Parent: optID(s.Parent.UUID), Name: s.Name,
		Description: s.Description, Purpose: s.Purpose, Customer: optID(s.Customer.UUID), Facility: optID(s.Facility), Asset: optID(s.Asset),
		PlannedStart: dateText(s.PlannedStart), PlannedEnd: dateText(s.PlannedEnd), EstimatedHours: fixed(s.EstimatedHours), Budget: fixed(s.Budget),
		Status: string(s.Status), Started: dateText(s.Started), Finished: dateText(s.Finished), CancelReason: s.CancelReason, Version: w.Version()}
	if s.Status == domain.Completed {
		d.Hours, d.Cost = fixed(s.Hours), fixed(s.Cost)
	}
	if detail {
		for _, a := range s.Assignments {
			d.Assignments = append(d.Assignments, AssignmentDTO{Person: a.Person.String(), Role: a.Role, Rate: fixed(a.Rate), From: a.From.String(), Until: dateText(a.Until)})
		}
		for _, h := range s.History {
			d.History = append(d.History, StepDTO{Status: string(h.Status), On: h.On.String()})
		}
	}
	return d
}

// recorded adds up the time of a piece of work: approved hours and their cost, and draft hours.
func (s service) recorded(ctx context.Context, id domain.WorkID) (hours, cost, pending vocab.Decimal, err error) {
	hours, cost, pending = vocab.DecimalFromInt(0), vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)
	es, err := s.Times.Find(ctx, domain.TimFieldWork.Eq(id))
	for _, e := range es {
		if e.State().Approved {
			hours, cost = hours.Add(e.State().Hours), cost.Add(e.Cost())
		} else {
			pending = pending.Add(e.State().Hours)
		}
	}
	return hours, cost, pending, err
}

func (s service) detail(ctx context.Context, w *domain.Work) (WorkDTO, error) {
	d := workDTO(w, true)
	hours, cost, pending, err := s.recorded(ctx, w.ID())
	if err != nil {
		return WorkDTO{}, err
	}
	d.Hours, d.Cost, d.PendingHours = fixed(hours), fixed(cost), fixed(pending)
	return d, nil
}

func (s service) wireWorks(svc *Service) {
	d := s.Deps
	svc.Open = changing(d.UoW, PermWorkUpdate, func(ctx context.Context, c OpenWork) (WorkDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		parent := domain.WorkID{UUID: optionalID(&v, "parent", c.Parent)}
		plan := c.plan(&v)
		if err := v.Err(); err != nil {
			return WorkDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return WorkDTO{}, err
		}
		if !parent.IsZero() {
			p, err := d.Works.Get(ctx, parent)
			if err != nil {
				return WorkDTO{}, err
			}
			if p.State().Company != company {
				return WorkDTO{}, fw.NotFound(domain.WorkKind, parent)
			}
			if !p.Open() {
				return WorkDTO{}, fw.Violation("work.closed", "the work it is part of is "+string(p.State().Status))
			}
		}
		w, err := domain.OpenWork(domain.NewWorkID(), company, c.Code, domain.Kind(strings.TrimSpace(c.Kind)), parent, plan, orToday(c.On))
		if err != nil {
			return WorkDTO{}, err
		}
		dup, err := d.Works.Exists(ctx, spec.And(domain.WrkFieldCompany.Eq(company), domain.WrkFieldCode.Eq(w.State().Code)))
		if err != nil {
			return WorkDTO{}, err
		}
		if dup {
			return WorkDTO{}, fw.Violation("work.duplicate_code", "the company already has work with that code")
		}
		if err := s.works.Create(ctx, w); err != nil {
			return WorkDTO{}, err
		}
		return s.detail(ctx, w)
	})

	update := func(ctx context.Context, id domain.WorkID, fn func(context.Context, *domain.Work) error) (WorkDTO, error) {
		sc := scopeOf(ctx)
		w, err := s.works.Update(ctx, id, func(ctx context.Context, w *domain.Work) error {
			if err := sc.check(domain.WorkKind, w.ID(), w.State().Company, true); err != nil {
				return err
			}
			return fn(ctx, w)
		})
		if err != nil {
			return WorkDTO{}, err
		}
		return s.detail(ctx, w)
	}
	svc.Change = changing(d.UoW, PermWorkUpdate, func(ctx context.Context, c ChangeWork) (WorkDTO, error) {
		var v fw.Validation
		plan := c.plan(&v)
		if err := v.Err(); err != nil {
			return WorkDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, w *domain.Work) error { return w.Change(plan) })
	})
	svc.Assign = changing(d.UoW, PermWorkUpdate, func(ctx context.Context, c AssignPerson) (WorkDTO, error) {
		var v fw.Validation
		person, rate := domain.PartyID{UUID: parseID(&v, "person", c.Person)}, parseDecimal(&v, "rate", c.Rate)
		if err := v.Err(); err != nil {
			return WorkDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, w *domain.Work) error { return w.Assign(person, c.Role, rate, orToday(c.From)) })
	})
	svc.Release = changing(d.UoW, PermWorkUpdate, func(ctx context.Context, c ReleasePerson) (WorkDTO, error) {
		var v fw.Validation
		person := domain.PartyID{UUID: parseID(&v, "person", c.Person)}
		if err := v.Err(); err != nil {
			return WorkDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, w *domain.Work) error { return w.Release(person, orToday(c.Until)) })
	})

	// Completing needs everything under the work closed and no time waiting for approval: the hours
	// and the cost of the work are those of its approved time.
	svc.Progress = changing(d.UoW, PermWorkProgress, func(ctx context.Context, c ProgressWork) (WorkDTO, error) {
		to := domain.Status(strings.TrimSpace(c.To))
		var v fw.Validation
		v.Require(slices.Contains(domain.Statuses, to) && to != domain.Created, "to", "enum", "scheduled, in-progress, on-hold, completed or cancelled")
		if err := v.Err(); err != nil {
			return WorkDTO{}, err
		}
		on := orToday(c.On)
		return update(ctx, c.ID, func(ctx context.Context, w *domain.Work) error {
			switch to {
			case domain.Scheduled:
				return w.Schedule(on)
			case domain.InProgress:
				return w.Start(on)
			case domain.OnHold:
				return w.Hold(on)
			case domain.Cancelled:
				return w.Cancel(on, c.Reason)
			}
			parts, err := d.Works.Find(ctx, domain.WrkFieldParent.Eq(w.ID()))
			if err != nil {
				return err
			}
			if slices.ContainsFunc(parts, (*domain.Work).Open) {
				return fw.Violation("work.open_parts", "work with open parts is not completed")
			}
			hours, cost, pending, err := s.recorded(ctx, w.ID())
			if err != nil {
				return err
			}
			if pending.IsPositive() {
				return fw.Violation("work.pending_time", "the work has time waiting for approval")
			}
			return w.Complete(on, hours, cost)
		})
	})

	svc.GetWork = guard(PermWorkRead, func(ctx context.Context, q GetWork) (WorkDTO, error) {
		w, err := d.Works.Get(ctx, q.ID)
		if err != nil {
			return WorkDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.WorkKind, w.ID(), w.State().Company, false); err != nil {
			return WorkDTO{}, err
		}
		return s.detail(ctx, w)
	})

	svc.SearchWorks = guard(PermWorkRead, func(ctx context.Context, q SearchWorks) (fw.Page[WorkDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Work]{within(scopeOf(ctx), domain.WrkFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.WrkFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Kind != "" {
			v.Require(slices.Contains(domain.Kinds, domain.Kind(q.Kind)), "kind", "enum", "a kind of work")
			parts = append(parts, domain.WrkFieldKind.Eq(q.Kind))
		}
		if q.Status != "" {
			v.Require(slices.Contains(domain.Statuses, domain.Status(q.Status)), "status", "enum", "a status")
			parts = append(parts, domain.WrkFieldStatus.Eq(q.Status))
		}
		if q.Parent != "" {
			parts = append(parts, domain.WrkFieldParent.Eq(domain.WorkID{UUID: parseID(&v, "parent", q.Parent)}))
		}
		if q.Customer != "" {
			parts = append(parts, domain.WrkFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", q.Customer)}))
		}
		if err := v.Err(); err != nil {
			return fw.Page[WorkDTO]{}, err
		}
		page, err := d.Works.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.WrkFieldCode.Asc()))
		if err != nil {
			return fw.Page[WorkDTO]{}, err
		}
		return fw.MapPage(page, func(w *domain.Work) WorkDTO { return workDTO(w, false) }), nil
	})
}

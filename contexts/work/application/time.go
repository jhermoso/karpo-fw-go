package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/work/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// RecordTime records the time a person worked on a piece of work in a day. Billable defaults to
// whether the work is for a customer.
type RecordTime struct {
	Work     string     `json:"work"`
	Person   string     `json:"person"`
	Date     vocab.Date `json:"date"`
	Hours    string     `json:"hours"`
	Billable *bool      `json:"billable,omitempty"`
	Comment  string     `json:"comment,omitempty"`
}

// CorrectTime changes a draft entry.
type CorrectTime struct {
	ID       domain.TimeEntryID `json:"-"`
	Hours    string             `json:"hours"`
	Billable bool               `json:"billable"`
	Comment  string             `json:"comment,omitempty"`
}

// WithdrawTime deletes a draft entry.
type WithdrawTime struct{ ID domain.TimeEntryID }

// ApproveTime makes an entry final.
type ApproveTime struct {
	ID domain.TimeEntryID `json:"-"`
}

// SearchTime searches the time of the caller's scope.
type SearchTime struct {
	Company, Work, Person, From, To string
	Pending                         bool // not approved yet
	Page, Size                      int
}

// GetTimesheet returns the time of a person of a company between two days.
type GetTimesheet struct{ Company, Person, From, To string }

// TimeDTO is the transport form of a time entry.
type TimeDTO struct {
	ID       string `json:"id"`
	Company  string `json:"company"`
	Work     string `json:"work"`
	Person   string `json:"person"`
	Date     string `json:"date"`
	Hours    string `json:"hours"`
	Rate     string `json:"rate"`
	Cost     string `json:"cost"`
	Billable bool   `json:"billable"`
	Comment  string `json:"comment,omitempty"`
	Approved bool   `json:"approved"`
	Version  int64  `json:"version"`
}

func timeDTO(t *domain.TimeEntry) TimeDTO {
	s := t.State()
	return TimeDTO{ID: t.ID().String(), Company: s.Company.String(), Work: s.Work.String(), Person: s.Person.String(), Date: s.Date.String(),
		Hours: fixed(s.Hours), Rate: fixed(s.Rate), Cost: fixed(t.Cost()), Billable: s.Billable, Comment: s.Comment, Approved: s.Approved,
		Version: t.Version()}
}

// TimesheetDTO is the time of a person in a period (the C# Timesheet was a header with neither
// period nor status: here it is a view of the entries).
type TimesheetDTO struct {
	Entries       []TimeDTO `json:"entries"`
	Hours         string    `json:"hours"`
	ApprovedHours string    `json:"approvedHours"`
	Cost          string    `json:"cost"`
}

// daily refuses more than 24 hours of a person in a day over all the work of the company.
func (s service) daily(ctx context.Context, company domain.OrganizationID, person domain.PartyID, on vocab.Date, except domain.TimeEntryID, hours vocab.Decimal) error {
	es, err := s.Times.Find(ctx, spec.And(domain.TimFieldCompany.Eq(company), domain.TimFieldPerson.Eq(person), domain.TimFieldDate.Eq(on)))
	if err != nil {
		return err
	}
	for _, e := range es {
		if e.ID() != except {
			hours = hours.Add(e.State().Hours)
		}
	}
	if hours.GreaterThan(vocab.DecimalFromInt(domain.MaxHoursADay)) {
		return fw.Violation("work.day_exceeded", "a person does not work more than 24 hours in a day")
	}
	return nil
}

func (s service) wireTime(svc *Service) {
	d := s.Deps
	svc.Record = changing(d.UoW, PermTimeRecord, func(ctx context.Context, c RecordTime) (TimeDTO, error) {
		var v fw.Validation
		wid, person := domain.WorkID{UUID: parseID(&v, "work", c.Work)}, domain.PartyID{UUID: parseID(&v, "person", c.Person)}
		hours := parseDecimal(&v, "hours", c.Hours)
		if err := v.Err(); err != nil {
			return TimeDTO{}, err
		}
		w, err := d.Works.Get(ctx, wid)
		if err != nil {
			return TimeDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.WorkKind, wid, w.State().Company, true); err != nil {
			return TimeDTO{}, err
		}
		billable := !w.State().Customer.IsZero()
		if c.Billable != nil {
			billable = *c.Billable
		}
		e, err := domain.RecordTime(domain.NewTimeEntryID(), w, person, c.Date, hours, billable, c.Comment)
		if err != nil {
			return TimeDTO{}, err
		}
		if err := s.daily(ctx, w.State().Company, person, c.Date, e.ID(), hours); err != nil {
			return TimeDTO{}, err
		}
		if err := s.times.Create(ctx, e); err != nil {
			return TimeDTO{}, err
		}
		return timeDTO(e), nil
	})

	update := func(ctx context.Context, id domain.TimeEntryID, fn func(context.Context, *domain.TimeEntry) error) (TimeDTO, error) {
		sc := scopeOf(ctx)
		e, err := s.times.Update(ctx, id, func(ctx context.Context, e *domain.TimeEntry) error {
			if err := sc.check(domain.TimeEntryKind, e.ID(), e.State().Company, true); err != nil {
				return err
			}
			return fn(ctx, e)
		})
		if err != nil {
			return TimeDTO{}, err
		}
		return timeDTO(e), nil
	}
	svc.Correct = changing(d.UoW, PermTimeRecord, func(ctx context.Context, c CorrectTime) (TimeDTO, error) {
		var v fw.Validation
		hours := parseDecimal(&v, "hours", c.Hours)
		if err := v.Err(); err != nil {
			return TimeDTO{}, err
		}
		return update(ctx, c.ID, func(ctx context.Context, e *domain.TimeEntry) error {
			if err := e.Correct(hours, c.Billable, c.Comment); err != nil {
				return err
			}
			st := e.State()
			return s.daily(ctx, st.Company, st.Person, st.Date, e.ID(), hours)
		})
	})
	svc.Approve = changing(d.UoW, PermTimeApprove, func(ctx context.Context, c ApproveTime) (TimeDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, e *domain.TimeEntry) error { return e.Approve() })
	})
	svc.Withdraw = changing(d.UoW, PermTimeRecord, func(ctx context.Context, c WithdrawTime) (struct{}, error) {
		sc := scopeOf(ctx)
		return struct{}{}, s.times.Delete(ctx, c.ID, func(_ context.Context, e *domain.TimeEntry) error {
			if err := sc.check(domain.TimeEntryKind, e.ID(), e.State().Company, true); err != nil {
				return err
			}
			return e.Withdraw()
		})
	})

	filter := func(ctx context.Context, v *fw.Validation, company, work, person, from, to string, pending bool) spec.Specification[*domain.TimeEntry] {
		parts := []spec.Specification[*domain.TimeEntry]{within(scopeOf(ctx), domain.TimFieldCompany)}
		if company != "" {
			parts = append(parts, domain.TimFieldCompany.Eq(domain.OrganizationID{UUID: parseID(v, "company", company)}))
		}
		if work != "" {
			parts = append(parts, domain.TimFieldWork.Eq(domain.WorkID{UUID: parseID(v, "work", work)}))
		}
		if person != "" {
			parts = append(parts, domain.TimFieldPerson.Eq(domain.PartyID{UUID: parseID(v, "person", person)}))
		}
		if from != "" {
			day, err := vocab.ParseDate(from)
			v.Require(err == nil, "from", "format", "from must be a date")
			parts = append(parts, domain.TimFieldDate.Ge(day))
		}
		if to != "" {
			day, err := vocab.ParseDate(to)
			v.Require(err == nil, "to", "format", "to must be a date")
			parts = append(parts, domain.TimFieldDate.Le(day))
		}
		if pending {
			parts = append(parts, domain.TimFieldApproved.Eq(false))
		}
		return spec.And(parts...)
	}
	svc.SearchTime = guard(PermTimeRead, func(ctx context.Context, q SearchTime) (fw.Page[TimeDTO], error) {
		var v fw.Validation
		sp := filter(ctx, &v, q.Company, q.Work, q.Person, q.From, q.To, q.Pending)
		if err := v.Err(); err != nil {
			return fw.Page[TimeDTO]{}, err
		}
		page, err := d.Times.FindPage(ctx, sp, fw.NewPageRequest(q.Page, q.Size, domain.TimFieldDate.Asc()))
		if err != nil {
			return fw.Page[TimeDTO]{}, err
		}
		return fw.MapPage(page, timeDTO), nil
	})
	svc.Timesheet = guard(PermTimeRead, func(ctx context.Context, q GetTimesheet) (TimesheetDTO, error) {
		var v fw.Validation
		v.Require(q.Company != "" && q.Person != "" && q.From != "" && q.To != "", "period", "required", "company, person, from and to are required")
		sp := filter(ctx, &v, q.Company, "", q.Person, q.From, q.To, false)
		if err := v.Err(); err != nil {
			return TimesheetDTO{}, err
		}
		es, err := d.Times.Find(ctx, sp, domain.TimFieldDate.Asc())
		if err != nil {
			return TimesheetDTO{}, err
		}
		out := TimesheetDTO{Entries: []TimeDTO{}}
		hours, approved, cost := vocab.DecimalFromInt(0), vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)
		for _, e := range es {
			out.Entries = append(out.Entries, timeDTO(e))
			hours, cost = hours.Add(e.State().Hours), cost.Add(e.Cost())
			if e.State().Approved {
				approved = approved.Add(e.State().Hours)
			}
		}
		out.Hours, out.ApprovedHours, out.Cost = fixed(hours), fixed(approved), fixed(cost)
		return out, nil
	})
}

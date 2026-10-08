package application

import (
	"context"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/hr/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Staff implements contracts.Staff (one query per batch; it serves contexts, not users).
type Staff struct{ Employments domain.EmploymentRepository }

var _ contracts.Staff = Staff{}

func tooMany(n int) error {
	if n > contracts.MaxBatch {
		return fmt.Errorf("%w: at most %d ids per call", fw.ErrValidation, contracts.MaxBatch)
	}
	return nil
}

// EmploymentsOn implements contracts.Staff: the employments active on the date, with their
// primary contract on that date.
func (s Staff) EmploymentsOn(ctx context.Context, personIDs []string, date string) (map[string][]contracts.EmploymentRef, error) {
	if err := tooMany(len(personIDs)); err != nil {
		return nil, err
	}
	on, err := vocab.ParseDate(date)
	if err != nil {
		return nil, fmt.Errorf("%w: a date YYYY-MM-DD is required", fw.ErrValidation)
	}
	var people []domain.PersonID
	for _, id := range personIDs {
		if u, err := fw.ParseUUID(id); err == nil && !u.IsZero() {
			people = append(people, domain.PersonID{UUID: u})
		}
	}
	out := map[string][]contracts.EmploymentRef{}
	if len(people) == 0 {
		return out, nil
	}
	es, err := s.Employments.Find(ctx, domain.EmpFieldPerson.In(people...))
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		if !e.ActiveOn(on) {
			continue
		}
		ref := contracts.EmploymentRef{ID: e.ID().String(), Person: e.Person().String(), Employer: e.Employer().String(), Number: e.Number(),
			Hired: e.Hired().String(), Terminated: dateText(e.Terminated()), JobCategory: e.JobCategory(), WageGroup: e.WageGroup()}
		if c, ok := e.PrimaryContractOn(on); ok {
			d := contractDTO(c)
			ref.Contract = &contracts.ContractRef{ID: d.ID, TypeCode: d.TypeCode, Start: d.Start, End: d.End, Agreement: d.Agreement,
				WorkCenter: d.WorkCenter, WeeklyHours: d.WeeklyHours}
		}
		out[ref.Person] = append(out[ref.Person], ref)
	}
	return out, nil
}

// PositionDirectory implements contracts.Positions.
type PositionDirectory struct{ Positions domain.PositionRepository }

var _ contracts.Positions = PositionDirectory{}

func positionRef(p *domain.Position, now time.Time) contracts.PositionRef {
	r := contracts.PositionRef{ID: p.ID().String(), Unit: p.Unit().String(), Organization: p.Organization().String(), Type: p.Type().String(),
		Active: p.Status() != domain.StatusInactive}
	if h, ok := p.HolderAt(now); ok {
		r.Holder = h.String()
	}
	if sup, ok := p.PrimarySupervisorAt(now); ok {
		r.ReportsTo = sup.String()
	}
	return r
}

// Resolve implements contracts.Positions.
func (d PositionDirectory) Resolve(ctx context.Context, ids []string) (map[string]contracts.PositionRef, error) {
	if err := tooMany(len(ids)); err != nil {
		return nil, err
	}
	var want []domain.PositionID
	for _, s := range ids {
		if id, err := domain.ParsePositionID(s); err == nil && !id.IsZero() {
			want = append(want, id)
		}
	}
	out := map[string]contracts.PositionRef{}
	if len(want) == 0 {
		return out, nil
	}
	ps, err := d.Positions.Find(ctx, domain.PositionsWithIDs(want...))
	if err != nil {
		return nil, err
	}
	now := fw.Now()
	for _, p := range ps {
		out[p.ID().String()] = positionRef(p, now)
	}
	return out, nil
}

// HeldBy implements contracts.Positions.
func (d PositionDirectory) HeldBy(ctx context.Context, personID string) ([]contracts.PositionRef, error) {
	u, err := fw.ParseUUID(personID)
	if err != nil {
		return nil, fmt.Errorf("%w: person must be an id", fw.ErrValidation)
	}
	now := fw.Now()
	ps, err := d.Positions.Find(ctx, domain.HeldBy(domain.PersonID{UUID: u}, now))
	if err != nil {
		return nil, err
	}
	out := make([]contracts.PositionRef, 0, len(ps))
	for _, p := range ps {
		out = append(out, positionRef(p, now))
	}
	return out, nil
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	one := func(e app.IntegrationEvent) ([]app.IntegrationEvent, error) { return []app.IntegrationEvent{e}, nil }
	messaging.On(r, func(_ context.Context, e domain.EmployeeHired) ([]app.IntegrationEvent, error) {
		return one(contracts.EmployeeHiredV1{EmploymentID: e.AggregateID, Person: e.Person, Employer: e.Employer, Number: e.Number, Hired: e.Hired})
	})
	messaging.On(r, func(_ context.Context, e domain.EmployeeTerminated) ([]app.IntegrationEvent, error) {
		return one(contracts.EmployeeTerminatedV1{EmploymentID: e.AggregateID, Person: e.Person, Employer: e.Employer,
			Terminated: e.Terminated, Reason: e.Reason})
	})
	messaging.On(r, func(_ context.Context, e domain.ContractStarted) ([]app.IntegrationEvent, error) {
		return one(contracts.ContractStartedV1{EmploymentID: e.AggregateID, ContractID: e.Contract, TypeCode: e.TypeCode, Start: e.Start,
			Agreement: e.Agreement, WorkCenter: e.WorkCenter, Primary: e.Primary})
	})
	messaging.On(r, func(_ context.Context, e domain.ContractEnded) ([]app.IntegrationEvent, error) {
		return one(contracts.ContractEndedV1{EmploymentID: e.AggregateID, ContractID: e.Contract, End: e.End, Reason: e.Reason})
	})
	messaging.On(r, func(_ context.Context, e domain.PositionFilled) ([]app.IntegrationEvent, error) {
		return one(contracts.PositionFilledV1{PositionID: e.AggregateID, Person: e.Holder, From: e.From.Format(time.RFC3339Nano)})
	})
	messaging.On(r, func(_ context.Context, e domain.PositionVacated) ([]app.IntegrationEvent, error) {
		return one(contracts.PositionVacatedV1{PositionID: e.AggregateID, Person: e.Holder, At: e.At.Format(time.RFC3339Nano)})
	})
	return r
}

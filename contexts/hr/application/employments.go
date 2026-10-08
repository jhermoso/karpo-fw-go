package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Hire hires a person affiliated with an internal organization.
type Hire struct {
	Person      string     `json:"person"`
	Employer    string     `json:"employer"`
	Number      string     `json:"number,omitempty"`
	Hired       vocab.Date `json:"hired"`
	JobCategory string     `json:"jobCategory,omitempty"`
	WageGroup   string     `json:"wageGroup,omitempty"`
}

// AddContract adds a labor contract to an employment.
type AddContract struct {
	ID          domain.EmploymentID `json:"-"`
	TypeCode    string              `json:"typeCode"`
	Number      string              `json:"number,omitempty"`
	Start       vocab.Date          `json:"start"`
	End         vocab.Date          `json:"end,omitzero"`
	Agreement   string              `json:"agreement"`
	WorkCenter  string              `json:"workCenter"`
	WeeklyHours string              `json:"weeklyHours,omitempty"`
	Bonified    bool                `json:"bonified"`
	Primary     bool                `json:"primary"`
}

// EndContract ends a labor contract.
type EndContract struct {
	ID       domain.EmploymentID `json:"-"`
	Contract string              `json:"contract"`
	End      vocab.Date          `json:"end"`
	Reason   string              `json:"reason,omitempty"`
}

// Terminate ends an employment, its contracts and the positions the person holds in the employer.
type Terminate struct {
	ID     domain.EmploymentID `json:"-"`
	On     vocab.Date          `json:"on"`
	Reason string              `json:"reason,omitempty"`
}

// SetClassification changes the job category and wage group.
type SetClassification struct {
	ID          domain.EmploymentID `json:"-"`
	JobCategory string              `json:"jobCategory"`
	WageGroup   string              `json:"wageGroup"`
}

// GetEmployment loads an employment.
type GetEmployment struct{ ID domain.EmploymentID }

// SearchEmployments searches employments of the caller's scope.
type SearchEmployments struct {
	Employer, Person, Number string
	ActiveOnly               bool
	Page, Size               int
}

// ContractDTO is the transport form of a labor contract.
type ContractDTO struct {
	ID                string `json:"id"`
	TypeCode          string `json:"typeCode"`
	Number            string `json:"number,omitempty"`
	Start             string `json:"start"`
	End               string `json:"end,omitempty"`
	Agreement         string `json:"agreement"`
	WorkCenter        string `json:"workCenter"`
	WeeklyHours       string `json:"weeklyHours,omitempty"`
	Bonified          bool   `json:"bonified"`
	Primary           bool   `json:"primary"`
	TerminationReason string `json:"terminationReason,omitempty"`
}

// EmploymentDTO is the transport form of an employment.
type EmploymentDTO struct {
	ID                string        `json:"id"`
	Person            string        `json:"person"`
	Employer          string        `json:"employer"`
	Number            string        `json:"number,omitempty"`
	Hired             string        `json:"hired"`
	Terminated        string        `json:"terminated,omitempty"`
	TerminationReason string        `json:"terminationReason,omitempty"`
	JobCategory       string        `json:"jobCategory,omitempty"`
	WageGroup         string        `json:"wageGroup,omitempty"`
	Contracts         []ContractDTO `json:"contracts"`
	Version           int64         `json:"version"`
	ModifiedBy        string        `json:"modifiedBy,omitempty"`
}

func parseDate(s string) (vocab.Date, error) { return vocab.ParseDate(s) }

func dateText(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func contractDTO(c domain.Contract) ContractDTO {
	d := ContractDTO{ID: c.ID.String(), TypeCode: c.TypeCode, Number: c.Number, Start: c.Start.String(), End: dateText(c.End),
		Agreement: c.Agreement.String(), WorkCenter: c.WorkCenter.String(), Bonified: c.Bonified, Primary: c.Primary,
		TerminationReason: c.TerminationReason}
	if !c.WeeklyHours.IsZero() {
		d.WeeklyHours = c.WeeklyHours.String()
	}
	return d
}

func employmentDTO(e *domain.Employment) EmploymentDTO {
	d := EmploymentDTO{ID: e.ID().String(), Person: e.Person().String(), Employer: e.Employer().String(), Number: e.Number(),
		Hired: e.Hired().String(), Terminated: dateText(e.Terminated()), TerminationReason: e.TerminationReason(),
		JobCategory: e.JobCategory(), WageGroup: e.WageGroup(), Contracts: []ContractDTO{}, Version: e.Version(),
		ModifiedBy: e.ModifiedBy().Name}
	for _, c := range e.Contracts() {
		d.Contracts = append(d.Contracts, contractDTO(c))
	}
	return d
}

func (s service) employmentUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.EmploymentID, fn func(context.Context, *domain.Employment) error) (EmploymentDTO, error) {
		sc := scopeOf(ctx)
		e, err := s.employments.Update(ctx, id, func(ctx context.Context, e *domain.Employment) error {
			if err := sc.check(domain.EmploymentKind, e.ID(), e.Employer(), true); err != nil {
				return err
			}
			return fn(ctx, e)
		})
		if err != nil {
			return EmploymentDTO{}, err
		}
		return employmentDTO(e), nil
	}

	svc.Hire = guard(PermEmploymentCreate, func(ctx context.Context, c Hire) (EmploymentDTO, error) {
		var v fw.Validation
		person := domain.PersonID{UUID: parseID(&v, "person", c.Person)}
		employer := domain.OrganizationID{UUID: parseID(&v, "employer", c.Employer)}
		if err := v.Err(); err != nil {
			return EmploymentDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", employer, employer, true); err != nil {
			return EmploymentDTO{}, err
		}
		if s.Organizations != nil {
			ok, err := s.Organizations.Affiliated(ctx, person, employer)
			if err != nil {
				return EmploymentDTO{}, err
			}
			if !ok {
				// The person is registered in Parties with an Employment affiliation first: that is
				// what makes them visible to the employer's users.
				return EmploymentDTO{}, fw.Violation("hr.not_affiliated", "the person is not affiliated with the employer")
			}
		}
		current, err := s.Employments.Find(ctx, domain.CurrentEmploymentsOf(person).And(domain.EmpFieldEmployer.Eq(employer)))
		if err != nil {
			return EmploymentDTO{}, err
		}
		if len(current) > 0 {
			return EmploymentDTO{}, fw.Violation("hr.already_employed", "the person is already an employee of the organization")
		}
		e, err := domain.Hire(domain.NewEmploymentID(), domain.EmploymentState{Person: person, Employer: employer, Number: c.Number,
			Hired: c.Hired, JobCategory: c.JobCategory, WageGroup: c.WageGroup}, vocab.DateOf(fw.Now()))
		if err != nil {
			return EmploymentDTO{}, err
		}
		if e.Number() != "" {
			same, err := s.Employments.Find(ctx, domain.EmpFieldEmployer.Eq(employer).And(domain.EmpFieldNumber.Eq(e.Number())))
			if err != nil {
				return EmploymentDTO{}, err
			}
			for _, o := range same {
				if o.Person() != person {
					return EmploymentDTO{}, fw.Violation("hr.duplicate_employee_number", "the employee number belongs to another person")
				}
			}
		}
		if err := s.employments.Create(ctx, e); err != nil {
			return EmploymentDTO{}, err
		}
		return employmentDTO(e), nil
	}, pipeline.Transactional[Hire, EmploymentDTO](s.tx()))

	svc.AddContract = guard(PermEmploymentUpdate, func(ctx context.Context, c AddContract) (EmploymentDTO, error) {
		var v fw.Validation
		agreementID := domain.AgreementID{UUID: parseID(&v, "agreement", c.Agreement)}
		wcID := domain.WorkCenterID{UUID: parseID(&v, "workCenter", c.WorkCenter)}
		var hours vocab.Decimal
		if c.WeeklyHours != "" {
			h, err := vocab.ParseDecimal(c.WeeklyHours)
			v.Require(err == nil, "weeklyHours", "format", "weekly hours must be a decimal number")
			hours = h
		}
		if err := v.Err(); err != nil {
			return EmploymentDTO{}, err
		}
		agreement, ok, err := s.agreement(ctx, agreementID)
		if err != nil {
			return EmploymentDTO{}, err
		}
		if !ok {
			v.Add("agreement", "unknown", "unknown collective agreement")
			return EmploymentDTO{}, v.Err()
		}
		return update(ctx, c.ID, func(ctx context.Context, e *domain.Employment) error {
			if !agreement.InForceOn(c.Start) {
				return fw.Violation("hr.agreement_not_in_force", "the collective agreement is not in force on the start date")
			}
			if agreement.Scope == domain.ScopeCompany && agreement.Organization != e.Employer() {
				return fw.Violation("hr.agreement_other_company", "the company agreement belongs to another organization")
			}
			wc, err := s.WorkCenters.Get(ctx, wcID)
			if err != nil {
				return err
			}
			if wc.Employer() != e.Employer() {
				return fw.NotFound(domain.WorkCenterKind, wcID)
			}
			if !wc.IsOpen() || c.Start.Before(wc.Opened()) {
				return fw.Violation("hr.work_center_closed", "the work center is not open on the start date")
			}
			_, err = e.AddContract(domain.Contract{TypeCode: c.TypeCode, Number: c.Number, Start: c.Start, End: c.End, Agreement: agreementID,
				WorkCenter: wcID, WeeklyHours: hours, Bonified: c.Bonified, Primary: c.Primary})
			return err
		})
	}, pipeline.Transactional[AddContract, EmploymentDTO](s.tx()))

	svc.EndContract = guard(PermEmploymentUpdate, func(ctx context.Context, c EndContract) (EmploymentDTO, error) {
		id, err := domain.ParseContractID(c.Contract)
		if err != nil {
			var v fw.Validation
			v.Add("contract", "format", "contract must be an id")
			return EmploymentDTO{}, v.Err()
		}
		return update(ctx, c.ID, func(_ context.Context, e *domain.Employment) error { return e.EndContract(id, c.End, c.Reason) })
	}, retry[EndContract, EmploymentDTO]())

	svc.Terminate = guard(PermEmploymentUpdate, func(ctx context.Context, c Terminate) (EmploymentDTO, error) {
		dto, err := update(ctx, c.ID, func(_ context.Context, e *domain.Employment) error { return e.Terminate(c.On, c.Reason) })
		if err != nil {
			return EmploymentDTO{}, err
		}
		// The person leaves the positions held in the employer at the end of the last day.
		leave := c.On.AddDays(1).BaseTime()
		person := domain.PersonID{UUID: fw.MustParseUUID(dto.Person)}
		held, err := s.Positions.Find(ctx, domain.PosFieldOrganization.Eq(domain.OrganizationID{UUID: fw.MustParseUUID(dto.Employer)}).And(
			domain.PosFieldHolders.Any(domain.HolderFieldPerson.Eq(person).And(domain.HolderFieldUntil.IsNull()))))
		if err != nil {
			return EmploymentDTO{}, err
		}
		for _, p := range held {
			if h, ok := p.HolderAt(leave.Add(-1)); !ok || h != person {
				continue // the holding starts later: HR decides what to do with it
			}
			if _, err := s.positions.Update(ctx, p.ID(), func(_ context.Context, p *domain.Position) error { return p.Vacate(leave) }); err != nil {
				return EmploymentDTO{}, err
			}
		}
		return dto, nil
	}, pipeline.Transactional[Terminate, EmploymentDTO](s.tx()))

	svc.SetClassification = guard(PermEmploymentUpdate, func(ctx context.Context, c SetClassification) (EmploymentDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, e *domain.Employment) error {
			return e.SetClassification(c.JobCategory, c.WageGroup)
		})
	}, retry[SetClassification, EmploymentDTO]())

	svc.GetEmployment = guard(PermEmploymentRead, func(ctx context.Context, q GetEmployment) (EmploymentDTO, error) {
		e, err := s.Employments.Get(ctx, q.ID)
		if err != nil {
			return EmploymentDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.EmploymentKind, e.ID(), e.Employer(), false); err != nil {
			return EmploymentDTO{}, err
		}
		return employmentDTO(e), nil
	})

	svc.SearchEmployments = guard(PermEmploymentRead, func(ctx context.Context, q SearchEmployments) (fw.Page[EmploymentDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Employment]{within(scopeOf(ctx), domain.EmpFieldEmployer)}
		if q.Employer != "" {
			parts = append(parts, domain.EmpFieldEmployer.Eq(domain.OrganizationID{UUID: parseID(&v, "employer", q.Employer)}))
		}
		if q.Person != "" {
			parts = append(parts, domain.EmpFieldPerson.Eq(domain.PersonID{UUID: parseID(&v, "person", q.Person)}))
		}
		if q.Number != "" {
			parts = append(parts, domain.EmpFieldNumber.Eq(q.Number))
		}
		if q.ActiveOnly {
			parts = append(parts, domain.EmpFieldEnded.Eq(false))
		}
		if err := v.Err(); err != nil {
			return fw.Page[EmploymentDTO]{}, err
		}
		page, err := s.Employments.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.EmpFieldNumber.Asc()))
		if err != nil {
			return fw.Page[EmploymentDTO]{}, err
		}
		return fw.MapPage(page, employmentDTO), nil
	})
}

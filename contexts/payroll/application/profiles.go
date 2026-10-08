package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// SalaryDTO is the transport form of the agreed salary.
type SalaryDTO struct {
	Amount          string `json:"amount"`
	Periodicity     string `json:"periodicity"` // monthly | annual
	PaymentsPerYear int    `json:"paymentsPerYear"`
}

// TermsInput are the editable terms of a profile.
type TermsInput struct {
	ContributionGroup int        `json:"contributionGroup"`
	IncomeTaxRate     string     `json:"incomeTaxRate,omitempty"`
	Salary            *SalaryDTO `json:"salary,omitempty"`
	EmployerAccount   string     `json:"employerAccount,omitempty"`
}

// OpenProfile opens the payroll profile of the current employment of a person with an employer.
type OpenProfile struct {
	Person   string `json:"person"`
	Employer string `json:"employer"`
	TermsInput
}

// SetTerms replaces the terms of a profile.
type SetTerms struct {
	ID domain.ProfileID `json:"-"`
	TermsInput
}

// AddSplit adds a payment split.
type AddSplit struct {
	ID          domain.ProfileID `json:"-"`
	IBAN        string           `json:"iban"`
	Percent     string           `json:"percent,omitempty"`
	Amount      string           `json:"amount,omitempty"`
	Residual    bool             `json:"residual,omitempty"`
	Priority    int              `json:"priority,omitempty"`
	Garnishment bool             `json:"garnishment,omitempty"`
	From        vocab.Date       `json:"from"`
	Until       vocab.Date       `json:"until,omitzero"`
}

// EndSplit ends a payment split.
type EndSplit struct {
	ID    domain.ProfileID `json:"-"`
	Split string           `json:"split"`
	On    vocab.Date       `json:"on"`
}

// GetProfile loads a profile.
type GetProfile struct{ ID domain.ProfileID }

// SearchProfiles searches profiles of the caller's scope.
type SearchProfiles struct {
	Employer, Person string
	Page, Size       int
}

// SplitDTO is the transport form of a split.
type SplitDTO struct {
	ID          string `json:"id"`
	IBAN        string `json:"iban"`
	Percent     string `json:"percent,omitempty"`
	Amount      string `json:"amount,omitempty"`
	Residual    bool   `json:"residual,omitempty"`
	Priority    int    `json:"priority"`
	Garnishment bool   `json:"garnishment,omitempty"`
	From        string `json:"from"`
	Until       string `json:"until,omitempty"`
}

// ProfileDTO is the transport form of a profile.
type ProfileDTO struct {
	ID                string     `json:"id"`
	Employment        string     `json:"employment"`
	Person            string     `json:"person"`
	Employer          string     `json:"employer"`
	ContributionGroup int        `json:"contributionGroup"`
	IncomeTaxRate     string     `json:"incomeTaxRate"`
	Salary            *SalaryDTO `json:"salary,omitempty"`
	AnnualSalary      string     `json:"annualSalary,omitempty"`
	EmployerAccount   string     `json:"employerAccount,omitempty"`
	Splits            []SplitDTO `json:"splits"`
	Version           int64      `json:"version"`
}

func profileDTO(p *domain.Profile) ProfileDTO {
	t := p.Terms()
	d := ProfileDTO{ID: p.ID().String(), Employment: p.Employment().String(), Person: p.Person().String(), Employer: p.Employer().String(),
		ContributionGroup: t.ContributionGroup, IncomeTaxRate: t.IncomeTaxRate.StringFixed(2), EmployerAccount: optID(t.Account.UUID),
		Splits: []SplitDTO{}, Version: p.Version()}
	if !t.Salary.IsZero() {
		d.Salary = &SalaryDTO{Amount: money(t.Salary.Amount), Periodicity: t.Salary.Periodicity.String(), PaymentsPerYear: t.Salary.PaymentsPerYear}
		d.AnnualSalary = money(t.Salary.Annual())
	}
	for _, s := range p.Splits() {
		d.Splits = append(d.Splits, SplitDTO{ID: s.ID.String(), IBAN: s.IBAN.String(), Percent: decimalText(s.Percent), Amount: decimalText(s.Amount),
			Residual: s.Residual, Priority: s.Priority, Garnishment: s.Garnishment, From: s.From.String(), Until: dateText(s.Until)})
	}
	return d
}

// terms parses the terms and checks the employer account belongs to the employer and is active.
func (s service) terms(ctx context.Context, in TermsInput, employer domain.OrganizationID) (domain.Terms, error) {
	var v fw.Validation
	t := domain.Terms{ContributionGroup: in.ContributionGroup, IncomeTaxRate: parseDecimal(&v, "incomeTaxRate", in.IncomeTaxRate)}
	if in.Salary != nil {
		per, ok := domain.ParsePeriodicity(in.Salary.Periodicity)
		v.Require(ok, "salary.periodicity", "enum", "monthly or annual")
		t.Salary = domain.Salary{Amount: parseDecimal(&v, "salary.amount", in.Salary.Amount), Periodicity: per, PaymentsPerYear: in.Salary.PaymentsPerYear}
	}
	if in.EmployerAccount != "" {
		t.Account = domain.EmployerAccountID{UUID: parseID(&v, "employerAccount", in.EmployerAccount)}
	}
	if err := v.Err(); err != nil {
		return domain.Terms{}, err
	}
	if !t.Account.IsZero() {
		a, err := s.Accounts.Get(ctx, t.Account)
		if err != nil {
			return domain.Terms{}, err
		}
		if a.Employer() != employer {
			return domain.Terms{}, fw.NotFound(domain.EmployerAccountKind, t.Account)
		}
		if !a.IsActive() {
			return domain.Terms{}, fw.Violation("payroll.account_inactive", "the employer account is not active")
		}
	}
	return t, nil
}

func (s service) profileUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.ProfileID, fn func(context.Context, *domain.Profile) error) (ProfileDTO, error) {
		sc := scopeOf(ctx)
		p, err := s.profiles.Update(ctx, id, func(ctx context.Context, p *domain.Profile) error {
			if err := sc.check(domain.ProfileKind, p.ID(), p.Employer(), true); err != nil {
				return err
			}
			return fn(ctx, p)
		})
		if err != nil {
			return ProfileDTO{}, err
		}
		return profileDTO(p), nil
	}

	svc.OpenProfile = guard(PermProfileUpdate, func(ctx context.Context, c OpenProfile) (ProfileDTO, error) {
		var v fw.Validation
		person := domain.PersonID{UUID: parseID(&v, "person", c.Person)}
		employer := domain.OrganizationID{UUID: parseID(&v, "employer", c.Employer)}
		if err := v.Err(); err != nil {
			return ProfileDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", employer, employer, true); err != nil {
			return ProfileDTO{}, err
		}
		emp, ok, err := s.Employments.EmploymentOn(ctx, person, employer, vocab.DateOf(fw.Now()))
		if err != nil {
			return ProfileDTO{}, err
		}
		if !ok {
			return ProfileDTO{}, fw.Violation("payroll.not_employed", "the person is not an employee of the employer")
		}
		existing, err := s.Profiles.Find(ctx, domain.ProfFieldEmployment.Eq(emp.ID))
		if err != nil {
			return ProfileDTO{}, err
		}
		if len(existing) > 0 {
			return ProfileDTO{}, fw.Violation("payroll.profile_exists", "the employment already has a payroll profile")
		}
		t, err := s.terms(ctx, c.TermsInput, employer)
		if err != nil {
			return ProfileDTO{}, err
		}
		p, err := domain.OpenProfile(domain.NewProfileID(), domain.ProfileState{Employment: emp.ID, Person: person, Employer: employer,
			ContributionGroup: t.ContributionGroup, IncomeTaxRate: t.IncomeTaxRate, Salary: t.Salary, Account: t.Account})
		if err != nil {
			return ProfileDTO{}, err
		}
		if err := s.profiles.Create(ctx, p); err != nil {
			return ProfileDTO{}, err
		}
		return profileDTO(p), nil
	}, pipeline.Transactional[OpenProfile, ProfileDTO](s.UoW))

	svc.SetTerms = guard(PermProfileUpdate, func(ctx context.Context, c SetTerms) (ProfileDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, p *domain.Profile) error {
			t, err := s.terms(ctx, c.TermsInput, p.Employer())
			if err != nil {
				return err
			}
			return p.SetTerms(t)
		})
	}, retry[SetTerms, ProfileDTO]())

	svc.AddSplit = guard(PermProfileUpdate, func(ctx context.Context, c AddSplit) (ProfileDTO, error) {
		var v fw.Validation
		split := domain.Split{IBAN: parseIBAN(&v, "iban", c.IBAN), Percent: parseDecimal(&v, "percent", c.Percent),
			Amount: parseDecimal(&v, "amount", c.Amount), Residual: c.Residual, Priority: c.Priority, Garnishment: c.Garnishment,
			From: c.From, Until: c.Until}
		if err := v.Err(); err != nil {
			return ProfileDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, p *domain.Profile) error { _, err := p.AddSplit(split); return err })
	}, retry[AddSplit, ProfileDTO]())

	svc.EndSplit = guard(PermProfileUpdate, func(ctx context.Context, c EndSplit) (ProfileDTO, error) {
		var v fw.Validation
		id := domain.SplitID{UUID: parseID(&v, "split", c.Split)}
		if err := v.Err(); err != nil {
			return ProfileDTO{}, err
		}
		return update(ctx, c.ID, func(_ context.Context, p *domain.Profile) error { return p.EndSplit(id, c.On) })
	}, retry[EndSplit, ProfileDTO]())

	svc.GetProfile = guard(PermProfileRead, func(ctx context.Context, q GetProfile) (ProfileDTO, error) {
		p, err := s.Profiles.Get(ctx, q.ID)
		if err != nil {
			return ProfileDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.ProfileKind, p.ID(), p.Employer(), false); err != nil {
			return ProfileDTO{}, err
		}
		return profileDTO(p), nil
	})

	svc.SearchProfiles = guard(PermProfileRead, func(ctx context.Context, q SearchProfiles) (fw.Page[ProfileDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Profile]{within(scopeOf(ctx), domain.ProfFieldEmployer)}
		if q.Employer != "" {
			parts = append(parts, domain.ProfFieldEmployer.Eq(domain.OrganizationID{UUID: parseID(&v, "employer", q.Employer)}))
		}
		if q.Person != "" {
			parts = append(parts, domain.ProfFieldPerson.Eq(domain.PersonID{UUID: parseID(&v, "person", q.Person)}))
		}
		if err := v.Err(); err != nil {
			return fw.Page[ProfileDTO]{}, err
		}
		page, err := s.Profiles.FindPage(ctx, spec.And(parts...), fw.NewPageRequest[*domain.Profile](q.Page, q.Size))
		if err != nil {
			return fw.Page[ProfileDTO]{}, err
		}
		return fw.MapPage(page, profileDTO), nil
	})
}

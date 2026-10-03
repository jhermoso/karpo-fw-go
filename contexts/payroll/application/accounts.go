package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// RegisterAccount registers a CCC of an employer.
type RegisterAccount struct {
	Employer string `json:"employer"`
	Code     string `json:"code"`
	Regime   string `json:"regime"`
	Activity string `json:"activity,omitempty"`
	Method   string `json:"method"` // direct-debit | late-payable | early-payable | manual
	IBAN     string `json:"iban,omitempty"`
}

// SetAccountPayment changes how contributions are paid.
type SetAccountPayment struct {
	ID     domain.EmployerAccountID `json:"-"`
	Method string                   `json:"method"`
	IBAN   string                   `json:"iban,omitempty"`
}

// DeactivateAccount records the Social Security "baja" of a CCC.
type DeactivateAccount struct {
	ID domain.EmployerAccountID `json:"-"`
}

// GetAccount loads an employer account.
type GetAccount struct{ ID domain.EmployerAccountID }

// SearchAccounts searches employer accounts of the caller's scope.
type SearchAccounts struct {
	Employer   string
	ActiveOnly bool
	Page, Size int
}

// AccountDTO is the transport form of an employer account.
type AccountDTO struct {
	ID       string `json:"id"`
	Employer string `json:"employer"`
	Code     string `json:"code"`
	Regime   string `json:"regime"`
	Activity string `json:"activity,omitempty"`
	Method   string `json:"method"`
	IBAN     string `json:"iban,omitempty"`
	Active   bool   `json:"active"`
	Version  int64  `json:"version"`
}

func accountDTO(a *domain.EmployerAccount) AccountDTO {
	return AccountDTO{ID: a.ID().String(), Employer: a.Employer().String(), Code: a.Code(), Regime: a.Regime(), Activity: a.Activity(),
		Method: a.Method().String(), IBAN: a.IBAN().String(), Active: a.IsActive(), Version: a.Version()}
}

func (s service) accountUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.EmployerAccountID, fn func(*domain.EmployerAccount) error) (AccountDTO, error) {
		sc := scopeOf(ctx)
		a, err := s.accounts.Update(ctx, id, func(_ context.Context, a *domain.EmployerAccount) error {
			if err := sc.check(domain.EmployerAccountKind, a.ID(), a.Employer(), true); err != nil {
				return err
			}
			return fn(a)
		})
		if err != nil {
			return AccountDTO{}, err
		}
		return accountDTO(a), nil
	}

	svc.RegisterAccount = guard(PermAccountCreate, func(ctx context.Context, c RegisterAccount) (AccountDTO, error) {
		var v fw.Validation
		employer := domain.OrganizationID{UUID: parseID(&v, "employer", c.Employer)}
		method, ok := domain.ParsePaymentMethod(c.Method)
		v.Require(ok, "method", "enum", "direct-debit, late-payable, early-payable or manual")
		iban := parseIBAN(&v, "iban", c.IBAN)
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", employer, employer, true); err != nil {
			return AccountDTO{}, err
		}
		a, err := domain.RegisterEmployerAccount(domain.NewEmployerAccountID(), domain.EmployerAccountState{Employer: employer, Code: c.Code,
			Regime: c.Regime, Activity: c.Activity, Method: method, IBAN: iban})
		if err != nil {
			return AccountDTO{}, err
		}
		// A CCC identifies one employer registration: it is unique while active, whoever owns it.
		same, err := s.Accounts.Find(ctx, domain.AccFieldCode.Eq(a.Code()).And(domain.AccFieldActive.Eq(true)))
		if err != nil {
			return AccountDTO{}, err
		}
		if len(same) > 0 {
			return AccountDTO{}, fw.Violation("payroll.duplicate_ccc", "the CCC is already registered")
		}
		if err := s.accounts.Create(ctx, a); err != nil {
			return AccountDTO{}, err
		}
		return accountDTO(a), nil
	}, pipeline.Transactional[RegisterAccount, AccountDTO](s.UoW))

	svc.SetAccountPayment = guard(PermAccountUpdate, func(ctx context.Context, c SetAccountPayment) (AccountDTO, error) {
		var v fw.Validation
		method, ok := domain.ParsePaymentMethod(c.Method)
		v.Require(ok, "method", "enum", "direct-debit, late-payable, early-payable or manual")
		iban := parseIBAN(&v, "iban", c.IBAN)
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		return update(ctx, c.ID, func(a *domain.EmployerAccount) error { return a.SetPayment(method, iban) })
	}, retry[SetAccountPayment, AccountDTO]())

	svc.DeactivateAccount = guard(PermAccountUpdate, func(ctx context.Context, c DeactivateAccount) (AccountDTO, error) {
		return update(ctx, c.ID, func(a *domain.EmployerAccount) error { a.Deactivate(); return nil })
	}, retry[DeactivateAccount, AccountDTO]())

	svc.GetAccount = guard(PermAccountRead, func(ctx context.Context, q GetAccount) (AccountDTO, error) {
		a, err := s.Accounts.Get(ctx, q.ID)
		if err != nil {
			return AccountDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.EmployerAccountKind, a.ID(), a.Employer(), false); err != nil {
			return AccountDTO{}, err
		}
		return accountDTO(a), nil
	})

	svc.SearchAccounts = guard(PermAccountRead, func(ctx context.Context, q SearchAccounts) (fw.Page[AccountDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.EmployerAccount]{within(scopeOf(ctx), domain.AccFieldEmployer)}
		if q.Employer != "" {
			parts = append(parts, domain.AccFieldEmployer.Eq(domain.OrganizationID{UUID: parseID(&v, "employer", q.Employer)}))
		}
		if q.ActiveOnly {
			parts = append(parts, domain.AccFieldActive.Eq(true))
		}
		if err := v.Err(); err != nil {
			return fw.Page[AccountDTO]{}, err
		}
		page, err := s.Accounts.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.AccFieldCode.Asc()))
		if err != nil {
			return fw.Page[AccountDTO]{}, err
		}
		return fw.MapPage(page, accountDTO), nil
	})
}

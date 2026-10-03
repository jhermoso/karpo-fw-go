package application

import (
	"context"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// CreateAccount adds an account to the chart of a company.
type CreateAccount struct {
	Company  string `json:"company"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Postable bool   `json:"postable"`
}

// RenameAccount renames an account.
type RenameAccount struct {
	ID   domain.AccountID `json:"-"`
	Name string           `json:"name"`
}

// DeactivateAccount retires an account.
type DeactivateAccount struct {
	ID domain.AccountID `json:"-"`
}

// SearchAccounts lists the chart of a company, optionally under a code prefix.
type SearchAccounts struct{ Company, Prefix string }

// AccountDTO is the transport form of an account.
type AccountDTO struct {
	ID       string `json:"id"`
	Company  string `json:"company"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Nature   string `json:"nature"`
	Postable bool   `json:"postable"`
	Active   bool   `json:"active"`
}

func accountDTO(a *domain.Account) AccountDTO {
	s := a.State()
	return AccountDTO{ID: a.ID().String(), Company: s.Company.String(), Code: s.Code, Name: s.Name, Nature: a.Nature().String(), Postable: s.Postable, Active: s.Active}
}

// OpenLedger opens the ledger of a company with its posting profile.
type OpenLedger struct {
	Company    string            `json:"company"`
	StartMonth int               `json:"startMonth"`
	Accounts   map[string]string `json:"accounts,omitempty"` // role → account code
	TaxCodes   map[string]string `json:"taxCodes,omitempty"` // tax code → output tax account
}

// SetProfile replaces the posting profile of a ledger.
type SetProfile struct {
	ID       domain.LedgerID   `json:"-"`
	Accounts map[string]string `json:"accounts"`
	TaxCodes map[string]string `json:"taxCodes,omitempty"`
}

// ChangePeriod closes or reopens a period.
type ChangePeriod struct {
	ID    domain.LedgerID `json:"-"`
	Year  int             `json:"year"`
	Month int             `json:"month"`
}

// GetLedger loads the ledger of a company.
type GetLedger struct{ Company string }

// LedgerDTO is the transport form of a ledger.
type LedgerDTO struct {
	ID         string            `json:"id"`
	Company    string            `json:"company"`
	StartMonth int               `json:"startMonth"`
	Accounts   map[string]string `json:"accounts"`
	TaxCodes   map[string]string `json:"taxCodes"`
	Closed     []string          `json:"closed"` // YYYY/MM (fiscal year / period)
	Version    int64             `json:"version"`
}

func ledgerDTO(l *domain.Ledger) LedgerDTO {
	s := l.State()
	d := LedgerDTO{ID: l.ID().String(), Company: s.Company.String(), StartMonth: s.StartMonth, Accounts: map[string]string{}, TaxCodes: s.TaxCodes,
		Closed: []string{}, Version: l.Version()}
	for r, c := range s.Accounts {
		d.Accounts[string(r)] = c
	}
	for _, p := range s.Closed {
		d.Closed = append(d.Closed, fmt.Sprintf("%d/%02d", p.Year, p.Month))
	}
	return d
}

func roles(in map[string]string) map[domain.Role]string {
	out := map[domain.Role]string{}
	for k, v := range in {
		out[domain.Role(k)] = v
	}
	return out
}

func taxCodes(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[strings.ToUpper(strings.TrimSpace(k))] = v
	}
	return out
}

func (s service) chartUseCases(svc *Service) {
	svc.CreateAccount = guard(PermAccountWrite, func(ctx context.Context, c CreateAccount) (AccountDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		if err := v.Err(); err != nil {
			return AccountDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return AccountDTO{}, err
		}
		a, err := domain.ReconstituteAccount(domain.NewAccountID(), domain.AccountState{Company: company, Code: c.Code, Name: c.Name, Postable: c.Postable, Active: true})
		if err != nil {
			return AccountDTO{}, err
		}
		dup, err := s.Accounts.Exists(ctx, domain.AccFieldCompany.Eq(company).And(domain.AccFieldCode.Eq(a.State().Code)))
		if err != nil {
			return AccountDTO{}, err
		}
		if dup {
			return AccountDTO{}, fw.Violation("accounting.duplicate_account", "the chart already has that code")
		}
		if err := s.accounts.Create(ctx, a); err != nil {
			return AccountDTO{}, err
		}
		return accountDTO(a), nil
	}, pipeline.Transactional[CreateAccount, AccountDTO](s.UoW))

	updateAccount := func(ctx context.Context, id domain.AccountID, fn func(*domain.Account) error) (AccountDTO, error) {
		sc := scopeOf(ctx)
		a, err := s.accounts.Update(ctx, id, func(_ context.Context, a *domain.Account) error {
			if err := sc.check(domain.AccountKind, a.ID(), a.State().Company, true); err != nil {
				return err
			}
			return fn(a)
		})
		if err != nil {
			return AccountDTO{}, err
		}
		return accountDTO(a), nil
	}
	svc.RenameAccount = guard(PermAccountWrite, func(ctx context.Context, c RenameAccount) (AccountDTO, error) {
		return updateAccount(ctx, c.ID, func(a *domain.Account) error { return a.Rename(c.Name) })
	}, retry[RenameAccount, AccountDTO]())
	svc.DeactivateAccount = guard(PermAccountWrite, func(ctx context.Context, c DeactivateAccount) (AccountDTO, error) {
		return updateAccount(ctx, c.ID, func(a *domain.Account) error { a.Deactivate(); return nil })
	}, retry[DeactivateAccount, AccountDTO]())

	svc.SearchAccounts = guard(PermAccountRead, func(ctx context.Context, q SearchAccounts) ([]AccountDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return nil, err
		}
		as, err := s.Accounts.Find(ctx, domain.AccFieldCompany.Eq(company), domain.AccFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []AccountDTO{}
		for _, a := range as {
			if strings.HasPrefix(a.State().Code, q.Prefix) {
				out = append(out, accountDTO(a))
			}
		}
		return out, nil
	})

	svc.OpenLedger = guard(PermLedgerWrite, func(ctx context.Context, c OpenLedger) (LedgerDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		if err := v.Err(); err != nil {
			return LedgerDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return LedgerDTO{}, err
		}
		if _, err := s.ledgerOf(ctx, company); err == nil {
			return LedgerDTO{}, fw.Violation("accounting.ledger_exists", "the company already has a ledger")
		}
		l, err := domain.ReconstituteLedger(domain.NewLedgerID(), domain.LedgerState{Company: company, StartMonth: c.StartMonth,
			Accounts: roles(c.Accounts), TaxCodes: taxCodes(c.TaxCodes)})
		if err != nil {
			return LedgerDTO{}, err
		}
		if err := s.ledgers.Create(ctx, l); err != nil {
			return LedgerDTO{}, err
		}
		return ledgerDTO(l), nil
	}, pipeline.Transactional[OpenLedger, LedgerDTO](s.UoW))

	updateLedger := func(ctx context.Context, id domain.LedgerID, fn func(*domain.Ledger) error) (LedgerDTO, error) {
		sc := scopeOf(ctx)
		l, err := s.ledgers.Update(ctx, id, func(_ context.Context, l *domain.Ledger) error {
			if err := sc.check(domain.LedgerKind, l.ID(), l.State().Company, true); err != nil {
				return err
			}
			return fn(l)
		})
		if err != nil {
			return LedgerDTO{}, err
		}
		return ledgerDTO(l), nil
	}
	svc.SetProfile = guard(PermLedgerWrite, func(ctx context.Context, c SetProfile) (LedgerDTO, error) {
		return updateLedger(ctx, c.ID, func(l *domain.Ledger) error { return l.SetProfile(roles(c.Accounts), taxCodes(c.TaxCodes)) })
	}, retry[SetProfile, LedgerDTO]())
	svc.ClosePeriod = guard(PermLedgerWrite, func(ctx context.Context, c ChangePeriod) (LedgerDTO, error) {
		return updateLedger(ctx, c.ID, func(l *domain.Ledger) error { return l.Close(domain.Period{Year: c.Year, Month: c.Month}) })
	}, retry[ChangePeriod, LedgerDTO]())
	svc.ReopenPeriod = guard(PermLedgerWrite, func(ctx context.Context, c ChangePeriod) (LedgerDTO, error) {
		return updateLedger(ctx, c.ID, func(l *domain.Ledger) error { l.Reopen(domain.Period{Year: c.Year, Month: c.Month}); return nil })
	}, retry[ChangePeriod, LedgerDTO]())
	svc.GetLedger = guard(PermLedgerRead, func(ctx context.Context, q GetLedger) (LedgerDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return LedgerDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return LedgerDTO{}, err
		}
		l, err := s.ledgerOf(ctx, company)
		if err != nil {
			return LedgerDTO{}, fw.NotFound(domain.LedgerKind, company)
		}
		return ledgerDTO(l), nil
	})
}

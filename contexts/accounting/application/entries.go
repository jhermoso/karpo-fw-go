package application

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// LineInput is a line of a manual entry.
type LineInput struct {
	Account     string `json:"account"`
	Party       string `json:"party,omitempty"`
	Debit       string `json:"debit,omitempty"`
	Credit      string `json:"credit,omitempty"`
	Description string `json:"description,omitempty"`
}

// PostEntry posts a manual entry.
type PostEntry struct {
	Company     string      `json:"company"`
	Date        vocab.Date  `json:"date"`
	Description string      `json:"description"`
	Lines       []LineInput `json:"lines"`
}

// ReverseEntry reverses an entry on a date (today by default).
type ReverseEntry struct {
	ID   domain.EntryID `json:"-"`
	Date vocab.Date     `json:"date,omitzero"`
}

// GetEntry loads an entry.
type GetEntry struct{ ID domain.EntryID }

// SearchEntries searches the journal of a company.
type SearchEntries struct {
	Company, From, To string
	Page, Size        int
}

// TrialBalance returns the debits, credits and balance of each account with movements in a date
// range (sumas y saldos).
type TrialBalance struct{ Company, From, To string }

// AccountLedger returns the movements of an account in a date range with the running balance
// (libro mayor).
type AccountLedger struct{ Company, Account, From, To string }

// LineDTO is the transport form of a line.
type LineDTO struct {
	Account     string `json:"account"`
	Party       string `json:"party,omitempty"`
	Debit       string `json:"debit,omitempty"`
	Credit      string `json:"credit,omitempty"`
	Description string `json:"description,omitempty"`
}

// EntryDTO is the transport form of an entry.
type EntryDTO struct {
	ID          string    `json:"id"`
	Company     string    `json:"company"`
	Number      int64     `json:"number"`
	Year        int       `json:"year"`
	Period      int       `json:"period"`
	Date        string    `json:"date"`
	Description string    `json:"description"`
	Source      string    `json:"source,omitempty"`
	SourceID    string    `json:"sourceId,omitempty"`
	Total       string    `json:"total"`
	Lines       []LineDTO `json:"lines"`
	Reverses    string    `json:"reverses,omitempty"`
	ReversedBy  string    `json:"reversedBy,omitempty"`
}

// BalanceRow is a row of the trial balance.
type BalanceRow struct {
	Account string `json:"account"`
	Name    string `json:"name"`
	Debit   string `json:"debit"`
	Credit  string `json:"credit"`
	Balance string `json:"balance"` // debit − credit
}

// Movement is a line of the ledger of an account.
type Movement struct {
	Date        string `json:"date"`
	Entry       int64  `json:"entry"`
	Description string `json:"description"`
	Party       string `json:"party,omitempty"`
	Debit       string `json:"debit,omitempty"`
	Credit      string `json:"credit,omitempty"`
	Balance     string `json:"balance"`
}

func optMoney(d vocab.Decimal) string {
	if d.IsZero() {
		return ""
	}
	return money(d)
}

func optID(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

func entryDTO(e *domain.Entry) EntryDTO {
	s := e.State()
	d := EntryDTO{ID: e.ID().String(), Company: s.Company.String(), Number: s.Number, Year: s.Year, Period: s.Month, Date: s.Date.String(),
		Description: s.Description, Source: s.Source.Type, SourceID: s.Source.ID, Total: money(e.Total()), Lines: []LineDTO{},
		Reverses: optID(s.Reverses.UUID), ReversedBy: optID(s.ReversedBy.UUID)}
	for _, l := range s.Lines {
		d.Lines = append(d.Lines, LineDTO{Account: l.Account, Party: optID(l.Party.UUID), Debit: optMoney(l.Debit), Credit: optMoney(l.Credit),
			Description: l.Description})
	}
	return d
}

// rangeOf parses an optional date range.
func rangeOf(v *fw.Validation, from, to string) (vocab.Date, vocab.Date) {
	var f, t vocab.Date
	if from != "" {
		d, err := vocab.ParseDate(from)
		v.Require(err == nil, "from", "format", "a date YYYY-MM-DD is required")
		f = d
	}
	if to != "" {
		d, err := vocab.ParseDate(to)
		v.Require(err == nil, "to", "format", "a date YYYY-MM-DD is required")
		t = d
	}
	return f, t
}

func (s service) journal(ctx context.Context, company domain.OrganizationID, from, to vocab.Date) ([]*domain.Entry, error) {
	parts := []spec.Specification[*domain.Entry]{domain.EntFieldCompany.Eq(company)}
	if !from.IsZero() {
		parts = append(parts, domain.EntFieldDate.Ge(from))
	}
	if !to.IsZero() {
		parts = append(parts, domain.EntFieldDate.Le(to))
	}
	return s.Entries.Find(ctx, spec.And(parts...), domain.EntFieldDate.Asc(), domain.EntFieldNumber.Asc())
}

func (s service) entryUseCases(svc *Service) {
	svc.PostEntry = guard(PermEntryCreate, func(ctx context.Context, c PostEntry) (EntryDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		d := domain.Draft{Company: company, Date: c.Date, Description: c.Description, Source: domain.Source{Type: "manual"}}
		for _, l := range c.Lines {
			line := domain.Line{Account: strings.TrimSpace(l.Account), Debit: parseDecimal(&v, "debit", l.Debit), Credit: parseDecimal(&v, "credit", l.Credit),
				Description: l.Description}
			if l.Party != "" {
				line.Party = domain.PartyID{UUID: parseID(&v, "party", l.Party)}
			}
			v.Require(!line.Debit.IsNegative() && !line.Credit.IsNegative(), "lines", "amount", "manual lines have non-negative amounts")
			d.Lines = append(d.Lines, line)
		}
		v.Require(!c.Date.IsZero(), "date", "required", "the date is required")
		if err := v.Err(); err != nil {
			return EntryDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return EntryDTO{}, err
		}
		e, err := s.post(ctx, d)
		if err != nil {
			return EntryDTO{}, err
		}
		return entryDTO(e), nil
	}, retry[PostEntry, EntryDTO](), pipeline.Transactional[PostEntry, EntryDTO](s.UoW))

	svc.ReverseEntry = guard(PermEntryReverse, func(ctx context.Context, c ReverseEntry) (EntryDTO, error) {
		e, err := s.Entries.Get(ctx, c.ID)
		if err != nil {
			return EntryDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.EntryKind, e.ID(), e.State().Company, true); err != nil {
			return EntryDTO{}, err
		}
		on := c.Date
		if on.IsZero() {
			on = vocab.DateOf(fw.Now())
		}
		r, err := s.reverse(ctx, c.ID, on, domain.Source{Type: "manual-reversal"})
		if err != nil {
			return EntryDTO{}, err
		}
		return entryDTO(r), nil
	}, retry[ReverseEntry, EntryDTO](), pipeline.Transactional[ReverseEntry, EntryDTO](s.UoW))

	svc.GetEntry = guard(PermEntryRead, func(ctx context.Context, q GetEntry) (EntryDTO, error) {
		e, err := s.Entries.Get(ctx, q.ID)
		if err != nil {
			return EntryDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.EntryKind, e.ID(), e.State().Company, false); err != nil {
			return EntryDTO{}, err
		}
		return entryDTO(e), nil
	})

	svc.SearchEntries = guard(PermEntryRead, func(ctx context.Context, q SearchEntries) (fw.Page[EntryDTO], error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		from, to := rangeOf(&v, q.From, q.To)
		if err := v.Err(); err != nil {
			return fw.Page[EntryDTO]{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return fw.Page[EntryDTO]{}, err
		}
		parts := []spec.Specification[*domain.Entry]{domain.EntFieldCompany.Eq(company)}
		if !from.IsZero() {
			parts = append(parts, domain.EntFieldDate.Ge(from))
		}
		if !to.IsZero() {
			parts = append(parts, domain.EntFieldDate.Le(to))
		}
		page, err := s.Entries.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.EntFieldDate.Asc(), domain.EntFieldNumber.Asc()))
		if err != nil {
			return fw.Page[EntryDTO]{}, err
		}
		return fw.MapPage(page, entryDTO), nil
	})

	svc.TrialBalance = guard(PermEntryRead, func(ctx context.Context, q TrialBalance) ([]BalanceRow, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		from, to := rangeOf(&v, q.From, q.To)
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return nil, err
		}
		es, err := s.journal(ctx, company, from, to)
		if err != nil {
			return nil, err
		}
		type sums struct{ d, c vocab.Decimal }
		totals := map[string]*sums{}
		for _, e := range es {
			for _, l := range e.State().Lines {
				t, ok := totals[l.Account]
				if !ok {
					t = &sums{vocab.DecimalFromInt(0), vocab.DecimalFromInt(0)}
					totals[l.Account] = t
				}
				t.d, t.c = t.d.Add(l.Debit), t.c.Add(l.Credit)
			}
		}
		as, err := s.Accounts.Find(ctx, domain.AccFieldCompany.Eq(company))
		if err != nil {
			return nil, err
		}
		names := map[string]string{}
		for _, a := range as {
			names[a.State().Code] = a.State().Name
		}
		out := []BalanceRow{}
		for code, t := range totals {
			out = append(out, BalanceRow{Account: code, Name: names[code], Debit: money(t.d), Credit: money(t.c), Balance: money(t.d.Sub(t.c))})
		}
		slices.SortFunc(out, func(a, b BalanceRow) int { return strings.Compare(a.Account, b.Account) })
		return out, nil
	})

	svc.AccountLedger = guard(PermEntryRead, func(ctx context.Context, q AccountLedger) ([]Movement, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		from, to := rangeOf(&v, q.From, q.To)
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return nil, err
		}
		es, err := s.journal(ctx, company, from, to)
		if err != nil {
			return nil, err
		}
		balance := vocab.DecimalFromInt(0)
		out := []Movement{}
		for _, e := range es {
			st := e.State()
			for _, l := range st.Lines {
				if l.Account != q.Account {
					continue
				}
				balance = balance.Add(l.Debit).Sub(l.Credit)
				desc := l.Description
				if desc == "" {
					desc = st.Description
				}
				out = append(out, Movement{Date: st.Date.String(), Entry: st.Number, Description: desc, Party: optID(l.Party.UUID),
					Debit: optMoney(l.Debit), Credit: optMoney(l.Credit), Balance: money(balance)})
			}
		}
		return out, nil
	})
}

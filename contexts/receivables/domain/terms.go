// Package domain is the model of the Receivables (Cobros) bounded context: payment terms and the
// schedule of due dates they produce, the receivable of each issued invoice with its
// installments, the collections from customers and their allocation to installments, and the
// credit profile of each customer (default terms, risk limit, block). Invoices belong to Billing,
// customers to Parties, bank accounts and remittances to Treasury: this context references them
// by identity.
package domain

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

type (
	// TermsID identifies payment terms.
	TermsID struct{ fw.UUID }
	// ReceivableID identifies a receivable: the id of the invoice it comes from.
	ReceivableID struct{ fw.UUID }
	// CollectionID identifies a collection.
	CollectionID struct{ fw.UUID }
	// CreditProfileID identifies a credit profile.
	CreditProfileID struct{ fw.UUID }
	// OrganizationID is the seller, an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
	// PartyID is the customer, a party of the Parties context.
	PartyID struct{ fw.UUID }
)

// TermsKind is the stable aggregate type name.
const TermsKind = "receivables.payment_terms"

// MonthRange is a period of months (both included) in which nothing falls due (for example
// August); it may wrap around the year (December to January).
type MonthRange struct{ From, To time.Month }

// IsZero reports whether no period was set.
func (r MonthRange) IsZero() bool { return r.From == 0 && r.To == 0 }

// Contains reports whether a month is in the range.
func (r MonthRange) Contains(m time.Month) bool {
	if r.IsZero() {
		return false
	}
	if r.From <= r.To {
		return m >= r.From && m <= r.To
	}
	return m >= r.From || m <= r.To
}

// TermsState is the persisted state of payment terms (the C# PaymentCondition of Parties, which
// nothing used and nothing turned into due dates).
type TermsState struct {
	Seller          OrganizationID
	Code            string
	Description     string
	Installments    int
	DaysToFirst     int
	DaysBetween     int
	FixedDays       []int // days of the month on which payments fall due (up to 3)
	NoPayment       MonthRange
	CommercialMonth bool // periods of 30 days count as calendar months
	ControlHolidays bool // a due date on a holiday moves back (at most BackwardDays) or forward
	BackwardDays    int
	Active          bool
	Audit           traits.AuditStamp
}

func (s *TermsState) check(v *fw.Validation) {
	s.Code = strings.ToUpper(strings.TrimSpace(s.Code))
	v.Require(s.Code != "" && utf8.RuneCountInString(s.Code) <= 10, "code", "length", "a code of 1 to 10 characters")
	s.Description = strings.TrimSpace(s.Description)
	v.Require(s.Description != "" && utf8.RuneCountInString(s.Description) <= 100, "description", "length", "a description of 1 to 100 characters")
	v.Require(!s.Seller.IsZero(), "seller", "required", "terms belong to a seller")
	v.Require(s.Installments >= 1 && s.Installments <= 24, "installments", "range", "from 1 to 24 installments")
	v.Require(s.DaysToFirst >= 0 && s.DaysToFirst <= 720 && s.DaysBetween >= 0 && s.DaysBetween <= 365, "daysToFirst", "range", "days within limits")
	v.Require(s.Installments == 1 || s.DaysBetween > 0, "daysBetween", "required", "several installments need days between them")
	fixed := slices.Clone(s.FixedDays)
	slices.Sort(fixed)
	fixed = slices.Compact(fixed)
	v.Require(len(fixed) == len(s.FixedDays) && len(fixed) <= 3, "fixedDays", "count", "up to 3 different fixed days")
	for _, d := range fixed {
		v.Require(d >= 1 && d <= 31, "fixedDays", "range", "days from 1 to 31")
	}
	s.FixedDays = fixed
	okMonths := func(m time.Month) bool { return m >= 1 && m <= 12 }
	v.Require(s.NoPayment.IsZero() || (okMonths(s.NoPayment.From) && okMonths(s.NoPayment.To)), "noPayment", "range",
		"both months of the period, from 1 to 12, or none")
	v.Require(s.BackwardDays >= 0 && s.BackwardDays <= 31, "backwardDays", "range", "0 to 31 days")
}

// Terms are payment terms of a seller.
type Terms struct {
	fw.BaseAggregateRoot[TermsID]
	traits.Audited
	s TermsState
}

// ReconstituteTerms rebuilds payment terms.
func ReconstituteTerms(id TermsID, s TermsState) (*Terms, error) {
	base, err := fw.NewBaseAggregateRoot(TermsKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.check(&v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Terms{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (t *Terms) State() TermsState {
	s := t.s
	s.FixedDays = slices.Clone(s.FixedDays)
	return s
}

// Retire deactivates the terms.
func (t *Terms) Retire() { t.s.Active = false }

// AuditSnapshot implements traits.Snapshotter.
func (t *Terms) AuditSnapshot() map[string]any {
	return map[string]any{"code": t.s.Code, "installments": t.s.Installments, "active": t.s.Active}
}

// Calendar tells holidays (a port; no calendar exists in the C#, the default has none).
type Calendar interface {
	IsHoliday(ctx context.Context, d vocab.Date) (bool, error)
}

// NoHolidays is a calendar without holidays.
type NoHolidays struct{}

// IsHoliday implements Calendar.
func (NoHolidays) IsHoliday(context.Context, vocab.Date) (bool, error) { return false, nil }

// Due is an installment of a schedule.
type Due struct {
	Date   vocab.Date
	Amount vocab.Decimal
}

// Schedule computes the due dates and amounts of an amount issued on a date:
//  1. installment k falls DaysToFirst + k × DaysBetween days after the issue date (in calendar
//     months when CommercialMonth and the days are a multiple of 30);
//  2. with fixed days, it moves to the next fixed day (clamped to the end of the month);
//  3. in a month without payments, it moves to the first fixed day (or the 1st) after the period;
//  4. with ControlHolidays, a holiday moves back up to BackwardDays, else forward to a working day;
//  5. the amount is split in equal parts rounded to the cent; the last part takes the remainder.
//
// A negative amount (a credit) has a single installment due on the issue date.
func (t *Terms) Schedule(ctx context.Context, issued vocab.Date, amount vocab.Decimal, cal Calendar) ([]Due, error) {
	if amount.IsNegative() {
		return []Due{{Date: issued, Amount: amount}}, nil
	}
	if cal == nil {
		cal = NoHolidays{}
	}
	n := t.s.Installments
	part := amount.Div(vocab.DecimalFromInt(int64(n))).RoundDown(2)
	out := make([]Due, 0, n)
	rest := amount
	for k := 0; k < n; k++ {
		days := t.s.DaysToFirst + k*t.s.DaysBetween
		d := issued.AddDays(days)
		if t.s.CommercialMonth && days%30 == 0 {
			d = issued.AddMonths(days / 30)
		}
		d = t.toFixedDay(d)
		for guard := 0; t.s.NoPayment.Contains(d.Month()) && guard < 12; guard++ {
			d = t.toFixedDay(vocab.MustDate(d.Year(), d.Month(), 1).AddMonths(1))
		}
		if t.s.ControlHolidays {
			var err error
			if d, err = t.avoidHolidays(ctx, d, cal); err != nil {
				return nil, err
			}
		}
		a := part
		if k == n-1 {
			a = rest
		}
		rest = rest.Sub(a)
		out = append(out, Due{Date: d, Amount: a})
	}
	return out, nil
}

// toFixedDay moves a date to the next fixed day, clamping to the end of the month.
func (t *Terms) toFixedDay(d vocab.Date) vocab.Date {
	if len(t.s.FixedDays) == 0 {
		return d
	}
	for month := 0; month < 2; month++ {
		first := vocab.MustDate(d.Year(), d.Month(), 1).AddMonths(month)
		last := first.AddMonths(1).AddDays(-1).Day()
		for _, fd := range t.s.FixedDays {
			c := vocab.MustDate(first.Year(), first.Month(), min(fd, last))
			if !c.Before(d) {
				return c
			}
		}
	}
	return d
}

func (t *Terms) avoidHolidays(ctx context.Context, d vocab.Date, cal Calendar) (vocab.Date, error) {
	holiday, err := cal.IsHoliday(ctx, d)
	if err != nil || !holiday {
		return d, err
	}
	for back := 1; back <= t.s.BackwardDays; back++ {
		c := d.AddDays(-back)
		h, err := cal.IsHoliday(ctx, c)
		if err != nil {
			return d, err
		}
		if !h {
			return c, nil
		}
	}
	for fwd := 1; fwd <= 366; fwd++ {
		c := d.AddDays(fwd)
		h, err := cal.IsHoliday(ctx, c)
		if err != nil {
			return d, err
		}
		if !h {
			return c, nil
		}
	}
	return d, nil
}

// Terms fields.
var (
	TermsFieldSeller = spec.Comparable("seller", func(t *Terms) OrganizationID { return t.s.Seller })
	TermsFieldCode   = spec.Ordered("code", func(t *Terms) string { return t.s.Code })
	TermsFieldActive = spec.Comparable("active", func(t *Terms) bool { return t.s.Active })
)

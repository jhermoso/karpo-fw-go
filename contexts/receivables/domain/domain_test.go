package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func date(s string) vocab.Date {
	d, err := vocab.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

var seller = domain.OrganizationID{UUID: fw.NewUUID()}

func terms(t *testing.T, s domain.TermsState) *domain.Terms {
	t.Helper()
	s.Seller, s.Code, s.Description, s.Active = seller, "T", "Terms", true
	if s.Installments == 0 {
		s.Installments = 1
	}
	tr, err := domain.ReconstituteTerms(domain.NewTermsID(), s)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

type holidays map[string]bool

func (h holidays) IsHoliday(_ context.Context, _ domain.OrganizationID, d vocab.Date) (bool, error) {
	return h[d.String()], nil
}

func schedule(t *testing.T, tr *domain.Terms, issued, amount string, cal domain.Calendar) []domain.Due {
	t.Helper()
	ds, err := tr.Schedule(context.Background(), date(issued), dec(amount), cal)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func expect(t *testing.T, got []domain.Due, want ...string) {
	t.Helper()
	if len(got) != len(want)/2 {
		t.Fatalf("installments: %+v", got)
	}
	for i := range got {
		if got[i].Date.String() != want[2*i] || !got[i].Amount.Equal(dec(want[2*i+1])) {
			t.Fatalf("installment %d: %s %s, want %s %s", i+1, got[i].Date, got[i].Amount, want[2*i], want[2*i+1])
		}
	}
}

func TestSchedule(t *testing.T) {
	if _, err := domain.ReconstituteTerms(domain.NewTermsID(), domain.TermsState{Seller: seller, Code: "X", Description: "X", Installments: 2}); err == nil {
		t.Fatal("several installments need days between them")
	}
	if _, err := domain.ReconstituteTerms(domain.NewTermsID(), domain.TermsState{Seller: seller, Code: "X", Description: "X", Installments: 1,
		NoPayment: domain.MonthRange{From: time.August}}); err == nil {
		t.Fatal("both months of the no payment period")
	}
	// 30/60/90 days: equal parts, the remainder on the last one.
	t369 := terms(t, domain.TermsState{Installments: 3, DaysToFirst: 30, DaysBetween: 30})
	expect(t, schedule(t, t369, "2026-01-15", "1000", nil), "2026-02-14", "333.33", "2026-03-16", "333.33", "2026-04-15", "333.34")
	// Commercial months: 30 days are a month.
	com := terms(t, domain.TermsState{Installments: 3, DaysToFirst: 30, DaysBetween: 30, CommercialMonth: true})
	expect(t, schedule(t, com, "2026-01-31", "100", nil), "2026-02-28", "33.33", "2026-03-31", "33.33", "2026-04-30", "33.34")
	// Fixed days 5 and 20; a fixed 31 clamps to the end of the month.
	fixed := terms(t, domain.TermsState{DaysToFirst: 30, FixedDays: []int{20, 5}})
	expect(t, schedule(t, fixed, "2026-01-15", "10", nil), "2026-02-20", "10")
	expect(t, schedule(t, fixed, "2026-01-25", "10", nil), "2026-03-05", "10")
	end := terms(t, domain.TermsState{DaysToFirst: 10, FixedDays: []int{31}})
	expect(t, schedule(t, end, "2026-02-10", "10", nil), "2026-02-28", "10")
	// No payment in August; the period may wrap around the year.
	august := terms(t, domain.TermsState{DaysToFirst: 30, NoPayment: domain.MonthRange{From: time.August, To: time.August}})
	expect(t, schedule(t, august, "2026-07-10", "10", nil), "2026-09-01", "10")
	augustFixed := terms(t, domain.TermsState{DaysToFirst: 30, FixedDays: []int{10}, NoPayment: domain.MonthRange{From: time.August, To: time.August}})
	expect(t, schedule(t, augustFixed, "2026-07-10", "10", nil), "2026-09-10", "10")
	winter := terms(t, domain.TermsState{DaysToFirst: 30, NoPayment: domain.MonthRange{From: time.December, To: time.January}})
	expect(t, schedule(t, winter, "2026-11-20", "10", nil), "2027-02-01", "10")
	// Holidays: back up to BackwardDays, else forward.
	cal := holidays{"2026-02-14": true, "2026-02-13": true}
	back1 := terms(t, domain.TermsState{DaysToFirst: 30, ControlHolidays: true, BackwardDays: 1})
	expect(t, schedule(t, back1, "2026-01-15", "10", cal), "2026-02-15", "10")
	back2 := terms(t, domain.TermsState{DaysToFirst: 30, ControlHolidays: true, BackwardDays: 2})
	expect(t, schedule(t, back2, "2026-01-15", "10", cal), "2026-02-12", "10")
	// A credit is due at once.
	expect(t, schedule(t, t369, "2026-01-15", "-36.41", nil), "2026-01-15", "-36.41")
}

func TestReceivable(t *testing.T) {
	customer := domain.PartyID{UUID: fw.NewUUID()}
	tr := terms(t, domain.TermsState{Installments: 2, DaysToFirst: 30, DaysBetween: 30})
	st := domain.ReceivableState{Seller: seller, Customer: customer, Number: "FA-2026-000001", Issued: date("2026-01-15"),
		Currency: vocab.MustCurrencyCode("EUR"), Total: dec("185.41")}
	r, err := domain.OpenReceivable(domain.ReceivableID{UUID: fw.NewUUID()}, st, schedule(t, tr, "2026-01-15", "185.41", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Open().Equal(dec("185.41")) || !r.Overdue(date("2026-03-01")).Equal(dec("92.70")) {
		t.Fatalf("open %s overdue %s", r.Open(), r.Overdue(date("2026-03-01")))
	}
	if err := r.Apply(1, dec("100")); !isViolation(err, "receivables.over_collected") {
		t.Fatalf("over collected: %v", err)
	}
	if err := r.Apply(1, dec("-1")); !isViolation(err, "receivables.amount_sign") {
		t.Fatalf("sign: %v", err)
	}
	if err := r.Apply(1, dec("92.70")); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(2, dec("92.71")); err != nil || !r.Settled() {
		t.Fatalf("settled: %v", err)
	}
	if err := r.Unapply(2, dec("50")); err != nil || r.Settled() || !r.Open().Equal(dec("50")) {
		t.Fatalf("unapply: %v", err)
	}
	if err := r.Unapply(2, dec("42.71")); err != nil {
		t.Fatal(err)
	}
	if err := r.Unapply(2, dec("0.01")); !isViolation(err, "receivables.unapply") {
		t.Fatal("cannot reverse more than collected")
	}
}

func TestCollectionAndCredit(t *testing.T) {
	payer := domain.PartyID{UUID: fw.NewUUID()}
	eur := vocab.MustCurrencyCode("EUR")
	if _, err := domain.RegisterCollection(domain.NewCollectionID(), domain.CollectionState{Seller: seller, Payer: payer, Date: date("2026-02-14"),
		Amount: dec("0"), Currency: eur, Method: domain.Transfer}); err == nil {
		t.Fatal("a positive amount")
	}
	c, err := domain.RegisterCollection(domain.NewCollectionID(), domain.CollectionState{Seller: seller, Payer: payer, Date: date("2026-02-14"),
		Amount: dec("100"), Currency: eur, Method: domain.Transfer, Reference: "TRF 123"})
	if err != nil {
		t.Fatal(err)
	}
	rec := domain.ReceivableID{UUID: fw.NewUUID()}
	id, err := c.Allocate(rec, 1, dec("60"), date("2026-02-14"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Allocate(rec, 2, dec("41"), date("2026-02-14")); !isViolation(err, "receivables.over_allocated") {
		t.Fatalf("over allocated: %v", err)
	}
	if a, err := c.Deallocate(id); err != nil || !a.Amount.Equal(dec("60")) || !c.Unallocated().Equal(dec("100")) {
		t.Fatal(err)
	}
	_, _ = c.Allocate(rec, 1, dec("40"), date("2026-02-14"))
	back, err := c.Cancel()
	if err != nil || len(back) != 1 || c.Unallocated().IsPositive() && !c.State().Cancelled {
		t.Fatal(err)
	}
	if _, err := c.Allocate(rec, 1, dec("1"), date("2026-02-14")); !isViolation(err, "receivables.collection_cancelled") {
		t.Fatal("cancelled")
	}

	off, _ := domain.RegisterCollection(domain.NewCollectionID(), domain.CollectionState{Seller: seller, Payer: payer, Date: date("2026-02-14"),
		Currency: eur, Method: domain.Offset})
	_, _ = off.Allocate(rec, 1, dec("36.41"), date("2026-02-14"))
	if off.Balanced() {
		t.Fatal("an offset balances credit and debit")
	}
	_, _ = off.Allocate(domain.ReceivableID{UUID: fw.NewUUID()}, 1, dec("-36.41"), date("2026-02-14"))
	if !off.Balanced() {
		t.Fatal("balanced offset")
	}

	cp, err := domain.ReconstituteCreditProfile(domain.NewCreditProfileID(), domain.CreditProfileState{Seller: seller, Customer: payer, Limit: dec("100")})
	if err == nil {
		t.Fatal("an unlimited profile has no limit amount")
	}
	cp, _ = domain.ReconstituteCreditProfile(domain.NewCreditProfileID(), domain.CreditProfileState{Seller: seller, Customer: payer})
	if _, limited := cp.Available(dec("1000")); limited {
		t.Fatal("no limit")
	}
	_ = cp.Set(domain.TermsID{}, true, dec("0"), false)
	if av, limited := cp.Available(dec("10")); !limited || !av.Equal(dec("-10")) {
		t.Fatal("a limit of zero is a limit")
	}
}

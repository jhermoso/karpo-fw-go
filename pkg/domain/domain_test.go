package domain_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

type PartyID struct{ domain.UUID }
type OrderID struct{ domain.UUID }
type InvoiceNo struct{ domain.LongID }

type Money struct {
	amount   int64
	currency string
}

func NewMoney(amount int64, currency string) (Money, error) {
	var v domain.Validation
	v.Require(amount >= 0, "amount", "range", "amount must not be negative")
	v.Require(len(currency) == 3, "currency", "format", "currency must be ISO 4217")
	if err := v.Err(); err != nil {
		return Money{}, err
	}
	return Money{amount: amount, currency: currency}, nil
}

type Account struct {
	domain.BaseAggregateRoot[PartyID]
	balance Money
}

type Deposited struct {
	domain.EventMeta
	Amount int64 `json:"amount"`
}

func (Deposited) EventType() string { return "test.deposited" }

func NewAccount(id PartyID) (*Account, error) {
	base, err := domain.NewBaseAggregateRoot("test.account", id)
	if err != nil {
		return nil, err
	}
	return &Account{BaseAggregateRoot: base}, nil
}

func (a *Account) Deposit(m Money) error {
	if a.balance.currency != "" && a.balance.currency != m.currency {
		return domain.Violation("account.currency", "currency mismatch")
	}
	a.balance = Money{amount: a.balance.amount + m.amount, currency: m.currency}
	a.Raise(Deposited{EventMeta: a.NewEventMeta(), Amount: m.amount})
	return nil
}

type Order struct {
	domain.BaseAggregateRoot[OrderID]
}

func TestUUID_FormatParseAndVersion(t *testing.T) {
	u := domain.NewUUID()
	if u.IsZero() || u[6]>>4 != 7 || u[8]>>6 != 2 {
		t.Fatalf("expected a version 7 RFC 9562 uuid, got %s", u)
	}
	for _, s := range []string{u.String(), "{" + u.String() + "}", u.String()[0:8] + u.String()[9:13] + u.String()[14:18] + u.String()[19:23] + u.String()[24:]} {
		p, err := domain.ParseUUID(s)
		if err != nil || p != u {
			t.Fatalf("parse %q: %v %v", s, p, err)
		}
	}
	if _, err := domain.ParseUUID("not-a-uuid"); !errors.Is(err, domain.ErrInvalidIdentity) {
		t.Fatalf("expected ErrInvalidIdentity, got %v", err)
	}
}

func TestUUID_IsMonotonic(t *testing.T) {
	prev := domain.NewUUID()
	for i := 0; i < 10000; i++ {
		next := domain.NewUUID()
		if next.String() <= prev.String() {
			t.Fatalf("uuid %d not increasing: %s <= %s", i, next, prev)
		}
		prev = next
	}
}

func TestTypedIdentifiers(t *testing.T) {
	id := PartyID{domain.NewUUID()}
	raw, err := json.Marshal(struct{ ID PartyID }{id})
	if err != nil {
		t.Fatal(err)
	}
	var back struct{ ID PartyID }
	if err := json.Unmarshal(raw, &back); err != nil || back.ID != id {
		t.Fatalf("json round trip failed: %s -> %v (%v)", raw, back.ID, err)
	}
	var _ domain.UUIDBacked = id
	var _ domain.LongBacked = InvoiceNo{42}
	if (InvoiceNo{42}).String() != "42" || !(InvoiceNo{}).IsZero() {
		t.Fatal("LongID behaviour")
	}
}

func TestEntity_RejectsZeroIdentityAndComparesByTypeAndID(t *testing.T) {
	if _, err := NewAccount(PartyID{}); !errors.Is(err, domain.ErrInvalidIdentity) {
		t.Fatalf("zero identity must be rejected, got %v", err)
	}
	u := domain.NewUUID()
	a1, _ := NewAccount(PartyID{u})
	a2, _ := NewAccount(PartyID{u})
	a3, _ := NewAccount(PartyID{domain.NewUUID()})
	if !domain.SameIdentity[PartyID](a1, a2) || domain.SameIdentity[PartyID](a1, a3) || domain.SameIdentity[PartyID](a1, nil) {
		t.Fatal("identity equality broken")
	}
	var nilAcc *Account
	if domain.SameIdentity[PartyID](a1, nilAcc) {
		t.Fatal("typed nil must not be equal")
	}
}

func TestAggregate_EventsVersionAndMetadata(t *testing.T) {
	restore := domain.SetClock(fixedClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
	defer restore()

	id := PartyID{domain.NewUUID()}
	acc, _ := NewAccount(id)
	if !acc.IsNew() || acc.Version() != 0 || acc.AggregateType() != "test.account" {
		t.Fatal("new aggregate state")
	}
	m, _ := NewMoney(100, "EUR")
	if err := acc.Deposit(m); err != nil {
		t.Fatal(err)
	}
	usd, _ := NewMoney(1, "USD")
	if err := acc.Deposit(usd); !errors.Is(err, domain.ErrRuleViolation) {
		t.Fatalf("expected rule violation, got %v", err)
	}

	evts := acc.PendingEvents()
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	meta := evts[0].Meta()
	if meta.AggregateID != id.String() || meta.AggregateType != "test.account" || meta.AggregateVersion != 1 ||
		!meta.OccurredAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) || meta.EventID == "" {
		t.Fatalf("unexpected metadata %+v", meta)
	}

	domain.MarkPersisted[PartyID](acc, 1)
	acc.ClearEvents()
	if acc.Version() != 1 || acc.IsNew() || len(acc.PendingEvents()) != 0 {
		t.Fatal("persisted state")
	}
	_ = acc.Deposit(m)
	if acc.PendingEvents()[0].Meta().AggregateVersion != 2 {
		t.Fatal("event version must be the version after the next save")
	}
}

func TestAggregateRoot_TypeSafety(t *testing.T) {
	// Compile-time guarantees: *Account is an AggregateRoot[PartyID] and *Order an
	// AggregateRoot[OrderID]; a repository of one cannot receive the other.
	var _ domain.AggregateRoot[PartyID] = (*Account)(nil)
	var _ domain.AggregateRoot[OrderID] = (*Order)(nil)
}

func TestValidation_AccumulatesAndMerges(t *testing.T) {
	_, err := NewMoney(-1, "EURO")
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || len(ve.Errors) != 2 || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected two field errors, got %v", err)
	}
	var outer domain.Validation
	outer.Merge("price", err)
	outer.Merge("note", errors.New("too long"))
	merged := outer.Err().(*domain.ValidationError)
	if len(merged.Errors) != 3 || merged.Errors[0].Field != "price.amount" || merged.Errors[2].Field != "note" {
		t.Fatalf("unexpected merge %+v", merged.Errors)
	}
	var none domain.Validation
	if none.Err() != nil || none.HasErrors() {
		t.Fatal("empty validation must be nil")
	}
}

func TestErrors_MatchSentinels(t *testing.T) {
	id := PartyID{domain.NewUUID()}
	cases := map[error]error{
		domain.NotFound("x", id):                                 domain.ErrNotFound,
		domain.Conflict("x", id, 3, "stale"):                     domain.ErrConflict,
		domain.Violation("c", "m"):                               domain.ErrRuleViolation,
		&domain.ValidationError{}:                                domain.ErrValidation,
		errors.Join(errors.New("ctx"), domain.NotFound("x", id)): domain.ErrNotFound,
	}
	for err, sentinel := range cases {
		if !errors.Is(err, sentinel) {
			t.Errorf("%v does not match %v", err, sentinel)
		}
	}
}

func TestPage(t *testing.T) {
	req := domain.PageRequest[*Account]{Number: 0, Size: 5000}.Normalize()
	if req.Number != 1 || req.Size != domain.MaxPageSize || req.Offset() != 0 {
		t.Fatalf("normalize: %+v", req)
	}
	p := domain.NewPage([]int{1, 2}, 45, 3, 20)
	if p.TotalPages != 3 {
		t.Fatalf("total pages %d", p.TotalPages)
	}
	s := domain.MapPage(p, func(i int) string { return string(rune('a' + i)) })
	if s.Items[1] != "c" || s.Total != 45 {
		t.Fatalf("map page %+v", s)
	}
}

func TestFactoryFunc(t *testing.T) {
	f := domain.FactoryFunc[*Account, PartyID](func(_ context.Context, id PartyID) (*Account, error) { return NewAccount(id) })
	acc, err := f.Create(context.Background(), PartyID{domain.NewUUID()})
	if err != nil || acc == nil {
		t.Fatal(err)
	}
}

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

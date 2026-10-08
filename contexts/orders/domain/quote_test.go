package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func quoteViolates(t *testing.T, err error, code string) {
	t.Helper()
	var rv *fw.RuleViolationError
	if !errors.As(err, &rv) || rv.Code != code {
		t.Fatalf("want %s: %v", code, err)
	}
}

func TestQuote_Lifecycle(t *testing.T) {
	dec := func(s string) vocab.Decimal { d, _ := vocab.ParseDecimal(s); return d }
	day := func(s string) vocab.Date { d, _ := vocab.ParseDate(s); return d }
	draft := func(until string) *domain.Quote {
		t.Helper()
		st := domain.QuoteState{Company: domain.OrganizationID{UUID: fw.NewUUID()}, Customer: domain.PartyID{UUID: fw.NewUUID()}, Date: day("2026-10-08"),
			CustomerDiscount: dec("5"), Status: domain.QuoteAccepted, Number: "X", Order: domain.NewOrderID()}
		if until != "" {
			st.ValidUntil = day(until)
		}
		q, err := domain.DraftQuote(domain.NewQuoteID(), st)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	line := domain.QuoteLine{Product: domain.ProductID{UUID: fw.NewUUID()}, SKU: "W", Description: "Tornillo", UoM: "ud", Quantity: dec("3"), UnitPrice: dec("10"),
		Discount: dec("10")}

	// A draft starts clean whatever it is given, and holds thirty days.
	q := draft("")
	if s := q.State(); s.Status != domain.QuoteDraft || s.Number != "" || !s.Order.IsZero() || s.ValidUntil != day("2026-11-07") {
		t.Fatalf("draft: %+v", s)
	}
	if _, err := domain.DraftQuote(domain.NewQuoteID(), domain.QuoteState{Company: q.State().Company, Customer: q.State().Customer, Date: day("2026-10-08"),
		ValidUntil: day("2026-10-07"), CustomerDiscount: dec("0")}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a validity before the date: %v", err)
	}
	quoteViolates(t, q.Send("PRE-1", day("2026-10-08")), "orders.quote_empty")
	if no, err := q.AddLine(line); err != nil || no != 1 || q.State().Lines[0].NetPrice.String() != "8.55" || q.Total().StringFixed(2) != "25.65" {
		t.Fatalf("line: %d %v %+v", no, err, q.State().Lines)
	}
	bad := line
	bad.Quantity = dec("0")
	if _, err := q.AddLine(bad); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("no quantity: %v", err)
	}
	if err := q.Change(day("2026-10-09"), day("2026-10-20"), " Ref ", ""); err != nil || q.State().Reference != "Ref" || q.State().ValidUntil != day("2026-10-20") {
		t.Fatalf("change: %v %+v", err, q.State())
	}
	if err := q.Change(day("2026-10-09"), day("2026-10-01"), "", ""); !errors.Is(err, fw.ErrValidation) || q.State().ValidUntil != day("2026-10-20") {
		t.Fatalf("a bad change leaves it as it was: %v", err)
	}
	quoteViolates(t, q.Accept(domain.NewOrderID(), day("2026-10-09")), "orders.quote_not_sent")
	quoteViolates(t, q.Reject(""), "orders.quote_not_sent")
	quoteViolates(t, q.Send("", day("2026-10-09")), "orders.number")
	quoteViolates(t, q.Send("PRE-1", day("2026-10-21")), "orders.quote_expired")
	if err := q.Send("PRE-1", day("2026-10-20")); err != nil || q.State().Status != domain.QuoteSent || len(q.PendingEvents()) != 1 {
		t.Fatalf("send on its last day: %v", err)
	}
	quoteViolates(t, q.Send("PRE-2", day("2026-10-20")), "orders.quote_not_draft")
	_, err := q.AddLine(line)
	quoteViolates(t, err, "orders.quote_not_draft")
	quoteViolates(t, q.RemoveLine(1), "orders.quote_not_draft")
	quoteViolates(t, q.Change(day("2026-10-09"), day("2026-10-20"), "", ""), "orders.quote_not_draft")

	// The last day counts; the next does not.
	if q.Due(day("2026-10-20")) || !q.Due(day("2026-10-21")) || q.Expire(day("2026-10-20")) {
		t.Fatal("due")
	}
	quoteViolates(t, q.Accept(domain.NewOrderID(), day("2026-10-21")), "orders.quote_expired")
	quoteViolates(t, q.Accept(domain.OrderID{}, day("2026-10-20")), "orders.quote_order")
	order := domain.NewOrderID()
	if err := q.Accept(order, day("2026-10-20")); err != nil || q.State().Status != domain.QuoteAccepted || q.State().Order != order || len(q.PendingEvents()) != 2 {
		t.Fatalf("accept: %v", err)
	}
	quoteViolates(t, q.Accept(order, day("2026-10-20")), "orders.quote_not_sent")
	quoteViolates(t, q.Withdraw(""), "orders.quote_closed")
	if q.Expire(day("2027-01-01")) {
		t.Fatal("an accepted quote does not expire")
	}

	// Expired, and a draft withdrawn without telling anyone.
	e := draft("2026-10-10")
	_, _ = e.AddLine(line)
	_ = e.Send("PRE-3", day("2026-10-08"))
	if !e.Expire(day("2026-10-11")) || e.State().Status != domain.QuoteExpired || e.Expire(day("2026-10-12")) || len(e.PendingEvents()) != 2 {
		t.Fatalf("expire: %+v", e.State())
	}
	w := draft("")
	if err := w.Withdraw(" no interesa "); err != nil || w.State().Status != domain.QuoteWithdrawn || w.State().Reason != "no interesa" || len(w.PendingEvents()) != 0 {
		t.Fatalf("withdraw a draft: %v %+v", err, w.State())
	}
	if _, err := domain.ReconstituteQuote(w.ID(), w.State()); err != nil {
		t.Fatalf("a withdrawn draft has no number: %v", err)
	}
	st := e.State()
	st.Number = ""
	if _, err := domain.ReconstituteQuote(e.ID(), st); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("what was sent has a number: %v", err)
	}
}

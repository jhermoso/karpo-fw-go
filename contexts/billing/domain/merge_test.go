package domain_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Unir borradores de albaranes (D-146): las líneas y los orígenes de todos, la última fecha de
// operación; las reglas que lo impiden.
func TestInvoice_Merge(t *testing.T) {
	seller, customer := domain.OrganizationID{UUID: fw.NewUUID()}, domain.PartyID{UUID: fw.NewUUID()}
	draft := func(ref string, day int, change func(*domain.InvoiceState)) *domain.Invoice {
		t.Helper()
		st := domain.InvoiceState{Seller: seller, Customer: customer, Kind: domain.Ordinary, Description: "Albarán " + ref,
			OperationDate: vocab.MustDate(2026, 10, day), Sources: []domain.Source{{Type: "orders.delivery", ID: fw.NewUUID().String(), Ref: ref}}}
		if change != nil {
			change(&st)
		}
		i, err := domain.DraftInvoice(domain.NewInvoiceID(), st)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := i.AddLine(domain.LineInput{Description: "Tornillo " + ref, Quantity: dec("1"), UnitPrice: dec("10"), TaxCode: "G21"}); err != nil {
			t.Fatal(err)
		}
		return i
	}

	a, b, c := draft("A-1", 9, nil), draft("A-2", 20, nil), draft("A-3", 4, nil)
	if err := a.Merge([]*domain.Invoice{b, c}, "Albaranes A-1, A-2, A-3"); err != nil {
		t.Fatal(err)
	}
	st := a.State()
	if len(st.Lines) != 3 || len(st.Sources) != 3 || st.Sources[1].Ref != "A-2" || st.OperationDate != vocab.MustDate(2026, 10, 20) ||
		st.Description != "Albaranes A-1, A-2, A-3" || !a.Net().Equal(dec("30")) {
		t.Fatalf("merged: %+v", st)
	}
	if err := a.Discard(); !isViolation(err, "billing.sourced_draft") {
		t.Fatalf("a merged draft is not discarded: %v", err)
	}

	issued := draft("A-9", 1, nil)
	if err := issued.Issue(domain.Issuance{Number: "FA-1", Date: vocab.MustDate(2026, 10, 2), Seller: domain.Identity{NIF: "A58818501"},
		Customer: domain.Identity{NIF: "12345678Z"}, Taxes: domain.Breakdown{Net: dec("10")}}); err != nil {
		t.Fatal(err)
	}
	for code, others := range map[string][]*domain.Invoice{
		"billing.merge_too_few":        nil,
		"billing.invoice_not_draft":    {issued},
		"billing.merge_unsourced":      {draft("", 5, func(s *domain.InvoiceState) { s.Sources = nil })},
		"billing.merge_other_customer": {draft("B-1", 5, func(s *domain.InvoiceState) { s.Customer = domain.PartyID{UUID: fw.NewUUID()} })},
		"billing.merge_calculation": {draft("A-6", 5, func(s *domain.InvoiceState) {
			s.Calculation = domain.Calculation{Rounding: domain.RoundOnce, PriceDecimals: 6}
		})},
		"billing.merge_surcharge": {draft("A-7", 5, func(s *domain.InvoiceState) { s.EquivalenceSurcharge = true })},
	} {
		x := draft("X-1", 1, nil)
		if err := x.Merge(others, "Albaranes"); !isViolation(err, code) {
			t.Fatalf("%s: %v", code, err)
		}
		if len(x.State().Sources) != 1 || len(x.State().Lines) != 1 {
			t.Fatalf("%s: a refused merge changes nothing", code)
		}
	}
	// Un mismo albarán no está dos veces en una factura.
	d := draft("D-1", 1, nil)
	twin := draft("D-1", 2, func(s *domain.InvoiceState) { s.Sources = d.State().Sources })
	if err := d.Merge([]*domain.Invoice{twin}, "Albaranes"); err == nil {
		t.Fatal("the same delivery note twice")
	}
}

// Un albarán pendiente del cierre del periodo tiene vendedor, cliente, número, fecha y líneas.
func TestPendingDelivery(t *testing.T) {
	st := domain.PendingDeliveryState{Seller: domain.OrganizationID{UUID: fw.NewUUID()}, Customer: domain.PartyID{UUID: fw.NewUUID()}, Number: " A-1 ",
		Date: vocab.MustDate(2026, 10, 2), Lines: []domain.LineInput{{Description: "Tornillo", Quantity: dec("1"), UnitPrice: dec("10"), TaxCode: "G21"}}}
	p, err := domain.ReconstitutePendingDelivery(domain.PendingDeliveryID{UUID: fw.NewUUID()}, st)
	if err != nil || p.State().Number != "A-1" {
		t.Fatal(p, err)
	}
	for name, change := range map[string]func(*domain.PendingDeliveryState){
		"number": func(s *domain.PendingDeliveryState) { s.Number = "" },
		"date":   func(s *domain.PendingDeliveryState) { s.Date = vocab.Date{} },
		"lines":  func(s *domain.PendingDeliveryState) { s.Lines = nil },
	} {
		bad := st
		change(&bad)
		if _, err := domain.ReconstitutePendingDelivery(domain.PendingDeliveryID{UUID: fw.NewUUID()}, bad); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func TestSeries(t *testing.T) {
	seller := domain.OrganizationID{UUID: fw.NewUUID()}
	s, err := domain.OpenSeries(domain.NewSeriesID(), domain.SeriesState{Seller: seller, Code: " fa ", Year: 2026})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.Take(seller, 2026, false); err != nil || n != "FA-2026-000001" {
		t.Fatal(n, err)
	}
	if n, _ := s.Take(seller, 2026, false); n != "FA-2026-000002" {
		t.Fatal(n)
	}
	for code, try := range map[string]func() error{
		"billing.series_other_seller": func() error { _, err := s.Take(domain.OrganizationID{UUID: fw.NewUUID()}, 2026, false); return err },
		"billing.series_other_year":   func() error { _, err := s.Take(seller, 2027, false); return err },
		"billing.series_kind":         func() error { _, err := s.Take(seller, 2026, true); return err },
	} {
		if err := try(); !isViolation(err, code) {
			t.Fatalf("%s: %v", code, err)
		}
	}
	s.Close()
	if _, err := s.Take(seller, 2026, false); !isViolation(err, "billing.series_closed") {
		t.Fatal(err)
	}
	if s.State().Last != 2 {
		t.Fatal("a refused take does not consume a number")
	}
}

func TestInvoice(t *testing.T) {
	st := domain.InvoiceState{Seller: domain.OrganizationID{UUID: fw.NewUUID()}, Customer: domain.PartyID{UUID: fw.NewUUID()}, Kind: domain.Ordinary}
	inv, err := domain.DraftInvoice(domain.NewInvoiceID(), st)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inv.AddLine(domain.LineInput{Description: "X", Quantity: dec("-1"), UnitPrice: dec("1"), TaxCode: "G21"}); err == nil {
		t.Fatal("negative quantities only in corrective invoices")
	}
	if _, err := inv.AddLine(domain.LineInput{Description: "X", Quantity: dec("1"), UnitPrice: dec("1")}); err == nil {
		t.Fatal("a tax code or a treatment is required")
	}
	// 3 × 33.335 = 100.005, less 10 % = 90.0045 → 90.00
	if _, err := inv.AddLine(domain.LineInput{Description: "Consultoría", Quantity: dec("3"), UnitPrice: dec("33.335"), Discount: dec("10"), TaxCode: "g21"}); err != nil {
		t.Fatal(err)
	}
	if _, err := inv.AddLine(domain.LineInput{Description: "Formación", Quantity: dec("1"), UnitPrice: dec("50"), Treatment: "e1"}); err != nil {
		t.Fatal(err)
	}
	lines := inv.State().Lines
	if !lines[0].Net.Equal(dec("90")) || lines[0].TaxCode != "G21" || lines[1].Treatment != "E1" || !inv.Net().Equal(dec("140")) {
		t.Fatalf("lines: %+v", lines)
	}
	seller := domain.Identity{NIF: "A58818501", Name: "ACME SA", Country: "ES"}
	customer := domain.Identity{NIF: "12345678Z", Name: "GARCIA ANA", Country: "ES"}
	taxes := domain.Breakdown{Country: "ES", Net: dec("140"), Tax: dec("18.90"), Surcharge: dec("0")}
	if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: seller, Customer: customer,
		Taxes: domain.Breakdown{Net: dec("100")}}); !isViolation(err, "billing.breakdown_mismatch") {
		t.Fatalf("breakdown: %v", err)
	}
	if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: domain.Identity{NIF: "X"},
		Customer: customer, Taxes: taxes}); !isViolation(err, "billing.seller_nif") {
		t.Fatalf("seller nif: %v", err)
	}
	_ = inv.SetDetails(domain.Details{DueDate: vocab.MustDate(2026, 9, 1)})
	if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: seller, Customer: customer,
		Taxes: taxes}); !isViolation(err, "billing.due_before_issue") {
		t.Fatalf("due date: %v", err)
	}
	_ = inv.SetDetails(domain.Details{DueDate: vocab.MustDate(2026, 10, 28)})
	if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: seller, Customer: customer,
		Taxes: taxes}); err != nil {
		t.Fatal(err)
	}
	if !inv.State().Taxes.Total().Equal(dec("158.90")) || len(inv.PendingEvents()) != 1 {
		t.Fatal("total and event")
	}
	if _, err := inv.AddLine(domain.LineInput{Description: "X", Quantity: dec("1"), UnitPrice: dec("1"), TaxCode: "G21"}); !isViolation(err, "billing.invoice_not_draft") {
		t.Fatal("issued invoices are immutable")
	}
	if inv.Discard() == nil {
		t.Fatal("issued invoices are never deleted")
	}

	if _, err := domain.DraftInvoice(domain.NewInvoiceID(), domain.InvoiceState{Seller: st.Seller, Customer: st.Customer, Kind: domain.Corrective,
		Corrects: inv.ID(), Reason: "R9"}); err == nil {
		t.Fatal("reasons R1–R5")
	}
	fix, err := domain.DraftInvoice(domain.NewInvoiceID(), domain.InvoiceState{Seller: st.Seller, Customer: st.Customer, Kind: domain.Corrective,
		Corrects: inv.ID(), Reason: "R1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fix.AddLine(domain.LineInput{Description: "Abono consultoría", Quantity: dec("-1"), UnitPrice: dec("30"), TaxCode: "G21"}); err != nil {
		t.Fatal(err)
	}
	if !fix.Net().Equal(dec("-30")) {
		t.Fatal("negative correction")
	}
}

package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
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

func iban(s string) vocab.IBAN {
	i, err := vocab.NewIBAN(s)
	if err != nil {
		panic(err)
	}
	return i
}

var (
	company  = domain.OrganizationID{UUID: fw.NewUUID()}
	supplier = domain.PartyID{UUID: fw.NewUUID()}
)

// A professional's invoice: 1000 of services and 200 of supplies at 21%, 15% withheld.
func draft() domain.Draft {
	return domain.Draft{Company: company, Supplier: supplier, SupplierNumber: " 2026/0042 ", Issued: date("2026-09-30"), Received: date("2026-10-02"),
		Due: date("2026-10-30"), WithholdingRate: dec("15"), DeclaredTotal: dec("1452.00"),
		Lines: []domain.Line{{No: 1, Description: "Asesoría", Category: domain.ProfessionalServices, Base: dec("1000"), TaxCode: "G21"},
			{No: 2, Description: "Material", Category: domain.Supplies, Base: dec("200"), TaxCode: "G21"}}}
}

func breakdown(net, tax string) domain.Breakdown {
	return domain.Breakdown{Country: "ES", Net: dec(net), Tax: dec(tax),
		Lines: []domain.TaxLine{{TaxType: "vat", TaxCode: "G21", TreatmentKind: "subject", Rate: dec("21"), Base: dec(net), Amount: dec(tax)}}}
}

func TestRegister(t *testing.T) {
	bad := []func(*domain.Draft){
		func(d *domain.Draft) { d.SupplierNumber = "" },
		func(d *domain.Draft) { d.Received = date("2026-09-29") },
		func(d *domain.Draft) { d.Due = date("2026-09-29") },
		func(d *domain.Draft) { d.Lines[0].Category = "food" },
		func(d *domain.Draft) { d.Lines[1].TaxCode = "" },
		func(d *domain.Draft) { d.Lines[1].No = 3 },
		func(d *domain.Draft) { d.WithholdingRate = dec("51") },
	}
	for i, f := range bad {
		d := draft()
		f(&d)
		if _, err := domain.Register(domain.NewInvoiceID(), d, breakdown("1200", "252"), 2026, 1); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if _, err := domain.Register(domain.NewInvoiceID(), draft(), breakdown("1100", "231"), 2026, 1); !isViolation(err, "purchases.breakdown") {
		t.Fatalf("breakdown: %v", err)
	}
	d := draft()
	d.DeclaredTotal = dec("1452.01")
	if _, err := domain.Register(domain.NewInvoiceID(), d, breakdown("1200", "252"), 2026, 1); !isViolation(err, "purchases.total_mismatch") {
		t.Fatalf("total: %v", err)
	}
	d = draft()
	d.Lines[0].Category = domain.Supplies
	if _, err := domain.Register(domain.NewInvoiceID(), d, breakdown("1200", "252"), 2026, 1); !isViolation(err, "purchases.withholding") {
		t.Fatalf("withholding without professional services: %v", err)
	}
	d = draft()
	d.PayTo = []domain.PayTo{{IBAN: iban("ES9121000418450200051332"), Amount: dec("1452.00")}}
	if _, err := domain.Register(domain.NewInvoiceID(), d, breakdown("1200", "252"), 2026, 1); !isViolation(err, "purchases.pay_to") {
		t.Fatalf("pay to the total, not what is paid: %v", err)
	}
	d.PayTo[0].Amount = dec("1272.00")
	inv, err := domain.Register(domain.NewInvoiceID(), d, breakdown("1200", "252"), 2026, 7)
	if err != nil {
		t.Fatal(err)
	}
	s := inv.State()
	if s.Register != "FR-2026-000007" || s.SupplierNumber != "2026/0042" || s.Withholding.String() != "180" || inv.Total().String() != "1452" ||
		inv.Payable().String() != "1272" {
		t.Fatalf("registered: %+v total %s payable %s", s, inv.Total(), inv.Payable())
	}
	exp, deductible := inv.Expenses()
	if len(exp) != 2 || exp[0].Amount.String() != "1000" || exp[1].Amount.String() != "200" || deductible.String() != "252" {
		t.Fatalf("expenses: %+v %s", exp, deductible)
	}
	if err := inv.Cancel(" "); !isViolation(err, "purchases.cancel_reason") {
		t.Fatalf("reason: %v", err)
	}
	if err := inv.Cancel("duplicada"); err != nil {
		t.Fatal(err)
	}
	if err := inv.Cancel("otra vez"); !isViolation(err, "purchases.invoice_cancelled") {
		t.Fatalf("twice: %v", err)
	}
}

func TestNonDeductibleTaxIsShared(t *testing.T) {
	d := draft()
	d.WithholdingRate, d.NonDeductible = dec("0"), true
	d.Lines = append(d.Lines, domain.Line{No: 3, Category: domain.Supplies, Base: dec("100.01"), TaxCode: "G21"})
	d.Lines[0].Base = dec("333.33")
	// net 633.34 at 21% = 133.00
	d.DeclaredTotal = dec("766.34")
	inv, err := domain.Register(domain.NewInvoiceID(), d, breakdown("633.34", "133.00"), 2026, 1)
	if err != nil {
		t.Fatal(err)
	}
	exp, deductible := inv.Expenses()
	sum := vocab.DecimalFromInt(0)
	for _, e := range exp {
		sum = sum.Add(e.Amount)
	}
	if len(exp) != 2 || !deductible.IsZero() || sum.String() != "766.34" || exp[0].Amount.String() != "403.33" {
		t.Fatalf("non deductible: %+v %s", exp, deductible)
	}
}

func TestCorrectiveInvoice(t *testing.T) {
	d := draft()
	d.WithholdingRate, d.Corrects = dec("0"), domain.NewInvoiceID()
	if _, err := domain.Register(domain.NewInvoiceID(), d, breakdown("1200", "252"), 2026, 1); !isViolation(err, "purchases.corrective_sign") {
		t.Fatalf("positive corrective: %v", err)
	}
	d.Lines = d.Lines[:1]
	d.Lines[0].Base, d.DeclaredTotal = dec("-100"), dec("-121")
	inv, err := domain.Register(domain.NewInvoiceID(), d, breakdown("-100", "-21"), 2026, 2)
	if err != nil || !inv.Payable().Equal(dec("-121")) {
		t.Fatalf("corrective: %v", err)
	}
}

func TestSupplierProfile(t *testing.T) {
	if _, err := domain.ReconstituteSupplier(domain.NewSupplierID(), domain.SupplierState{Company: company, Supplier: supplier, Category: "food"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("category: %v", err)
	}
	p, err := domain.ReconstituteSupplier(domain.NewSupplierID(), domain.SupplierState{Company: company, Supplier: supplier, Category: domain.Rent, PaymentDays: 30})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Change(domain.SupplierState{PaymentDays: 400}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("days: %v", err)
	}
	if err := p.Change(domain.SupplierState{Category: domain.ProfessionalServices, WithholdingRate: dec("15"), Blocked: true}); err != nil ||
		p.State().Supplier != supplier || !p.State().Blocked {
		t.Fatalf("change: %v %+v", err, p.State())
	}
}

func TestCounter(t *testing.T) {
	c, err := domain.ReconstituteCounter(domain.NewCounterID(), company, 2026, 41)
	if err != nil || c.Next() != 42 {
		t.Fatalf("counter: %v", err)
	}
	if _, err := domain.ReconstituteCounter(domain.NewCounterID(), company, 1980, 0); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("year: %v", err)
	}
}

package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
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
	company = domain.OrganizationID{UUID: fw.NewUUID()}
	payee   = domain.PartyID{UUID: fw.NewUUID()}
	eur     = vocab.MustCurrencyCode("EUR")
)

func supplierInvoice(amount string) domain.PayableState {
	return domain.PayableState{Company: company, Payee: payee, Kind: domain.SupplierInvoice, Source: domain.Source{Type: "supplier-invoice", ID: fw.NewUUID().String()},
		Document: "F-001", Issued: date("2026-09-01"), Due: date("2026-10-01"), Currency: eur, Amount: dec(amount)}
}

func TestPayable_Rules(t *testing.T) {
	bad := []func(*domain.PayableState){
		func(s *domain.PayableState) { s.Payee = domain.PartyID{} },
		func(s *domain.PayableState) { s.Amount = dec("0") },
		func(s *domain.PayableState) { s.Amount = dec("10.001") },
		func(s *domain.PayableState) { s.Due = date("2026-08-31") },
		func(s *domain.PayableState) { s.Document = " " },
		func(s *domain.PayableState) { s.Currency = vocab.MustCurrencyCode("USD") },
		func(s *domain.PayableState) {
			s.PayTo = []domain.PayTo{{IBAN: iban("ES9121000418450200051332"), Amount: dec("99")}}
		},
		func(s *domain.PayableState) { s.Kind = domain.Tax },
	}
	for i, f := range bad {
		s := supplierInvoice("100")
		f(&s)
		if _, err := domain.RegisterPayable(domain.NewPayableID(), s); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	tax := supplierInvoice("216")
	tax.Kind, tax.Payee, tax.Authority = domain.Tax, domain.PartyID{}, "AEAT"
	p, err := domain.RegisterPayable(domain.NewPayableID(), tax)
	if err != nil || p.Payee() != "AEAT" {
		t.Fatalf("tax payable: %v", err)
	}
}

func TestPayable_ApplyUnapplyCancel(t *testing.T) {
	s := supplierInvoice("100")
	s.PayTo = []domain.PayTo{{IBAN: iban("ES9121000418450200051332"), Amount: dec("60")}, {IBAN: iban("ES7921000813610123456789"), Amount: dec("40")}}
	p, err := domain.RegisterPayable(domain.NewPayableID(), s)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Apply(dec("100.01")); !isViolation(err, "payments.over_paid") {
		t.Fatalf("over paid: %v", err)
	}
	if err := p.Apply(dec("0")); !isViolation(err, "payments.amount") {
		t.Fatalf("zero: %v", err)
	}
	if err := p.Apply(dec("30")); err != nil || p.Open().String() != "70" {
		t.Fatalf("apply: %v %s", err, p.Open())
	}
	if err := p.Cancel("error"); !isViolation(err, "payments.payable_paid") {
		t.Fatalf("cancel paid: %v", err)
	}
	if err := p.Apply(dec("70")); err != nil || !p.Settled() {
		t.Fatalf("settle: %v", err)
	}
	if err := p.SetPayTo(nil); !isViolation(err, "payments.payable_closed") {
		t.Fatalf("pay-to of a settled payable: %v", err)
	}
	if err := p.Unapply(dec("100.01")); !isViolation(err, "payments.unapply") {
		t.Fatalf("unapply too much: %v", err)
	}
	if err := p.Unapply(dec("100")); err != nil || p.Settled() {
		t.Fatalf("unapply: %v", err)
	}
	if err := p.Cancel("duplicated"); err != nil || !p.Open().IsZero() {
		t.Fatalf("cancel: %v", err)
	}
	if err := p.Apply(dec("1")); !isViolation(err, "payments.payable_cancelled") {
		t.Fatalf("apply to cancelled: %v", err)
	}
}

func TestPayment_AllocateAndCancel(t *testing.T) {
	p, err := domain.RegisterPayment(domain.NewPaymentID(), domain.PaymentState{Company: company, Payee: payee, Date: date("2026-10-01"), Amount: dec("50"),
		Currency: eur, Method: domain.Transfer})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.RegisterPayment(domain.NewPaymentID(), domain.PaymentState{Company: company, Payee: payee, Authority: "AEAT", Date: date("2026-10-01"),
		Amount: dec("50"), Currency: eur, Method: domain.Transfer}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("payee and authority: %v", err)
	}
	a := domain.NewPayableID()
	if _, err := p.Allocate(a, domain.SupplierInvoice, dec("30"), date("2026-10-01")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Allocate(a, domain.SupplierInvoice, dec("20.01"), date("2026-10-01")); !isViolation(err, "payments.over_allocated") {
		t.Fatalf("over allocated: %v", err)
	}
	id, err := p.Allocate(domain.NewPayableID(), domain.SupplierInvoice, dec("20"), date("2026-10-01"))
	if err != nil || !p.Unallocated().IsZero() {
		t.Fatalf("allocate: %v", err)
	}
	if back, err := p.Deallocate(id); err != nil || back.Amount.String() != "20" {
		t.Fatalf("deallocate: %v", err)
	}
	out, err := p.Cancel()
	if err != nil || len(out) != 1 || !p.State().Cancelled {
		t.Fatalf("cancel: %v %+v", err, out)
	}
	if _, err := p.Cancel(); !isViolation(err, "payments.payment_cancelled") {
		t.Fatalf("cancel twice: %v", err)
	}
	if _, err := p.Allocate(a, domain.SupplierInvoice, dec("1"), date("2026-10-01")); !isViolation(err, "payments.payment_cancelled") {
		t.Fatalf("allocate cancelled: %v", err)
	}
}

func TestTaxDue(t *testing.T) {
	for period, want := range map[string]string{"01": "2026-02-20", "11": "2026-12-20", "12": "2027-01-30", "1T": "2026-04-20", "3T": "2026-10-20", "4T": "2027-01-30"} {
		if d, err := domain.TaxDue(2026, period); err != nil || d.String() != want {
			t.Fatalf("%s: %s %v", period, d, err)
		}
	}
	if _, err := domain.TaxDue(2026, "0A"); !isViolation(err, "payments.tax_period") {
		t.Fatalf("annual: %v", err)
	}
}

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
	s, err := domain.OpenSeries(domain.NewSeriesID(), domain.SeriesState{Seller: seller, Code: " fa ", Year: 2026}, "")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.Take(seller, vocab.MustDate(2026, 3, 10), false); err != nil || n != "FA-2026-000001" {
		t.Fatal(n, err)
	}
	if n, _ := s.Take(seller, vocab.MustDate(2026, 3, 10), false); n != "FA-2026-000002" {
		t.Fatal(n)
	}
	for code, try := range map[string]func() error{
		"billing.series_other_seller": func() error {
			_, err := s.Take(domain.OrganizationID{UUID: fw.NewUUID()}, vocab.MustDate(2026, 3, 10), false)
			return err
		},
		"billing.series_other_year":     func() error { _, err := s.Take(seller, vocab.MustDate(2027, 1, 1), false); return err },
		"billing.series_kind":           func() error { _, err := s.Take(seller, vocab.MustDate(2026, 3, 10), true); return err },
		"billing.series_date_backwards": func() error { _, err := s.Take(seller, vocab.MustDate(2026, 3, 9), false); return err },
	} {
		if err := try(); !isViolation(err, code) {
			t.Fatalf("%s: %v", code, err)
		}
	}
	s.Close()
	if _, err := s.Take(seller, vocab.MustDate(2026, 3, 10), false); !isViolation(err, "billing.series_closed") {
		t.Fatal(err)
	}
	if st := s.State(); st.Last != 2 || st.LastIssueDate != vocab.MustDate(2026, 3, 10) {
		t.Fatal("a refused take does not consume a number")
	}
}

func TestInvoice(t *testing.T) {
	st := domain.InvoiceState{Seller: domain.OrganizationID{UUID: fw.NewUUID()}, Customer: domain.PartyID{UUID: fw.NewUUID()}, Kind: domain.Ordinary,
		Description: "Servicios de septiembre"}
	bare, err := domain.DraftInvoice(domain.NewInvoiceID(), domain.InvoiceState{Seller: st.Seller, Customer: st.Customer, Kind: domain.Ordinary})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.AddLine(domain.LineInput{Description: "Hora", Quantity: dec("1"), UnitPrice: dec("10"), TaxCode: "G21"}); err != nil {
		t.Fatal(err)
	}
	if err := bare.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28)}); !isViolation(err, "billing.description_required") {
		t.Fatalf("an invoice without a description: %v", err)
	}
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
	// Las escalas de la columna: cantidad y precio con 4 decimales, descuento con 4.
	for _, in := range []domain.LineInput{
		{Description: "X", Quantity: dec("1.00001"), UnitPrice: dec("1"), TaxCode: "G21"},
		{Description: "X", Quantity: dec("1"), UnitPrice: dec("0.00001"), TaxCode: "G21"},
		{Description: "X", Quantity: dec("1"), UnitPrice: dec("1"), Discount: dec("12.12345"), TaxCode: "G21"},
	} {
		if _, err := inv.AddLine(in); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("more decimals than the column: %+v: %v", in, err)
		}
	}
	// 3 × 33.335 = 100.005, menos el 10 % = 90.0045 → 90.00
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
	seller := domain.Identity{NIF: "A58818501", Name: "ACME SA", Country: "ES",
		Address: domain.Address{Street: "Calle Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}}
	customer := domain.Identity{NIF: "12345678Z", Name: "GARCIA ANA", Country: "ES",
		Address: domain.Address{Street: "Rúa Nova 2", PostalCode: "15705", Locality: "Santiago", Country: "ES"}}
	taxes := domain.Breakdown{Country: "ES", Net: dec("140"), Tax: dec("18.90"), Surcharge: dec("0")}
	if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: seller, Customer: customer,
		Taxes: domain.Breakdown{Net: dec("100")}}); !isViolation(err, "billing.breakdown_mismatch") {
		t.Fatalf("breakdown: %v", err)
	}
	if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: domain.Identity{NIF: "X"},
		Customer: customer, Taxes: taxes}); !isViolation(err, "billing.seller_nif") {
		t.Fatalf("seller nif: %v", err)
	}
	// El domicilio fiscal de las dos partes es obligatorio (D-147).
	noAddress := func(id domain.Identity) domain.Identity { id.Address = domain.Address{}; return id }
	if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: noAddress(seller),
		Customer: customer, Taxes: taxes}); !isViolation(err, "billing.seller_address_required") {
		t.Fatalf("seller address: %v", err)
	}
	if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: seller,
		Customer: noAddress(customer), Taxes: taxes}); !isViolation(err, "billing.customer_address_required") {
		t.Fatalf("customer address: %v", err)
	}
	// Al menos la calle y el país; la localidad todavía no se exige.
	for _, a := range []domain.Address{{Street: "  ", Country: "ES"}, {Street: "Rúa Nova 2", Locality: "Santiago"}, {Street: "Rúa Nova 2", Country: " "}} {
		partial := customer
		partial.Address = a
		if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: seller,
			Customer: partial, Taxes: taxes}); !isViolation(err, "billing.customer_address_required") {
			t.Fatalf("an incomplete address %+v: %v", a, err)
		}
	}
	if err := domain.RequireAddresses(domain.Ordinary, seller, domain.Identity{Address: domain.Address{Street: "Rúa Nova 2", Country: "ES"}}); err != nil {
		t.Fatalf("street and country are enough: %v", err)
	}
	_ = inv.SetDetails(domain.Details{Description: "Servicios de septiembre", DueDate: vocab.MustDate(2026, 9, 1)})
	if err := inv.Issue(domain.Issuance{Number: "FA-2026-000001", Date: vocab.MustDate(2026, 9, 28), Seller: seller, Customer: customer,
		Taxes: taxes}); !isViolation(err, "billing.due_before_issue") {
		t.Fatalf("due date: %v", err)
	}
	_ = inv.SetDetails(domain.Details{Description: "Servicios de septiembre", DueDate: vocab.MustDate(2026, 10, 28)})
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
		Corrects: inv.ID(), Reason: "R1", Description: "Abono"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fix.AddLine(domain.LineInput{Description: "Abono consultoría", Quantity: dec("-1"), UnitPrice: dec("30"), TaxCode: "G21"}); err != nil {
		t.Fatal(err)
	}
	if !fix.Net().Equal(dec("-30")) {
		t.Fatal("negative correction")
	}
	// La rectificativa no exige domicilio: lleva el de la factura que rectifica (D-147).
	if err := fix.Issue(domain.Issuance{Number: "FR-2026-000001", Date: vocab.MustDate(2026, 9, 29), Seller: noAddress(seller),
		Customer: noAddress(customer), Taxes: domain.Breakdown{Country: "ES", Net: dec("-30"), Tax: dec("-6.30"), Surcharge: dec("0")}}); err != nil {
		t.Fatalf("a corrective invoice without addresses: %v", err)
	}
}

// El código de una serie: sin patrón de la empresa, la regla fija (sin espacios ni barras); con
// patrón, el que diga el patrón, entero y en mayúsculas (D-129, D-144).
func TestSeries_CodePattern(t *testing.T) {
	seller := domain.OrganizationID{UUID: fw.NewUUID()}
	open := func(code, pattern string) error {
		_, err := domain.OpenSeries(domain.NewSeriesID(), domain.SeriesState{Seller: seller, Code: code, Year: 2026}, pattern)
		return err
	}
	if err := open("F/A", ""); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("no slash without a pattern: %v", err)
	}
	for code, pattern := range map[string]string{"FA/26": `F[A-Z]/[0-9]{2}`, "FA-1": `F[A-Z]-[0-9]`, "R": `R|FR`, " fx ": `f[a-z]`} {
		if err := open(code, pattern); err != nil {
			t.Fatalf("%s with %s: %v", code, pattern, err)
		}
	}
	// El patrón se comprueba sobre el código tal como se escribe; después se guarda en mayúsculas.
	if s, _ := domain.OpenSeries(domain.NewSeriesID(), domain.SeriesState{Seller: seller, Code: "fx", Year: 2026}, `f[a-z]`); s.State().Code != "FX" {
		t.Fatalf("stored in capitals: %s", s.State().Code)
	}
	// Unos paréntesis desequilibrados no se saltan el anclaje: «A)|(.*» no compila solo.
	for code, pattern := range map[string]string{"FA": `F[A-Z]/[0-9]{2}`, "XFR": `R|FR`, "FRX": `R|FR`, "fa/26": `F[A-Z]/[0-9]{2}`,
		"ANYTHING": `A)|(.*`} {
		if err := open(code, pattern); !isViolation(err, "billing.series_code_pattern") {
			t.Fatalf("%s with %s: %v", code, pattern, err)
		}
	}
	// Ni espacios ni caracteres de control, aunque el patrón los admita.
	for _, code := range []string{"F A", "F\tA", "F\u00a0A", "F\x01A"} {
		if err := open(code, `.+`); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("%q: %v", code, err)
		}
	}
	if err := open("ABCDEFGHIJKLMNOPQRSTU", `[A-Z]+`); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("the column still has 20 characters: %v", err)
	}
}

// Cómo se calcula una línea (D-128, D-144): una vez al final o el bruto antes, y el precio con los
// decimales del cálculo de la factura, que es el de la empresa al preparar el borrador.
func TestInvoice_Calculation(t *testing.T) {
	draft := func(c domain.Calculation) *domain.Invoice {
		t.Helper()
		inv, err := domain.DraftInvoice(domain.NewInvoiceID(), domain.InvoiceState{Seller: domain.OrganizationID{UUID: fw.NewUUID()},
			Customer: domain.PartyID{UUID: fw.NewUUID()}, Kind: domain.Ordinary, Calculation: c})
		if err != nil {
			t.Fatal(err)
		}
		return inv
	}
	if c := draft(domain.Calculation{}).State().Calculation; c != domain.DefaultCalculation() {
		t.Fatalf("without a calculation, the usual one: %+v", c)
	}
	// 1 × 0.125 con un 50 %: 0.0625 → 0.06 una vez; 0.125 → 0.13 y 0.065 → 0.07 redondeando el bruto.
	line := domain.LineInput{Description: "Tornillo", Quantity: dec("1"), UnitPrice: dec("0.125"), Discount: dec("50"), TaxCode: "G21"}
	for rounding, want := range map[domain.LineRounding]string{domain.RoundOnce: "0.06", domain.RoundGrossThenDiscount: "0.07"} {
		inv := draft(domain.Calculation{Rounding: rounding, PriceDecimals: 4})
		if _, err := inv.AddLine(line); err != nil {
			t.Fatal(err)
		}
		if got := inv.State().Lines[0].Net; !got.Equal(dec(want)) {
			t.Fatalf("%s: %s, want %s", rounding, got, want)
		}
	}
	two := draft(domain.Calculation{Rounding: domain.RoundOnce, PriceDecimals: 2})
	if _, err := two.AddLine(line); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("3 decimals in a price of 2: %v", err)
	}
	six := draft(domain.Calculation{Rounding: domain.RoundOnce, PriceDecimals: 6})
	if _, err := six.AddLine(domain.LineInput{Description: "Kilovatio", Quantity: dec("1000"), UnitPrice: dec("0.123456"), TaxCode: "G21"}); err != nil {
		t.Fatal(err)
	}
	if got := six.State().Lines[0].Net; !got.Equal(dec("123.46")) {
		t.Fatalf("1000 × 0.123456: %s", got)
	}
	if _, err := six.AddLine(domain.LineInput{Description: "X", Quantity: dec("1"), UnitPrice: dec("0.1234567"), TaxCode: "G21"}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("7 decimals: %v", err)
	}
	for _, c := range []domain.Calculation{{Rounding: "half_even", PriceDecimals: 2}, {Rounding: domain.RoundOnce, PriceDecimals: 7},
		{Rounding: domain.RoundOnce, PriceDecimals: -1}} {
		if _, err := domain.DraftInvoice(domain.NewInvoiceID(), domain.InvoiceState{Seller: domain.OrganizationID{UUID: fw.NewUUID()},
			Customer: domain.PartyID{UUID: fw.NewUUID()}, Kind: domain.Ordinary, Calculation: c}); !errors.Is(err, fw.ErrValidation) {
			t.Fatalf("%+v: %v", c, err)
		}
	}
}

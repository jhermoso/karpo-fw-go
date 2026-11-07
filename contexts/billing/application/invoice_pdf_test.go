package application

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/render/pdf"
)

func TestSpanishNumbers(t *testing.T) {
	for in, want := range map[string]string{"1234.56": "1.234,56 €", "-36.41": "-36,41 €", "0": "0,00 €", "1234567.5": "1.234.567,50 €", "100": "100,00 €"} {
		if got := euros(vocab.MustDecimal(in)); got != want {
			t.Errorf("euros(%s) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"2.5": "2,5", "21": "21", "5.2": "5,2", "1000": "1.000", "-3": "-3"} {
		if got := esDecimal(vocab.MustDecimal(in), 0); got != want {
			t.Errorf("esDecimal(%s) = %q, want %q", in, got, want)
		}
	}
	if got := esDecimal(vocab.MustDecimal("19.9990"), 2); got != "19,999" {
		t.Errorf("price: %q", got)
	}
	if got := esDecimal(vocab.MustDecimal("10"), 2); got != "10,00" {
		t.Errorf("price: %q", got)
	}
	if got := esDate(vocab.MustDate(2026, 9, 5)); got != "05/09/2026" {
		t.Errorf("date: %q", got)
	}
}

// Una factura que no cabe en una página sigue en otras, con la cabecera de las líneas repetida y
// el número de cada página.
func TestRenderInvoice_SeveralPages(t *testing.T) {
	st := domain.InvoiceState{Kind: domain.Ordinary, Number: "FA-2026-000009", IssueDate: vocab.MustDate(2026, 9, 28), Description: "Muchas horas",
		SellerIdentity: domain.Identity{NIF: "A58818501", Name: "Acme"}, CustomerIdentity: domain.Identity{NIF: "12345678Z", Name: "Ana"}}
	for k := range 120 {
		st.Lines = append(st.Lines, domain.Line{Description: "Hora " + strconv.Itoa(k+1), Quantity: vocab.MustDecimal("1"), UnitPrice: vocab.MustDecimal("10"),
			Net: vocab.MustDecimal("10")})
	}
	st.Taxes = domain.Breakdown{Net: vocab.MustDecimal("1200"), Tax: vocab.MustDecimal("252"),
		Lines: []domain.TaxLine{{TaxType: "vat", TaxCode: "G21", TreatmentKind: "subject", Rate: vocab.MustDecimal("21"), Base: vocab.MustDecimal("1200"), Amount: vocab.MustDecimal("252")}}}
	b := renderInvoice(st, nil)
	pages := bytes.Count(b, []byte("/Type /Page /Parent"))
	if pages < 2 {
		t.Fatalf("pages: %d", pages)
	}
	last := "Página " + strconv.Itoa(pages) + " de " + strconv.Itoa(pages)
	for _, want := range []string{last, "Hora 120", "1.452,00 €", `\(continúa\)`} {
		if !bytes.Contains(b, pdf.Encode(want)) {
			t.Errorf("missing %q", want)
		}
	}
	if n := bytes.Count(b, pdf.Encode(txtConcept)); n != pages {
		t.Errorf("line headers: %d in %d pages", n, pages)
	}
}

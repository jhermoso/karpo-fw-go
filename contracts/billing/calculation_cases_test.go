// Package billing_test pasa por el cálculo real de Karpo Go los casos compartidos con Karpo C#
// (calculation-cases.json, docs/FACTURACION.md, «Casos compartidos con Karpo C#»): las líneas por
// el dominio de Billing, el desglose por el TaxEngine de Fiscal con la jurisdicción española a
// través del adaptador de Billing, y los plazos por las condiciones de pago de Receivables. Con
// lineRounding = once, el neto de cada línea también por el importe de la línea de Orders (D-149).
package billing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/jurisdictions/es"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	rdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// casesFile es el fichero de casos, y format la versión que esta prueba sabe leer.
const (
	casesFile = "calculation-cases.json"
	format    = 1
)

type file struct {
	Format  int        `json:"format"`
	Title   string     `json:"title"`
	Rules   []string   `json:"rules"`
	Amounts string     `json:"amounts"`
	Codes   string     `json:"codes"`
	Order   string     `json:"order"`
	Catalog catalog    `json:"catalog"`
	Pending []string   `json:"pending"`
	Cases   []testCase `json:"cases"`
}

type catalog struct {
	Country   string `json:"country"`
	Territory string `json:"territory"`
	ValidFrom string `json:"validFrom"`
	Rates     []struct {
		Code      string `json:"code"`
		Tax       string `json:"tax"`
		Rate      string `json:"rate"`
		Surcharge string `json:"surcharge"`
	} `json:"rates"`
	Treatments []struct {
		Code string `json:"code"`
		Kind string `json:"kind"`
	} `json:"treatments"`
}

type testCase struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Input       input    `json:"input"`
	Expected    expected `json:"expected"`
}

type input struct {
	InvoiceKind string `json:"invoiceKind"`
	Date        string `json:"date"`
	Settings    struct {
		LineRounding  string `json:"lineRounding"`
		PriceDecimals int    `json:"priceDecimals"`
	} `json:"settings"`
	EquivalenceSurcharge bool `json:"equivalenceSurcharge"`
	Lines                []struct {
		Quantity  string `json:"quantity"`
		UnitPrice string `json:"unitPrice"`
		Discount  string `json:"discount"`
		// CustomerDiscount es el descuento del cliente, que se encadena con Discount (opcional).
		CustomerDiscount string `json:"customerDiscount"`
		TaxCode          string `json:"taxCode"`
		Treatment        string `json:"treatment"`
	} `json:"lines"`
	Installments *struct {
		Count       int `json:"count"`
		DaysToFirst int `json:"daysToFirst"`
		DaysBetween int `json:"daysBetween"`
	} `json:"installments"`
}

type group struct {
	TaxCode       string `json:"taxCode"`
	Treatment     string `json:"treatment"`
	Kind          string `json:"kind"`
	Base          string `json:"base"`
	Rate          string `json:"rate"`
	Amount        string `json:"amount"`
	SurchargeRate string `json:"surchargeRate"`
	Surcharge     string `json:"surcharge"`
}

type expected struct {
	Rejection *struct {
		Line  int    `json:"line"`
		Field string `json:"field"`
	} `json:"rejection"`
	LineNets     []string `json:"lineNets"`
	Breakdown    []group  `json:"breakdown"`
	Net          string   `json:"net"`
	Tax          string   `json:"tax"`
	Surcharge    string   `json:"surcharge"`
	Total        string   `json:"total"`
	Installments []struct {
		Date   string `json:"date"`
		Amount string `json:"amount"`
	} `json:"installments"`
}

func load(t *testing.T) file {
	t.Helper()
	raw, err := os.ReadFile(casesFile)
	if err != nil {
		t.Fatal(err)
	}
	// Los campos se leen con su tipo (los importes, como cadenas) y no se admite ninguno que la
	// prueba no conozca: un campo nuevo es un cambio del formato.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f file
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("%s: %v", casesFile, err)
	}
	if f.Format != format {
		t.Fatalf("format %d; this test reads %d", f.Format, format)
	}
	if len(f.Cases) == 0 {
		t.Fatal("no cases")
	}
	seen := map[string]bool{}
	for _, c := range f.Cases {
		if c.ID == "" || seen[c.ID] {
			t.Fatalf("empty or repeated id: %q", c.ID)
		}
		seen[c.ID] = true
	}
	return f
}

// taxEngine monta el TaxEngine de Fiscal con la jurisdicción española, el catálogo del fichero y
// el perfil fiscal del vendedor, sobre repositorios en memoria.
func taxEngine(t *testing.T, cat catalog, seller fw.UUID) binfra.FiscalTaxes {
	t.Helper()
	ctx := context.Background()
	store := memory.NewStore("calculation-cases")
	rates := memory.NewRepository[fdomain.TaxRateID, *fdomain.TaxRate](store)
	treatments := memory.NewRepository[fdomain.TreatmentID, *fdomain.Treatment](store)
	taxpayers := memory.NewRepository[fdomain.TaxpayerID, *fdomain.Taxpayer](store)
	territory, ok := fdomain.ParseTerritory(cat.Territory)
	if !ok {
		t.Fatalf("territory %q", cat.Territory)
	}
	from, err := vocab.ParseDate(cat.ValidFrom)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range cat.Rates {
		typ, ok := fdomain.ParseTaxType(r.Tax)
		if !ok {
			t.Fatalf("tax %q", r.Tax)
		}
		rate, err := fdomain.ReconstituteTaxRate(fdomain.NewTaxRateID(), fdomain.TaxRateState{Type: typ, Territory: territory, Code: r.Code,
			Description: r.Code, Rate: amount(t, "rate", r.Rate), Surcharge: amount(t, "surcharge", r.Surcharge), From: from})
		if err != nil {
			t.Fatal(err)
		}
		if err := rates.Save(ctx, rate); err != nil {
			t.Fatal(err)
		}
	}
	for _, x := range cat.Treatments {
		kind, ok := fdomain.ParseTreatmentKind(x.Kind)
		if !ok {
			t.Fatalf("kind %q", x.Kind)
		}
		tr, err := fdomain.ReconstituteTreatment(fdomain.NewTreatmentID(), fdomain.TreatmentState{Territory: territory, Code: x.Code,
			Description: x.Code, Kind: kind, Active: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := treatments.Save(ctx, tr); err != nil {
			t.Fatal(err)
		}
	}
	tp, err := fdomain.RegisterTaxpayer(fdomain.NewTaxpayerID(), fdomain.OrganizationID{UUID: seller},
		fdomain.TaxpayerTerms{Territory: territory, FiscalYearStartMonth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := taxpayers.Save(ctx, tp); err != nil {
		t.Fatal(err)
	}
	return binfra.FiscalTaxes{Engine: fapp.NewEngine(taxpayers, rates, treatments, es.Spain{})}
}

// amount lee un importe obligatorio: si falta, la prueba falla (no cuenta como cero).
func amount(t *testing.T, name, s string) vocab.Decimal {
	t.Helper()
	if s == "" {
		t.Fatalf("%s: missing amount", name)
	}
	d, err := vocab.ParseDecimal(s)
	if err != nil {
		t.Fatalf("%s %q: %v", name, s, err)
	}
	return d
}

func TestSharedCalculationCases(t *testing.T) {
	f := load(t)
	seller := fw.NewUUID()
	taxes := taxEngine(t, f.Catalog, seller)
	for _, c := range f.Cases {
		t.Run(c.ID, func(t *testing.T) { run(t, taxes, seller, c) })
	}
}

func run(t *testing.T, taxes binfra.FiscalTaxes, seller fw.UUID, c testCase) {
	ctx := context.Background()
	in, want := c.Input, c.Expected
	st := domain.InvoiceState{Seller: domain.OrganizationID{UUID: seller}, Customer: domain.PartyID{UUID: fw.NewUUID()},
		EquivalenceSurcharge: in.EquivalenceSurcharge,
		Calculation:          domain.Calculation{Rounding: domain.LineRounding(in.Settings.LineRounding), PriceDecimals: in.Settings.PriceDecimals}}
	switch in.InvoiceKind {
	case "ordinary":
		st.Kind = domain.Ordinary
	case "corrective":
		st.Kind, st.Corrects, st.Reason = domain.Corrective, domain.NewInvoiceID(), "R1"
	default:
		t.Fatalf("invoiceKind %q", in.InvoiceKind)
	}
	inv, err := domain.DraftInvoice(domain.NewInvoiceID(), st)
	if err != nil {
		t.Fatal(err)
	}
	discounts := make([]vocab.Decimal, len(in.Lines))
	for k, l := range in.Lines {
		// El descuento del cliente se encadena con el de la línea y se redondea a 4 decimales, como
		// hacen Orders y el precio automático de Billing (D-148, D-149).
		discounts[k] = amount(t, "discount", l.Discount)
		if l.CustomerDiscount != "" {
			discounts[k] = odomain.EffectiveDiscount(discounts[k], amount(t, "customerDiscount", l.CustomerDiscount))
		}
		_, err := inv.AddLine(domain.LineInput{Description: "Línea", Quantity: amount(t, "quantity", l.Quantity),
			UnitPrice: amount(t, "unitPrice", l.UnitPrice), Discount: discounts[k], TaxCode: l.TaxCode, Treatment: l.Treatment})
		if r := want.Rejection; r != nil && r.Line == k+1 {
			var ve *fw.ValidationError
			if !errors.As(err, &ve) || !slices.ContainsFunc(ve.Errors, func(e fw.FieldError) bool { return e.Field == r.Field }) {
				t.Fatalf("line %d: want an error of field %s, got %v", k+1, r.Field, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("line %d: %v", k+1, err)
		}
	}
	if want.Rejection != nil {
		t.Fatalf("want line %d rejected", want.Rejection.Line)
	}

	lines := inv.State().Lines
	if len(lines) != len(want.LineNets) {
		t.Fatalf("%d line nets, want %d", len(lines), len(want.LineNets))
	}
	for k, l := range lines {
		w := amount(t, "lineNets", want.LineNets[k])
		if !l.Net.Equal(w) {
			t.Errorf("net of line %d: %s, want %s", k+1, l.Net, w)
		}
		// El importe de una línea de pedido o de albarán es el neto de la factura con once (D-149).
		if in.Settings.LineRounding == string(domain.RoundOnce) {
			if got := odomain.LineAmount(l.Quantity, l.UnitPrice, discounts[k]); !got.Equal(w) {
				t.Errorf("amount of line %d in Orders: %s, want %s", k+1, got, w)
			}
		}
	}

	date, err := vocab.ParseDate(in.Date)
	if err != nil {
		t.Fatal(err)
	}
	b, err := taxes.Calculate(ctx, domain.OrganizationID{UUID: seller}, date, in.EquivalenceSurcharge, inv.TaxableLines())
	if err != nil {
		t.Fatal(err)
	}
	if !b.Net.Equal(inv.Net()) {
		t.Fatalf("the breakdown (%s) does not cover the net of the lines (%s)", b.Net, inv.Net())
	}
	type key struct{ code, treatment string }
	got := map[key]domain.TaxLine{}
	for _, l := range b.Lines {
		got[key{l.TaxCode, l.Treatment}] = l
	}
	if len(got) != len(want.Breakdown) {
		t.Errorf("%d breakdown groups, want %d: %+v", len(got), len(want.Breakdown), b.Lines)
	}
	for _, g := range want.Breakdown {
		name := g.TaxCode + g.Treatment
		l, ok := got[key{g.TaxCode, g.Treatment}]
		if !ok {
			t.Errorf("missing group %s", name)
			continue
		}
		for field, pair := range map[string][2]vocab.Decimal{"base": {l.Base, amount(t, "base", g.Base)}, "rate": {l.Rate, amount(t, "rate", g.Rate)},
			"amount": {l.Amount, amount(t, "amount", g.Amount)}, "surchargeRate": {l.SurchargeRate, amount(t, "surchargeRate", g.SurchargeRate)},
			"surcharge": {l.SurchargeAmount, amount(t, "surcharge", g.Surcharge)}} {
			if !pair[0].Equal(pair[1]) {
				t.Errorf("%s of %s: %s, want %s", field, name, pair[0], pair[1])
			}
		}
		if l.TreatmentKind != g.Kind {
			t.Errorf("kind of %s: %s, want %s", name, l.TreatmentKind, g.Kind)
		}
	}
	for field, pair := range map[string][2]vocab.Decimal{"net": {b.Net, amount(t, "net", want.Net)}, "tax": {b.Tax, amount(t, "tax", want.Tax)},
		"surcharge": {b.Surcharge, amount(t, "surcharge", want.Surcharge)}, "total": {b.Total(), amount(t, "total", want.Total)}} {
		if !pair[0].Equal(pair[1]) {
			t.Errorf("%s: %s, want %s", field, pair[0], pair[1])
		}
	}

	if in.Installments == nil {
		if len(want.Installments) > 0 {
			t.Fatal("expected installments without installments in the input")
		}
		return
	}
	terms, err := rdomain.ReconstituteTerms(rdomain.NewTermsID(), rdomain.TermsState{Seller: rdomain.OrganizationID{UUID: seller}, Code: "CASE",
		Description: c.ID, Installments: in.Installments.Count, DaysToFirst: in.Installments.DaysToFirst, DaysBetween: in.Installments.DaysBetween, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	dues, err := terms.Schedule(ctx, date, b.Total(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(dues) != len(want.Installments) {
		t.Fatalf("%d installments, want %d", len(dues), len(want.Installments))
	}
	for k, d := range dues {
		w := want.Installments[k]
		if d.Date.String() != w.Date || !d.Amount.Equal(amount(t, "installments.amount", w.Amount)) {
			t.Errorf("installment %d: %s %s, want %s %s", k+1, d.Date, d.Amount, w.Date, w.Amount)
		}
	}
}

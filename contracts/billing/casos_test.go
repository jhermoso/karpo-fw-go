// Package billing_test pasa por el cálculo real de Karpo Go los casos compartidos con Karpo C#
// (casos-calculo.json, docs/FACTURACION.md, «Casos compartidos con Karpo C#»): las líneas por el
// dominio de Billing, el desglose por el TaxEngine de Fiscal con la jurisdicción española a través
// del adaptador de Billing, y los plazos por las condiciones de pago de Receivables.
package billing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/jurisdictions/es"
	rdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
)

// formato es la versión del fichero que esta prueba sabe leer.
const formato = 1

type fichero struct {
	Formato    int      `json:"formato"`
	Titulo     string   `json:"titulo"`
	Reglas     []string `json:"reglas"`
	Importes   string   `json:"importes"`
	Codigos    string   `json:"codigos"`
	Orden      string   `json:"orden"`
	Catalogo   catalogo `json:"catalogo"`
	Pendientes []string `json:"pendientes"`
	Casos      []caso   `json:"casos"`
}

type catalogo struct {
	Pais         string `json:"pais"`
	Territorio   string `json:"territorio"`
	VigenteDesde string `json:"vigente_desde"`
	Tipos        []struct {
		Codigo   string `json:"codigo"`
		Impuesto string `json:"impuesto"`
		Tipo     string `json:"tipo"`
		Recargo  string `json:"recargo"`
	} `json:"tipos"`
	Tratamientos []struct {
		Codigo string `json:"codigo"`
		Clase  string `json:"clase"`
	} `json:"tratamientos"`
}

type caso struct {
	ID          string   `json:"id"`
	Descripcion string   `json:"descripcion"`
	Entrada     entrada  `json:"entrada"`
	Esperado    esperado `json:"esperado"`
}

type entrada struct {
	ClaseFactura string `json:"clase_factura"`
	Fecha        string `json:"fecha"`
	Ajustes      struct {
		LineRounding  string `json:"line_rounding"`
		PriceDecimals int    `json:"price_decimals"`
	} `json:"ajustes"`
	RecargoEquivalencia bool `json:"recargo_equivalencia"`
	Lineas              []struct {
		Cantidad       string `json:"cantidad"`
		Precio         string `json:"precio"`
		Descuento      string `json:"descuento"`
		CodigoImpuesto string `json:"codigo_impuesto"`
		Tratamiento    string `json:"tratamiento"`
	} `json:"lineas"`
	Plazos *struct {
		Numero          int `json:"numero"`
		DiasPrimerPlazo int `json:"dias_primer_plazo"`
		DiasEntrePlazos int `json:"dias_entre_plazos"`
	} `json:"plazos"`
}

type grupo struct {
	CodigoImpuesto string `json:"codigo_impuesto"`
	Tratamiento    string `json:"tratamiento"`
	Clase          string `json:"clase"`
	Base           string `json:"base"`
	Tipo           string `json:"tipo"`
	Cuota          string `json:"cuota"`
	TipoRecargo    string `json:"tipo_recargo"`
	Recargo        string `json:"recargo"`
}

type esperado struct {
	Rechazo *struct {
		Linea int    `json:"linea"`
		Campo string `json:"campo"`
	} `json:"rechazo"`
	Netos    []string `json:"netos"`
	Desglose []grupo  `json:"desglose"`
	Base     string   `json:"base"`
	Cuota    string   `json:"cuota"`
	Recargo  string   `json:"recargo"`
	Total    string   `json:"total"`
	Plazos   []struct {
		Fecha   string `json:"fecha"`
		Importe string `json:"importe"`
	} `json:"plazos"`
}

func leer(t *testing.T) fichero {
	t.Helper()
	raw, err := os.ReadFile("casos-calculo.json")
	if err != nil {
		t.Fatal(err)
	}
	// Los campos se leen con su tipo (los importes, como cadenas) y no se admite ninguno que la
	// prueba no conozca: un campo nuevo es un cambio del formato.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f fichero
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("casos-calculo.json: %v", err)
	}
	if f.Formato != formato {
		t.Fatalf("formato %d; esta prueba lee el %d", f.Formato, formato)
	}
	vistos := map[string]bool{}
	for _, c := range f.Casos {
		if c.ID == "" || vistos[c.ID] {
			t.Fatalf("id vacío o repetido: %q", c.ID)
		}
		vistos[c.ID] = true
	}
	return f
}

// fiscal monta el TaxEngine de Fiscal con la jurisdicción española, el catálogo del fichero y el
// perfil fiscal del vendedor, sobre repositorios en memoria.
func fiscal(t *testing.T, cat catalogo, seller fw.UUID) binfra.FiscalTaxes {
	t.Helper()
	ctx := context.Background()
	store := memory.NewStore("casos")
	rates := memory.NewRepository[fdomain.TaxRateID, *fdomain.TaxRate](store)
	treatments := memory.NewRepository[fdomain.TreatmentID, *fdomain.Treatment](store)
	taxpayers := memory.NewRepository[fdomain.TaxpayerID, *fdomain.Taxpayer](store)
	territory, ok := fdomain.ParseTerritory(cat.Territorio)
	if !ok {
		t.Fatalf("territorio %q", cat.Territorio)
	}
	from, err := vocab.ParseDate(cat.VigenteDesde)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range cat.Tipos {
		typ, ok := fdomain.ParseTaxType(r.Impuesto)
		if !ok {
			t.Fatalf("impuesto %q", r.Impuesto)
		}
		rate, err := fdomain.ReconstituteTaxRate(fdomain.NewTaxRateID(), fdomain.TaxRateState{Type: typ, Territory: territory, Code: r.Codigo,
			Description: r.Codigo, Rate: num(t, r.Tipo), Surcharge: num(t, r.Recargo), From: from})
		if err != nil {
			t.Fatal(err)
		}
		if err := rates.Save(ctx, rate); err != nil {
			t.Fatal(err)
		}
	}
	for _, x := range cat.Tratamientos {
		kind, ok := fdomain.ParseTreatmentKind(x.Clase)
		if !ok {
			t.Fatalf("clase %q", x.Clase)
		}
		tr, err := fdomain.ReconstituteTreatment(fdomain.NewTreatmentID(), fdomain.TreatmentState{Territory: territory, Code: x.Codigo,
			Description: x.Codigo, Kind: kind, Active: true})
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

func num(t *testing.T, s string) vocab.Decimal {
	t.Helper()
	if s == "" {
		return vocab.DecimalFromInt(0)
	}
	d, err := vocab.ParseDecimal(s)
	if err != nil {
		t.Fatalf("importe %q: %v", s, err)
	}
	return d
}

func TestCasosCompartidosDelCalculo(t *testing.T) {
	f := leer(t)
	seller := fw.NewUUID()
	taxes := fiscal(t, f.Catalogo, seller)
	for _, c := range f.Casos {
		t.Run(c.ID, func(t *testing.T) { probar(t, taxes, seller, c) })
	}
}

func probar(t *testing.T, taxes binfra.FiscalTaxes, seller fw.UUID, c caso) {
	ctx := context.Background()
	in, want := c.Entrada, c.Esperado
	st := domain.InvoiceState{Seller: domain.OrganizationID{UUID: seller}, Customer: domain.PartyID{UUID: fw.NewUUID()},
		EquivalenceSurcharge: in.RecargoEquivalencia,
		Calculation:          domain.Calculation{Rounding: domain.LineRounding(in.Ajustes.LineRounding), PriceDecimals: in.Ajustes.PriceDecimals}}
	switch in.ClaseFactura {
	case "ordinary":
		st.Kind = domain.Ordinary
	case "corrective":
		st.Kind, st.Corrects, st.Reason = domain.Corrective, domain.NewInvoiceID(), "R1"
	default:
		t.Fatalf("clase_factura %q", in.ClaseFactura)
	}
	inv, err := domain.DraftInvoice(domain.NewInvoiceID(), st)
	if err != nil {
		t.Fatal(err)
	}
	for k, l := range in.Lineas {
		_, err := inv.AddLine(domain.LineInput{Description: "Línea", Quantity: num(t, l.Cantidad), UnitPrice: num(t, l.Precio),
			Discount: num(t, l.Descuento), TaxCode: l.CodigoImpuesto, Treatment: l.Tratamiento})
		if r := want.Rechazo; r != nil && r.Linea == k+1 {
			var ve *fw.ValidationError
			if !errors.As(err, &ve) || len(ve.Errors) == 0 || ve.Errors[0].Field != r.Campo {
				t.Fatalf("línea %d: se esperaba un error del campo %s y dio %v", k+1, r.Campo, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("línea %d: %v", k+1, err)
		}
	}
	if want.Rechazo != nil {
		t.Fatalf("se esperaba el rechazo de la línea %d", want.Rechazo.Linea)
	}

	lines := inv.State().Lines
	if len(lines) != len(want.Netos) {
		t.Fatalf("%d netos, se esperaban %d", len(lines), len(want.Netos))
	}
	for k, l := range lines {
		if !l.Net.Equal(num(t, want.Netos[k])) {
			t.Errorf("neto de la línea %d: %s, se esperaba %s", k+1, l.Net, want.Netos[k])
		}
	}

	date, err := vocab.ParseDate(in.Fecha)
	if err != nil {
		t.Fatal(err)
	}
	b, err := taxes.Calculate(ctx, domain.OrganizationID{UUID: seller}, date, in.RecargoEquivalencia, inv.TaxableLines())
	if err != nil {
		t.Fatal(err)
	}
	if !b.Net.Equal(inv.Net()) {
		t.Fatalf("el desglose (%s) no cubre el neto de las líneas (%s)", b.Net, inv.Net())
	}
	type key struct{ code, treatment string }
	got := map[key]domain.TaxLine{}
	for _, l := range b.Lines {
		got[key{l.TaxCode, l.Treatment}] = l
	}
	if len(got) != len(want.Desglose) {
		t.Errorf("%d grupos en el desglose, se esperaban %d: %+v", len(got), len(want.Desglose), b.Lines)
	}
	for _, g := range want.Desglose {
		l, ok := got[key{g.CodigoImpuesto, g.Tratamiento}]
		if !ok {
			t.Errorf("falta el grupo %s%s", g.CodigoImpuesto, g.Tratamiento)
			continue
		}
		for name, pair := range map[string][2]vocab.Decimal{"base": {l.Base, num(t, g.Base)}, "tipo": {l.Rate, num(t, g.Tipo)},
			"cuota": {l.Amount, num(t, g.Cuota)}, "tipo_recargo": {l.SurchargeRate, num(t, g.TipoRecargo)},
			"recargo": {l.SurchargeAmount, num(t, g.Recargo)}} {
			if !pair[0].Equal(pair[1]) {
				t.Errorf("%s de %s%s: %s, se esperaba %s", name, g.CodigoImpuesto, g.Tratamiento, pair[0], pair[1])
			}
		}
		if l.TreatmentKind != g.Clase {
			t.Errorf("clase de %s%s: %s, se esperaba %s", g.CodigoImpuesto, g.Tratamiento, l.TreatmentKind, g.Clase)
		}
	}
	for name, pair := range map[string][2]vocab.Decimal{"base": {b.Net, num(t, want.Base)}, "cuota": {b.Tax, num(t, want.Cuota)},
		"recargo": {b.Surcharge, num(t, want.Recargo)}, "total": {b.Total(), num(t, want.Total)}} {
		if !pair[0].Equal(pair[1]) {
			t.Errorf("%s: %s, se esperaba %s", name, pair[0], pair[1])
		}
	}

	if in.Plazos == nil {
		if len(want.Plazos) > 0 {
			t.Fatal("plazos esperados sin plazos en la entrada")
		}
		return
	}
	terms, err := rdomain.ReconstituteTerms(rdomain.NewTermsID(), rdomain.TermsState{Seller: rdomain.OrganizationID{UUID: seller}, Code: "CASO",
		Description: c.ID, Installments: in.Plazos.Numero, DaysToFirst: in.Plazos.DiasPrimerPlazo, DaysBetween: in.Plazos.DiasEntrePlazos, Active: true})
	if err != nil {
		t.Fatal(err)
	}
	dues, err := terms.Schedule(ctx, date, b.Total(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(dues) != len(want.Plazos) {
		t.Fatalf("%d plazos, se esperaban %d", len(dues), len(want.Plazos))
	}
	for k, d := range dues {
		if d.Date.String() != want.Plazos[k].Fecha || !d.Amount.Equal(num(t, want.Plazos[k].Importe)) {
			t.Errorf("plazo %d: %s %s, se esperaba %s %s", k+1, d.Date, d.Amount, want.Plazos[k].Fecha, want.Plazos[k].Importe)
		}
	}
}

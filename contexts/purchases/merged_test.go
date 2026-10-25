package purchases_test

import (
	"context"
	"testing"

	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// Los listados de facturas recibidas y de perfiles de proveedor dejan fuera por defecto los de un
// proveedor fusionado en otra party, y los traen con includeMergedParties; la factura pedida por
// su identidad se sigue encontrando (docs/DECISIONES-COMUNES.md, D-100).
func TestListingsLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, purchases.WithMergedParties(merged))
	ctx := h.adminCtx
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme fusiones",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	h.grant("viewer", acme.ID)
	_, err = h.fiscal.Service.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "G21", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	h.ok(err)
	_, err = h.fiscal.Service.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	h.ok(err)
	ana, bea := fw.NewUUID().String(), fw.NewUUID().String()
	merged.ids = []string{bea}

	ids := map[string]string{}
	for _, s := range []string{ana, bea} {
		_, err := h.pur.Service.SetSupplier.Handle(ctx, uapp.SetSupplier{Company: acme.ID, Supplier: s, Category: "goods", PaymentDays: 30})
		h.ok(err)
		inv, err := h.pur.Service.RegisterInvoice.Handle(ctx, uapp.RegisterInvoice{Company: acme.ID, Supplier: s, SupplierNumber: "F-" + s[:8],
			Issued: vocab.MustDate(2026, 10, 1), Received: vocab.MustDate(2026, 10, 2), Lines: []uapp.LineInput{{Base: "100", TaxCode: "G21"}},
			Total: "121.00"})
		h.ok(err)
		ids[s] = inv.ID
	}

	suppliersOf := func(path string) []string {
		t.Helper()
		var page fw.Page[uapp.InvoiceDTO]
		h.must(h.do("GET", path, "viewer", nil, &page), 200, path)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Supplier)
		}
		return out
	}
	path := "/api/purchases/invoices?company=" + acme.ID
	if got := suppliersOf(path); len(got) != 1 || got[0] != ana {
		t.Fatalf("invoices: %v, want only %s", got, ana)
	}
	if got := suppliersOf(path + "&includeMergedParties=true"); len(got) != 2 {
		t.Fatalf("invoices with includeMergedParties: %v", got)
	}
	h.must(h.do("GET", "/api/purchases/invoices/"+ids[bea], "viewer", nil, nil), 200, "the invoice of a merged party, by id")

	var profiles []uapp.SupplierDTO
	h.must(h.do("GET", "/api/purchases/suppliers?company="+acme.ID, "viewer", nil, &profiles), 200, "suppliers")
	if len(profiles) != 1 || profiles[0].Supplier != ana {
		t.Fatalf("suppliers: %+v", profiles)
	}
	h.must(h.do("GET", "/api/purchases/suppliers?company="+acme.ID+"&includeMergedParties=true", "viewer", nil, &profiles), 200, "suppliers")
	if len(profiles) != 2 {
		t.Fatalf("suppliers with includeMergedParties: %+v", profiles)
	}
}

package billing_test

import (
	"context"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// El listado de facturas deja fuera por defecto las de un cliente fusionado en otra party, y las
// trae con includeMergedParties; la factura pedida por su identidad se sigue encontrando
// (docs/DECISIONES-COMUNES.md, D-100).
func TestInvoicesLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, billing.WithMergedParties(merged))
	ctx := h.adminCtx
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme fusiones",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	h.grant("clerk", acme.ID)
	ana, bea := fw.NewUUID().String(), fw.NewUUID().String()
	merged.ids = []string{bea}

	ids := map[string]string{}
	for _, c := range []string{ana, bea} {
		inv, err := h.billing.Service.DraftInvoice.Handle(ctx, bapp.DraftInvoice{Seller: acme.ID, Customer: c})
		h.ok(err)
		ids[c] = inv.ID
	}

	customers := func(path string) []string {
		t.Helper()
		var page fw.Page[bapp.InvoiceDTO]
		h.must(h.do("GET", path, "clerk", nil, &page), 200, path)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Customer)
		}
		return out
	}
	path := "/api/billing/invoices?seller=" + acme.ID
	if got := customers(path); len(got) != 1 || got[0] != ana {
		t.Fatalf("invoices: %v, want only %s", got, ana)
	}
	if got := customers(path + "&includeMergedParties=true"); len(got) != 2 {
		t.Fatalf("invoices with includeMergedParties: %v", got)
	}
	// Filtrar por el cliente fusionado trae sus facturas sin pedir includeMergedParties (D-108).
	if got := customers(path + "&customer=" + bea); len(got) != 1 || got[0] != bea {
		t.Fatalf("invoices filtered by the merged customer: %v", got)
	}
	h.must(h.do("GET", "/api/billing/invoices/"+ids[bea], "clerk", nil, nil), 200, "the invoice of a merged party, by id")
}

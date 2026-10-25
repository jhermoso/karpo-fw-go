package orders_test

import (
	"context"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// Los listados de presupuestos, pedidos y condiciones dejan fuera por defecto los de un cliente
// fusionado en otra party, y los traen con includeMergedParties; el registro pedido por su
// identidad se sigue encontrando (docs/DECISIONES-COMUNES.md, D-64 y D-100).
func TestListingsLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, orders.WithMergedParties(merged))
	ctx := h.adminCtx
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme fusiones",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	h.grant("seller", acme.ID)
	ana, bea := fw.NewUUID().String(), fw.NewUUID().String()
	merged.ids = []string{bea}

	day := vocab.MustDate(2026, 10, 20)
	ids := map[string]string{}
	for _, c := range []string{ana, bea} {
		o, err := h.ord.Service.DraftOrder.Handle(ctx, oapp.DraftOrder{Company: acme.ID, Customer: c, Date: day})
		h.ok(err)
		ids[c] = o.ID
		_, err = h.ord.Service.DraftQuote.Handle(ctx, oapp.DraftQuote{Company: acme.ID, Customer: c, Date: day})
		h.ok(err)
		_, err = h.ord.Service.SetTerms.Handle(ctx, oapp.SetTerms{Company: acme.ID, Customer: c, Discount: "5"})
		h.ok(err)
	}

	customers := func(path string) []string {
		t.Helper()
		var page fw.Page[struct {
			Customer string `json:"customer"`
		}]
		h.must(h.do("GET", path, "seller", nil, &page), 200, path)
		out := []string{}
		for _, it := range page.Items {
			out = append(out, it.Customer)
		}
		return out
	}
	path := "/api/orders/orders?company=" + acme.ID
	if got := customers(path); len(got) != 1 || got[0] != ana {
		t.Fatalf("orders: %v, want only %s", got, ana)
	}
	if got := customers(path + "&includeMergedParties=true"); len(got) != 2 {
		t.Fatalf("orders with includeMergedParties: %v", got)
	}
	h.must(h.do("GET", "/api/orders/orders/"+ids[bea], "seller", nil, nil), 200, "the order of a merged party, by id")
	// Filtrar por el cliente fusionado trae sus pedidos sin pedir includeMergedParties (D-108).
	if got := customers(path + "&customer=" + bea); len(got) != 1 || got[0] != bea {
		t.Fatalf("orders filtered by the merged customer: %v", got)
	}
	qs, err := h.ord.Service.SearchQuotes.Handle(ctx, oapp.SearchQuotes{Company: acme.ID, Customer: bea})
	h.ok(err)
	if len(qs.Items) != 1 || qs.Items[0].Customer != bea {
		t.Fatalf("quotes filtered by the merged customer: %+v", qs.Items)
	}
	for include, want := range map[bool]int{false: 1, true: 2} {
		qs, err := h.ord.Service.SearchQuotes.Handle(ctx, oapp.SearchQuotes{Company: acme.ID, IncludeMergedParties: include})
		h.ok(err)
		if len(qs.Items) != want || qs.Items[0].Customer != ana && !include {
			t.Fatalf("quotes (includeMergedParties %v): %+v", include, qs.Items)
		}
	}

	var terms []oapp.TermsDTO
	h.must(h.do("GET", "/api/orders/terms?company="+acme.ID, "seller", nil, &terms), 200, "terms")
	if len(terms) != 1 || terms[0].Customer != ana {
		t.Fatalf("terms: %+v", terms)
	}
	h.must(h.do("GET", "/api/orders/terms?company="+acme.ID+"&includeMergedParties=true", "seller", nil, &terms), 200, "terms")
	if len(terms) != 2 {
		t.Fatalf("terms with includeMergedParties: %+v", terms)
	}
}

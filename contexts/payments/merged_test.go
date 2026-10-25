package payments_test

import (
	"context"
	"testing"

	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/payments"
	yapp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// Los listados de obligaciones y de pagos dejan fuera por defecto los de un beneficiario fusionado
// en otra party, y los traen con includeMergedParties; lo que se paga a la administración
// tributaria, sin beneficiario, no se deja fuera; el registro pedido por su identidad se sigue
// encontrando (docs/DECISIONES-COMUNES.md, D-100).
func TestListingsLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, payments.WithMergedParties(merged))
	ctx := h.adminCtx
	acme, err := h.parties.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme fusiones",
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	h.ok(err)
	h.grant("clerk", acme.ID)
	ana, bea := fw.NewUUID().String(), fw.NewUUID().String()
	merged.ids = []string{bea}

	// Una factura recibida de cada proveedor y el Modelo 111, que se debe a la AEAT.
	for _, s := range []string{ana, bea} {
		h.send(fw.NewUUID().String(), "purchases.invoice-registered.v1", map[string]any{"invoiceId": fw.NewUUID().String(), "company": acme.ID,
			"supplier": s, "supplierNumber": "F-" + s[:8], "issued": "2026-09-15", "due": "2026-10-15", "payable": "121.00"})
	}
	h.send(fw.NewUUID().String(), "fiscal.filing-submitted.v1", map[string]any{"filingId": fw.NewUUID().String(), "declarant": acme.ID, "form": "111",
		"year": 2026, "period": "3T", "withheld": "216.00"})

	payees := func(path string) map[string]string {
		t.Helper()
		var page fw.Page[yapp.PayableDTO]
		h.must(h.do("GET", path, "clerk", nil, &page), 200, path)
		out := map[string]string{}
		for _, it := range page.Items {
			out[it.Payee] = it.ID
		}
		return out
	}
	path := "/api/payments/payables?company=" + acme.ID
	if got := payees(path); len(got) != 2 || got[ana] == "" || got["AEAT"] == "" {
		t.Fatalf("payables: %v, want %s and AEAT", got, ana)
	}
	all := payees(path + "&includeMergedParties=true")
	if len(all) != 3 || all[bea] == "" {
		t.Fatalf("payables with includeMergedParties: %v", all)
	}
	h.must(h.do("GET", "/api/payments/payables/"+all[bea], "clerk", nil, nil), 200, "the payable of a merged party, by id")

	// Un pago a cada proveedor y otro a la AEAT.
	paid := map[string]string{}
	for _, body := range []map[string]any{{"payee": ana}, {"payee": bea}, {"authority": "AEAT"}} {
		body["company"], body["date"], body["amount"], body["method"] = acme.ID, "2026-10-05", "10.00", "cash"
		var p yapp.PaymentDTO
		h.must(h.do("POST", "/api/payments/payments", "clerk", body, &p), 201, "payment")
		paid[p.Payee] = p.ID
	}
	count := func(path string) int {
		t.Helper()
		var page fw.Page[yapp.PaymentDTO]
		h.must(h.do("GET", path, "clerk", nil, &page), 200, path)
		for _, it := range page.Items {
			if it.Payee == bea && path == "/api/payments/payments?company="+acme.ID {
				t.Fatalf("payments: the merged payee is listed: %+v", page.Items)
			}
		}
		return len(page.Items)
	}
	if got := count("/api/payments/payments?company=" + acme.ID); got != 2 {
		t.Fatalf("payments: %d, want the one to %s and the one to AEAT", got, ana)
	}
	if got := count("/api/payments/payments?company=" + acme.ID + "&includeMergedParties=true"); got != 3 {
		t.Fatalf("payments with includeMergedParties: %d", got)
	}
	h.must(h.do("GET", "/api/payments/payments/"+paid[bea], "clerk", nil, nil), 200, "the payment of a merged party, by id")
}

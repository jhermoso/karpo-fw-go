package documents_test

import (
	"context"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/documents"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/documents/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// mergedParties hace de Parties: dice qué parties están fusionadas en otra.
type mergedParties struct{ ids []string }

func (m *mergedParties) MergedParties(context.Context) ([]string, error) { return m.ids, nil }

// El registro de documentos deja fuera por defecto los de una party fusionada en otra y los trae
// con includeMergedParties; un documento sin party no se deja fuera, y el documento pedido por su
// identidad se sigue encontrando (docs/DECISIONES-COMUNES.md, D-64 y D-100).
func TestDocumentsLeaveOutMergedParties(t *testing.T) {
	merged := &mergedParties{}
	h := compose(t, documents.WithMergedParties(merged))
	id := func() string { return fw.NewUUID().String() }
	acme, ana, bea := id(), id(), id()
	h.grant("viewer", acme)
	merged.ids = []string{bea}
	at := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	ok := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	ok(h.send("merged-1", "orders.order-confirmed.v1", at, map[string]any{"orderId": id(), "company": acme, "customer": ana,
		"number": "PED-2026-000001", "total": "10.00"}))
	ok(h.send("merged-2", "orders.order-confirmed.v1", at, map[string]any{"orderId": id(), "company": acme, "customer": bea,
		"number": "PED-2026-000002", "total": "20.00"}))
	// Una declaración no tiene party.
	ok(h.send("merged-3", "fiscal.filing-submitted.v1", at, map[string]any{"filingId": id(), "declarant": acme, "form": "111", "year": 2026,
		"period": "2T", "withheld": "300.00"}))

	listed := func(path string) map[string]dapp.DocumentDTO {
		t.Helper()
		var page fw.Page[dapp.DocumentDTO]
		h.must(h.get(path, "viewer", &page), 200, path)
		out := map[string]dapp.DocumentDTO{}
		for _, d := range page.Items {
			out[d.Type+" "+d.Party] = d
		}
		return out
	}
	path := "/api/documents?company=" + acme
	got := listed(path)
	if _, filing := got["tax-filing "]; len(got) != 2 || !filing || got["order "+ana].ID == "" {
		t.Fatalf("documents: %v, want Ana's order and the filing", got)
	}
	all := listed(path + "&includeMergedParties=true")
	if len(all) != 3 || all["order "+bea].ID == "" {
		t.Fatalf("documents with includeMergedParties: %v", all)
	}
	h.must(h.get("/api/documents/"+all["order "+bea].ID, "viewer", nil), 200, "the document of a merged party, by id")
}

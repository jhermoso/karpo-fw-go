package orders_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	oinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// problem envía una petición que debe fallar y devuelve su estado y el código del cuerpo.
func (h *quoteHost) problem(method, path, user string, body any) [2]any {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, &buf)
	req.Header.Set("Authorization", h.tokens[user])
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer res.Body.Close()
	var p struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(res.Body).Decode(&p)
	return [2]any{res.StatusCode, p.Code}
}

func (h *quoteHost) mustFail(got [2]any, status int, code, what string) {
	h.t.Helper()
	if got[0] != status || got[1] != code {
		h.t.Fatalf("%s: %v %v, want %d %q", what, got[0], got[1], status, code)
	}
}

// kindScenario: el tipo de presupuesto (D-155) es quote por omisión, puede ser proforma, se filtra
// en la búsqueda y no cambia una vez enviado.
func (h *quoteHost) kindScenario() {
	t := h.t
	acme, customer := fw.NewUUID().String(), fw.NewUUID().String()
	h.grant("seller", acme)
	h.grant("manager", acme)
	company := odomain.OrganizationID{UUID: fw.MustParseUUID(acme)}
	widget := odomain.ProductID{UUID: fw.NewUUID()}
	ten, _ := vocab.ParseDecimal("10")
	h.book.set(widget, odomain.Item{Company: company, SKU: "W-1", Name: "Tornillo", UoM: "ud", TaxCode: "G", Sellable: true, UnitPrice: ten})

	var q, p oapp.QuoteDTO
	h.must(h.do("POST", "/api/orders/quotes", "seller", map[string]any{"company": acme, "customer": customer}, &q), 201, "default kind")
	if q.Kind != "quote" {
		t.Fatalf("default kind: %q", q.Kind)
	}
	h.mustFail(h.problem("POST", "/api/orders/quotes", "seller", map[string]any{"company": acme, "customer": customer, "kind": "pedido"}), 422, "orders.invalid_quote_kind", "invalid kind")
	h.must(h.do("POST", "/api/orders/quotes", "seller", map[string]any{"company": acme, "customer": customer, "kind": "proforma"}, &p), 201, "proforma")
	if p.Kind != "proforma" {
		t.Fatalf("proforma: %q", p.Kind)
	}

	// Un borrador cambia de tipo; lo que no dice el tipo lo deja como está.
	h.mustFail(h.problem("PUT", "/api/orders/quotes/"+q.ID, "seller", map[string]any{"kind": "albaran"}), 422, "orders.invalid_quote_kind", "invalid kind on change")
	h.must(h.do("PUT", "/api/orders/quotes/"+q.ID, "seller", map[string]any{"kind": "proforma"}, &q), 200, "to proforma")
	h.must(h.do("PUT", "/api/orders/quotes/"+q.ID, "seller", map[string]any{"reference": "R"}, &q), 200, "kind kept")
	if q.Kind != "proforma" {
		t.Fatalf("changed kind: %q", q.Kind)
	}
	h.must(h.do("PUT", "/api/orders/quotes/"+q.ID, "seller", map[string]any{"kind": "quote"}, &q), 200, "back to quote")

	// Enviado, se numera como siempre y el tipo ya no cambia.
	h.must(h.do("POST", "/api/orders/quotes/"+p.ID+"/lines", "seller", map[string]any{"product": widget.String(), "quantity": "1"}, nil), 200, "line")
	h.must(h.do("POST", "/api/orders/quotes/"+p.ID+"/send", "manager", nil, &p), 200, "send")
	if p.Number == "" || p.Kind != "proforma" {
		t.Fatalf("sent proforma: %+v", p)
	}
	h.mustFail(h.problem("PUT", "/api/orders/quotes/"+p.ID, "seller", map[string]any{"kind": "quote"}), 422, "orders.quote_kind_locked", "kind locked")

	// La búsqueda filtra por tipo.
	var page struct {
		Items []oapp.QuoteDTO `json:"items"`
	}
	h.must(h.do("GET", "/api/orders/quotes?company="+acme+"&kind=proforma", "seller", nil, &page), 200, "search proformas")
	if len(page.Items) != 1 || page.Items[0].ID != p.ID {
		t.Fatalf("proformas: %+v", page.Items)
	}
	h.mustFail(h.problem("GET", "/api/orders/quotes?company="+acme+"&kind=otro", "seller", nil), 422, "orders.invalid_quote_kind", "search invalid kind")
}

func TestQuotes_Kind_MemoryThenSQLite(t *testing.T) {
	h := composeQuotes(t)
	h.kindScenario()

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{oinfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	h.kindScenario()
}

package billing_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// El albarán llega con el precio y el descuento encadenado de cada línea (D-149): el borrador, el
// pendiente de un cliente mensual y el borrador del cierre dan lo mismo que el albarán al céntimo
// (1000 × 1 € con un 7,375 % = 926,25, no los 926,30 del precio neto 0,9263). Un albarán de antes,
// con sólo el precio neto, se sigue facturando a ese precio sin descuento. Sobre SQLite migrada,
// para que el pendiente pase por su tabla.
func TestBilling_DeliveryWithChainedDiscount(t *testing.T) {
	h := compose(t)
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "chained.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	h.ok(err)
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations()})
	h.ok(err)
	_, err = m.Migrate(context.Background())
	h.ok(err)
	h.ok(h.sw.Swap(context.Background(), db))
	acme, ana := h.seller(t)
	bs, actx := h.billing.Service, h.adminCtx

	send := func(number, periodicity string, line map[string]any) {
		t.Helper()
		line["sku"], line["description"], line["taxCode"], line["quantity"] = "T", "Tornillo", "G21", "1000"
		data, _ := json.Marshal(map[string]any{"deliveryId": fw.NewUUID().String(), "orderNumber": "P-" + number, "company": acme, "customer": ana,
			"number": number, "date": "2026-10-03", "billingPeriodicity": periodicity, "lines": []map[string]any{line}})
		h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
			Source: "orders", Data: data}))
	}
	send("A-1", "per_delivery", map[string]any{"unitPrice": "1.0000", "discount": "7.3750", "netPrice": "0.9263", "amount": "926.25"})
	send("A-0", "", map[string]any{"netPrice": "0.9263", "amount": "926.30"}) // de antes de D-149
	by := map[string]bapp.InvoiceDTO{}
	for _, d := range h.drafts(acme) {
		by[d.SourceRef] = d
	}
	if d := by["A-1"]; len(d.Lines) != 1 || d.Lines[0].UnitPrice != "1" || d.Lines[0].Discount != "7.375" || d.Lines[0].Net != "926.25" || d.Net != "926.25" {
		t.Fatalf("the draft of a delivery note with its chained discount: %+v", d)
	}
	if d := by["A-0"]; len(d.Lines) != 1 || d.Lines[0].UnitPrice != "0.9263" || d.Lines[0].Discount != "" || d.Net != "926.30" {
		t.Fatalf("the draft of a delivery note from before D-149: %+v", d)
	}

	send("M-1", "monthly", map[string]any{"unitPrice": "1.0000", "discount": "7.3750", "netPrice": "0.9263", "amount": "926.25"})
	pending, err := bs.SearchPendingDeliveries.Handle(actx, bapp.SearchPendingDeliveries{Seller: acme, Customer: ana})
	if err != nil || len(pending.Items) != 1 || pending.Items[0].Net != "926.25" {
		t.Fatalf("the pending delivery note: %+v %v", pending, err)
	}
	res, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, Customer: ana, UpTo: vocab.MustDate(2026, 10, 31)})
	if err != nil || len(res.Drafts) != 1 || res.Drafts[0].Net != "926.25" || res.Drafts[0].Lines[0].Discount != "7.375" {
		t.Fatalf("the draft of the period: %+v %v", res, err)
	}
}

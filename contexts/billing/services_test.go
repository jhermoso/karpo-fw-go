package billing_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// services entrega a Billing una emisión de servicios de un pedido, como la publica Orders (D-155).
func (h *host) services(acme, customer, order, invoicing, quantity string) {
	h.t.Helper()
	data, _ := json.Marshal(map[string]any{"invoicingId": invoicing, "orderId": order, "orderNumber": "PED-1", "company": acme, "customer": customer,
		"date": "2026-10-05", "lines": []map[string]any{{"line": 1, "product": fw.NewUUID().String(), "sku": "SOP", "description": "Soporte",
			"taxCode": "G21", "quantity": quantity, "unitPrice": "10.0000", "discount": "0.0000"}}})
	h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.services-invoiceable.v1",
		Source: "orders", Data: data}))
}

// D-155, en memoria y en SQLite: cada emisión de servicios de un pedido tiene su borrador ordinario,
// con origen orders.service-invoicing «pedido|emisión»; la misma emisión entregada otra vez no
// prepara otro; descartar ese borrador publica billing.draft-discarded.v1 para que Orders devuelva
// las cantidades a pendiente; descartar el de un albarán, no.
func TestBilling_ServicesInvoicedFromTheOrder(t *testing.T) {
	for _, useSQL := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "sqlite"}[useSQL], func(t *testing.T) {
			h := compose(t)
			ctx := context.Background()
			if useSQL {
				raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "services.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
				h.ok(err)
				raw.SetMaxOpenConns(1)
				t.Cleanup(func() { _ = raw.Close() })
				db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
				m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations()})
				h.ok(err)
				_, err = m.Migrate(ctx)
				h.ok(err)
				h.ok(h.sw.Swap(ctx, db))
			}
			acme, ana := h.seller(t)
			order, first, second := fw.NewUUID().String(), fw.NewUUID().String(), fw.NewUUID().String()

			h.services(acme, ana, order, first, "2")
			h.services(acme, ana, order, first, "2") // entregada otra vez
			drafts := h.drafts(acme)
			if len(drafts) != 1 || drafts[0].SourceType != bapp.ServiceInvoicingSource || drafts[0].SourceID != order+"|"+first ||
				drafts[0].SourceRef != "PED-1" || drafts[0].Net != "20.00" || drafts[0].OperationDate != "2026-10-05" ||
				drafts[0].Lines[0].Description != "SOP Soporte" || drafts[0].Description != "Servicios del pedido PED-1" {
				t.Fatalf("the draft of the services: %+v", drafts)
			}
			h.services(acme, ana, order, second, "1")
			two := h.drafts(acme)
			if len(two) != 2 {
				t.Fatalf("one draft per invoicing: %d", len(two))
			}
			// Unir borradores es cosa de albaranes (D-146): los de servicios no se unen.
			_, err := h.billing.Service.MergeDrafts.Handle(h.adminCtx, bapp.MergeDrafts{Invoices: []string{two[0].ID, two[1].ID}})
			if !violation(err, "billing.merge_unsourced") {
				t.Fatalf("merging services drafts: %v", err)
			}

			var mu sync.Mutex
			var discarded []map[string]any
			broker := inprocess.NewBroker()
			broker.Subscribe("orders", application.MessageHandlerFunc(func(_ context.Context, env application.Envelope) error {
				if env.Type == "billing.draft-discarded.v1" {
					var e map[string]any
					_ = json.Unmarshal(env.Data, &e)
					mu.Lock()
					discarded = append(discarded, e)
					mu.Unlock()
				}
				return nil
			}))
			relay := h.billing.Relay(broker)
			_, err = relay.RelayOnce(ctx)
			h.ok(err)

			// Un albarán por albarán: su descarte no sale de Facturación.
			h.orderDelivery(acme, ana, fw.NewUUID().String(), "A-1", "per_delivery", true)
			for _, d := range h.drafts(acme) {
				id, _ := bdomain.ParseInvoiceID(d.ID)
				if d.SourceType == bapp.DeliverySource || d.SourceID == order+"|"+first {
					_, err := h.billing.Service.Discard.Handle(h.adminCtx, bapp.DiscardInvoice{ID: id})
					h.ok(err)
				}
			}
			_, err = relay.RelayOnce(ctx)
			h.ok(err)
			mu.Lock()
			defer mu.Unlock()
			if len(discarded) != 1 {
				t.Fatalf("one discarded services draft: %+v", discarded)
			}
			src := discarded[0]["sources"].([]any)[0].(map[string]any)
			if discarded[0]["seller"] != acme || src["type"] != bapp.ServiceInvoicingSource || src["id"] != order+"|"+first || src["ref"] != "PED-1" {
				t.Fatalf("the discarded event: %+v", discarded[0])
			}
			// Queda el borrador de la otra emisión.
			if left := h.drafts(acme); len(left) != 1 || left[0].SourceID != order+"|"+second {
				t.Fatalf("drafts left: %+v", left)
			}
		})
	}
}

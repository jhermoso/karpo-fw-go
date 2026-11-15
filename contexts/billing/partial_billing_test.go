package billing_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// customerPolicies son las políticas de facturación parcial de cada cliente, como las daría
// Orders (D-153).
type customerPolicies struct {
	mu sync.Mutex
	by map[string]bdomain.PartialBillingPolicy
}

func (c *customerPolicies) Terms(_ context.Context, _ bdomain.OrganizationID, customer bdomain.PartyID) (bdomain.Terms, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bdomain.Terms{Discount: vocab.DecimalFromInt(0), PartialBilling: c.by[customer.String()]}, nil
}

// orderDelivery entrega a Billing el albarán number del pedido order, como lo publica Orders, con
// si deja el pedido servido entero; devuelve su Id.
func (h *host) orderDelivery(acme, customer, order, number, periodicity string, complete bool) string {
	h.t.Helper()
	return h.orderDeliveryID(fw.NewUUID().String(), acme, customer, order, number, periodicity, complete)
}

// orderDeliveryID es orderDelivery con el Id del albarán dado: el mismo albarán, entregado otra vez.
func (h *host) orderDeliveryID(id, acme, customer, order, number, periodicity string, complete bool) string {
	h.t.Helper()
	data, _ := json.Marshal(map[string]any{"deliveryId": id, "orderId": order, "orderNumber": "P-" + number, "company": acme, "customer": customer,
		"number": number, "date": "2026-10-05", "billingPeriodicity": periodicity, "orderComplete": complete,
		"lines": []map[string]any{{"sku": "T", "description": "Tornillo", "taxCode": "G21", "quantity": "1", "unitPrice": "10.0000"}}})
	h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
		Source: "orders", Data: data}))
	return id
}

// serving entrega a Billing el evento de pedido servido entero (true) o reabierto (false).
func (h *host) serving(acme, order string, complete bool) {
	h.t.Helper()
	typ := map[bool]string{true: "orders.sales-order-serving-completed.v1", false: "orders.sales-order-serving-reopened.v1"}[complete]
	data, _ := json.Marshal(map[string]any{"orderId": order, "company": acme})
	h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: typ, Source: "orders", Data: data}))
}

// D-153, en memoria y en SQLite: con require_complete_order, el albarán de un pedido que aún tiene
// algo pendiente de servir no tiene borrador ni entra en el cierre hasta que el pedido se sirve
// entero (su último albarán o el evento de pedido servido); la política del cliente gana a la de
// la empresa; pedirlo a mano o unir borradores de un pedido reabierto da 422
// orders.partial_billing_not_allowed, salvo que se salte con el permiso propio y un motivo, que
// queda en la auditoría de la factura.
func TestBilling_PartialBillingPolicy(t *testing.T) {
	for _, useSQL := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "sqlite"}[useSQL], func(t *testing.T) {
			conf := &settings{by: map[string]bapp.CompanySettings{}}
			terms := &customerPolicies{by: map[string]bdomain.PartialBillingPolicy{}}
			h := compose(t, billing.WithSettings(conf), billing.WithPricing(nil, terms))
			ctx := context.Background()
			if useSQL {
				raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "partial.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
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
			bs, actx := h.billing.Service, h.adminCtx
			bea, err := h.parties.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Bea", FirstSurname: "Parcial"})
			h.ok(err)
			bySource := func() map[string]bapp.InvoiceDTO {
				out := map[string]bapp.InvoiceDTO{}
				for _, d := range h.drafts(acme) {
					for _, s := range d.Sources {
						out[s.Ref] = d
					}
				}
				return out
			}

			// Sin ajuste, allow_partial: el albarán de un pedido a medias tiene su borrador.
			h.orderDelivery(acme, ana, fw.NewUUID().String(), "A-0", "per_delivery", false)
			if _, ok := bySource()["A-0"]; !ok {
				t.Fatal("allow_partial by default")
			}

			// La empresa exige el pedido completo: A-1 espera; A-2, del mismo pedido y último, lo
			// completa y los dos tienen borrador.
			conf.set(acme, bapp.CompanySettings{PartialBilling: bdomain.RequireCompleteOrder})
			o1 := fw.NewUUID().String()
			a1 := h.orderDelivery(acme, ana, o1, "A-1", "per_delivery", false)
			if _, ok := bySource()["A-1"]; ok {
				t.Fatal("a delivery note of an incomplete order waits")
			}
			pending, err := bs.SearchPendingDeliveries.Handle(actx, bapp.SearchPendingDeliveries{Seller: acme})
			h.ok(err)
			if pending.Total != 1 || pending.Items[0].Delivery != a1 || pending.Items[0].Periodicity != "per_delivery" {
				t.Fatalf("the waiting delivery note: %+v", pending.Items)
			}
			h.orderDelivery(acme, ana, o1, "A-2", "per_delivery", true)
			if d := bySource(); d["A-1"].ID == "" || d["A-2"].ID == "" {
				t.Fatalf("the order is complete: %+v", d)
			}

			// El pedido servido por el evento (cierre de lo pendiente): A-3 deja de esperar.
			o3 := fw.NewUUID().String()
			h.orderDelivery(acme, ana, o3, "A-3", "per_delivery", false)
			h.serving(acme, o3, true)
			if _, ok := bySource()["A-3"]; !ok {
				t.Fatal("the order served by its event")
			}

			// Gana el cliente: Bea admite la parcial aunque la empresa no; con la empresa en
			// allow_partial, Ana la exige.
			terms.by[bea.ID] = bdomain.AllowPartial
			h.orderDelivery(acme, bea.ID, fw.NewUUID().String(), "B-1", "per_delivery", false)
			if _, ok := bySource()["B-1"]; !ok {
				t.Fatal("the customer's allow_partial wins")
			}
			conf.set(acme, bapp.CompanySettings{})
			terms.by[ana] = bdomain.RequireCompleteOrder
			o4 := fw.NewUUID().String()
			a4 := h.orderDelivery(acme, ana, o4, "A-4", "per_delivery", false)
			if _, ok := bySource()["A-4"]; ok {
				t.Fatal("the customer's require_complete_order wins")
			}

			// Pedirlo a mano: 422; saltarla sin el permiso, 403; sin motivo, 400; con los dos, el
			// borrador, y el motivo en la auditoría.
			pid := bdomain.PendingDeliveryID{UUID: fw.MustParseUUID(a4)}
			if _, err := bs.DraftPendingDelivery.Handle(actx, bapp.DraftPendingDelivery{ID: pid}); !violation(err, bdomain.ErrPartialBillingNotAllowed) {
				t.Fatalf("drafting a waiting delivery note: %v", err)
			}
			h.grant("clerk", acme)
			path := "/api/billing/pending-deliveries/" + a4 + "/draft"
			h.refused("POST", path, "clerk", map[string]any{}, bdomain.ErrPartialBillingNotAllowed, "drafting a waiting delivery note")
			h.must(h.do("POST", path, "clerk", map[string]any{"overridePartialBilling": true, "reason": "Lo pide el cliente"}, nil), http.StatusForbidden,
				"override without its permission")
			over := bapp.PartialBillingOverride{OverridePartialBilling: true}
			if _, err := bs.DraftPendingDelivery.Handle(actx, bapp.DraftPendingDelivery{ID: pid, PartialBillingOverride: over}); !errors.Is(err, fw.ErrValidation) {
				t.Fatalf("an override without a reason: %v", err)
			}
			over.Reason = "Lo pide el cliente"
			inv, err := bs.DraftPendingDelivery.Handle(actx, bapp.DraftPendingDelivery{ID: pid, PartialBillingOverride: over})
			h.ok(err)
			if len(inv.Sources) != 1 || inv.Sources[0].ID != a4 {
				t.Fatalf("the overridden draft: %+v", inv)
			}
			trail, err := h.billing.Audit.Trail(ctx, bdomain.InvoiceKind, inv.ID)
			h.ok(err)
			found := false
			for _, r := range trail {
				for _, c := range r.Changes {
					found = found || (r.Operation == bapp.OperationPartialBillingOverridden && c.Field == "reason" && c.New == "Lo pide el cliente")
				}
			}
			if !found {
				t.Fatalf("the override in the audit: %+v", trail)
			}

			// Unir borradores de un pedido reabierto: 422 salvo que se salte.
			o5 := fw.NewUUID().String()
			h.orderDelivery(acme, ana, o5, "A-5", "per_delivery", false)
			h.orderDelivery(acme, ana, o5, "A-6", "per_delivery", true)
			h.serving(acme, o5, false)
			d := bySource()
			merge := bapp.MergeDrafts{Invoices: []string{d["A-5"].ID, d["A-6"].ID}}
			if _, err := bs.MergeDrafts.Handle(actx, merge); !violation(err, bdomain.ErrPartialBillingNotAllowed) {
				t.Fatalf("merging the drafts of a reopened order: %v", err)
			}
			merge.PartialBillingOverride = over
			merged, err := bs.MergeDrafts.Handle(actx, merge)
			h.ok(err)
			if len(merged.Sources) != 2 {
				t.Fatalf("the overridden merge: %+v", merged)
			}

			// El orden de los mensajes no importa (revisión de D-153): un albarán a medias que llega
			// después del evento de servido (un pedido cerrado antes de que llegue su albarán) nace
			// facturable y tiene su borrador.
			o8 := fw.NewUUID().String()
			h.serving(acme, o8, true)
			h.orderDelivery(acme, ana, o8, "A-8", "per_delivery", false)
			if _, ok := bySource()["A-8"]; !ok {
				t.Fatal("a delivery note after its order was served")
			}
			// Y el mismo albarán entregado dos veces, la segunda como incompleto, no vuelve a esperar:
			// un albarán no pasa el pedido de completo a incompleto (sólo el evento de reabierto).
			o9 := fw.NewUUID().String()
			m9 := h.orderDelivery(acme, ana, o9, "M-9", "monthly", true)
			h.orderDeliveryID(m9, acme, ana, o9, "M-9", "monthly", false)
			res, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, UpTo: vocab.MustDate(2026, 10, 31)})
			h.ok(err)
			if len(res.Drafts) != 1 || res.Drafts[0].Sources[0].Ref != "M-9" {
				t.Fatalf("a repeated delivery note does not reopen its order: %+v", res)
			}

			// Cliente mensual: el cierre deja fuera el albarán del pedido a medias hasta que se sirve.
			o7 := fw.NewUUID().String()
			h.orderDelivery(acme, ana, o7, "M-1", "monthly", false)
			res, err = bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, UpTo: vocab.MustDate(2026, 10, 31)})
			h.ok(err)
			if len(res.Drafts) != 0 || len(res.Failed) != 0 {
				t.Fatalf("the close leaves the waiting delivery note pending: %+v", res)
			}
			h.serving(acme, o7, true)
			res, err = bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, UpTo: vocab.MustDate(2026, 10, 31)})
			h.ok(err)
			if len(res.Drafts) != 1 || res.Drafts[0].Sources[0].Ref != "M-1" {
				t.Fatalf("the close after the order is served: %+v", res)
			}
		})
	}
}

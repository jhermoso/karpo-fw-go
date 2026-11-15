package domain_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
)

// D-153: el albarán dice si el pedido queda servido entero (OrderComplete) y el pedido lanza
// SalesOrderServingCompleted al servirse entero, con su último albarán o al cerrar lo pendiente, y
// SalesOrderServingReopened al reabrirse (en Go aún no lo hace nada más que ReopenServing).
func TestOrder_ServingCompletedAndReopened(t *testing.T) {
	served := func(o *domain.Order) (int, int) {
		var done, reopened int
		for _, e := range o.PendingEvents() {
			switch e.(type) {
			case domain.SalesOrderServingCompleted:
				done++
			case domain.SalesOrderServingReopened:
				reopened++
			}
		}
		o.ClearEvents()
		return done, reopened
	}
	confirmed := func() *domain.Order {
		o := draft(t, "0")
		if _, err := o.AddLine(line("10", "1", "0", false)); err != nil {
			t.Fatal(err)
		}
		if err := o.Confirm("P-1"); err != nil {
			t.Fatal(err)
		}
		o.ClearEvents()
		return o
	}
	issued := func(o *domain.Order, qty string) domain.DeliveryIssued {
		lines, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec(qty)}})
		if err != nil {
			t.Fatal(err)
		}
		d, err := domain.IssueDelivery(domain.NewDeliveryID(), o, "A-"+qty, date("2026-10-05"), lines, domain.PerDelivery)
		if err != nil {
			t.Fatal(err)
		}
		return d.PendingEvents()[0].(domain.DeliveryIssued)
	}

	// 4 de 10: el pedido no queda servido; 6 más, sí, con un solo evento.
	o := confirmed()
	if e := issued(o, "4"); e.OrderComplete {
		t.Fatal("a partial delivery note does not complete the order")
	}
	if done, _ := served(o); done != 0 {
		t.Fatal("a partial delivery does not complete the serving")
	}
	if e := issued(o, "6"); !e.OrderComplete {
		t.Fatal("the last delivery note completes the order")
	}
	if done, _ := served(o); done != 1 || !o.ServedEntirely() {
		t.Fatal("the last delivery completes the serving")
	}
	// Reabrir un pedido servido lanza el evento; uno sin servir entero, no.
	o.ReopenServing()
	if _, reopened := served(o); reopened != 1 {
		t.Fatal("reopening a served order")
	}

	// Cerrar con lo pendiente también lo deja servido; anular un pedido sin entregas, no.
	o = confirmed()
	issued(o, "4")
	o.ReopenServing()
	if _, reopened := served(o); reopened != 0 {
		t.Fatal("an order with something pending is not reopened")
	}
	if err := o.Close("no hay más"); err != nil {
		t.Fatal(err)
	}
	if done, _ := served(o); done != 1 || !o.ServedEntirely() {
		t.Fatal("closing the pending completes the serving")
	}
	o = confirmed()
	if err := o.Cancel("anulado"); err != nil {
		t.Fatal(err)
	}
	if done, _ := served(o); done != 0 {
		t.Fatal("a cancelled order is not served")
	}
}

// La política de facturación parcial del cliente (D-153): allow_partial, require_complete_order o
// vacía (la de la empresa).
func TestTerms_PartialBillingPolicy(t *testing.T) {
	for policy, ok := range map[string]bool{"": true, "allow_partial": true, "require_complete_order": true, "require_complete": false} {
		_, err := domain.ReconstituteTerms(domain.NewTermsID(), domain.TermsState{Company: company, Customer: customer, Discount: dec("0"),
			PartialBillingPolicy: policy})
		if (err == nil) != ok {
			t.Fatalf("policy %q: %v", policy, err)
		}
	}
}

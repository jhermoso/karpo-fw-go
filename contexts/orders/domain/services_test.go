package domain_test

import (
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// D-155: un servicio que se factura desde el pedido se envía a facturar sólo por la diferencia con
// lo ya facturado, nunca dos veces lo mismo, con un id de emisión propio; descartado su borrador,
// esa emisión vuelve a pendiente y se puede pedir otra vez.
func TestOrder_ServicesInvoicedOnlyOnce(t *testing.T) {
	o := draft(t, "0")
	for _, l := range []domain.Line{line("3", "10", "0", false), line("5", "2", "0", false)} {
		if _, err := o.AddLine(l); err != nil {
			t.Fatal(err)
		}
	}
	direct := map[int]bool{1: true} // la línea 2 va con albarán
	if _, err := o.InvoiceServices(direct, fw.NewUUID(), date("2026-10-04"), false); err == nil {
		t.Fatal("a draft order does not invoice its services")
	}
	if err := o.Confirm("P-1"); err != nil {
		t.Fatal(err)
	}
	o.ClearEvents()
	invoiced := func() string { return o.State().Lines[0].Invoiced.String() }

	// Al servir 1 de 3 (on_serve) se factura 1; servir otra vez sin más no factura nada.
	if _, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("1")}}); err != nil {
		t.Fatal(err)
	}
	first := fw.NewUUID()
	if got := o.InvoiceServedServices(direct, first, date("2026-10-05")); got != first || invoiced() != "1" {
		t.Fatalf("first invoicing: %v %s", got, invoiced())
	}
	if got := o.InvoiceServedServices(direct, fw.NewUUID(), date("2026-10-05")); !got.IsZero() {
		t.Fatal("nothing new served, nothing invoiced")
	}
	es := events[domain.ServicesInvoiceable](o)
	if len(es) != 1 || es[0].Invoicing != first.String() || len(es[0].Lines) != 1 || es[0].Lines[0].Quantity != "1" ||
		es[0].Lines[0].UnitPrice != "10.0000" || es[0].Date != "2026-10-05" || es[0].Number != "P-1" {
		t.Fatalf("the invoiceable event: %+v", es)
	}

	// Con on_serve, pedirlo no factura lo no servido: nada que facturar.
	if _, err := o.InvoiceServices(direct, fw.NewUUID(), date("2026-10-06"), true); err == nil || invoiced() != "1" {
		t.Fatalf("on_serve does not invoice what is not served: %v %s", err, invoiced())
	}

	// A petición (manual, o al confirmar) se factura el resto de la cantidad: 2; después, nada.
	second := fw.NewUUID()
	if got, err := o.InvoiceServices(direct, second, date("2026-10-06"), false); err != nil || got != second || invoiced() != "3" {
		t.Fatalf("the rest: %v %v %s", got, err, invoiced())
	}
	if es := events[domain.ServicesInvoiceable](o); len(es) != 1 || es[0].Lines[0].Quantity != "2" {
		t.Fatalf("only the delta: %+v", es)
	}
	if _, err := o.InvoiceServices(direct, fw.NewUUID(), date("2026-10-06"), false); err == nil {
		t.Fatal("nothing left: 422 orders.nothing_to_invoice")
	}

	// Descartado el borrador de la primera emisión, su 1 vuelve a pendiente; dos veces, igual.
	o.ReleaseServiceInvoicing(first)
	o.ReleaseServiceInvoicing(first)
	if invoiced() != "2" || len(o.State().ServiceInvoicings) != 1 {
		t.Fatalf("released: %s %+v", invoiced(), o.State().ServiceInvoicings)
	}
	if _, err := o.InvoiceServices(direct, fw.NewUUID(), date("2026-10-07"), false); err != nil || invoiced() != "3" {
		t.Fatalf("invoiced again: %v %s", err, invoiced())
	}
	if _, err := domain.ReconstituteOrder(o.ID(), o.State()); err != nil {
		t.Fatal(err)
	}
}

// D-155: con on_serve, lo que vuelve a pendiente al descartar se pide otra vez sólo por lo servido;
// un pedido con servicios facturados (on_confirm) no se anula: 422 orders.services_invoiced.
func TestOrder_ServicesOnServeAndCancel(t *testing.T) {
	confirmed := func() *domain.Order {
		o := draft(t, "0")
		if _, err := o.AddLine(line("2", "20", "0", false)); err != nil {
			t.Fatal(err)
		}
		if err := o.Confirm("P-2"); err != nil {
			t.Fatal(err)
		}
		return o
	}
	direct := map[int]bool{1: true}
	o := confirmed()
	if _, err := o.Deliver([]domain.Pick{{Line: 1, Quantity: dec("1")}}); err != nil {
		t.Fatal(err)
	}
	first := o.InvoiceServedServices(direct, fw.NewUUID(), date("2026-10-05"))
	o.ReleaseServiceInvoicing(first)
	o.ClearEvents()
	if _, err := o.InvoiceServices(direct, fw.NewUUID(), date("2026-10-06"), true); err != nil {
		t.Fatal(err)
	}
	if es := events[domain.ServicesInvoiceable](o); len(es) != 1 || es[0].Lines[0].Quantity != "1" {
		t.Fatalf("only what was served: %+v", es)
	}

	o = confirmed()
	if _, err := o.InvoiceServices(direct, fw.NewUUID(), date("2026-10-04"), false); err != nil {
		t.Fatal(err)
	}
	var v *fw.RuleViolationError
	if err := o.Cancel("No se hace"); !errors.As(err, &v) || v.Code != "orders.services_invoiced" {
		t.Fatalf("cancel with invoiced services: %v", err)
	}
	o.ReleaseServiceInvoicing(o.State().ServiceInvoicings[0].ID)
	if err := o.Cancel("No se hace"); err != nil {
		t.Fatalf("cancel after the discard: %v", err)
	}
}

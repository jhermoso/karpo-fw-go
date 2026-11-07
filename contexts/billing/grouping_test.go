package billing_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// delivery entrega a Billing el albarán number de customer, con una línea de qty × 10,00, como lo
// publica Orders; devuelve su Id.
func (h *host) delivery(acme, customer, number, date, periodicity, qty string) string {
	h.t.Helper()
	id := fw.NewUUID().String()
	data, _ := json.Marshal(map[string]any{"deliveryId": id, "orderNumber": "P-" + number, "company": acme, "customer": customer,
		"number": number, "date": date, "billingPeriodicity": periodicity, "lines": []map[string]any{{"sku": "T", "description": "Tornillo",
			"taxCode": "G21", "quantity": qty, "netPrice": "10.0000"}}})
	h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
		Source: "orders", Data: data}))
	return id
}

func (h *host) drafts(acme string) []bapp.InvoiceDTO {
	h.t.Helper()
	page, err := h.billing.Service.SearchInvoices.Handle(h.adminCtx, bapp.SearchInvoices{Seller: acme, Status: "draft", Size: 100})
	h.ok(err)
	return page.Items
}

func violation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

// Varios borradores de albaranes de un cliente se unen a mano en uno (D-146): las líneas y los
// orígenes de todos, la última fecha de operación y una descripción que cita los albaranes; los demás
// desaparecen. No se unen menos de dos, ni de clientes distintos, ni uno emitido, ni uno hecho a
// mano, ni dos que se calculan distinto (D-144).
func TestBilling_MergeDeliveryDrafts(t *testing.T) {
	conf := &settings{by: map[string]bapp.CompanySettings{}}
	h := compose(t, billing.WithSettings(conf))
	acme, ana := h.seller(t)
	bs, actx := h.billing.Service, h.adminCtx
	bea, err := h.parties.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Bea", FirstSurname: "Otra"})
	h.ok(err)
	series, err := bs.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme, Code: "FA", Year: 2026})
	h.ok(err)

	a1 := h.delivery(acme, ana, "A-1", "2026-10-03", "per_delivery", "1")
	a2 := h.delivery(acme, ana, "A-2", "2026-10-07", "", "2") // un albarán de antes de D-146, sin periodicidad
	h.delivery(acme, bea.ID, "B-1", "2026-10-05", "per_delivery", "1")
	conf.set(acme, bapp.CompanySettings{Calculation: bdomain.Calculation{Rounding: bdomain.RoundOnce, PriceDecimals: 6}})
	h.delivery(acme, ana, "A-6", "2026-10-08", "per_delivery", "1")
	conf.set(acme, bapp.CompanySettings{})
	h.delivery(acme, ana, "A-9", "2026-10-09", "per_delivery", "1")
	manual, err := bs.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: acme, Customer: ana, DetailsInput: bapp.DetailsInput{Description: "A mano"}})
	h.ok(err)

	by := map[string]bapp.InvoiceDTO{}
	for _, d := range h.drafts(acme) {
		by[d.SourceRef] = d
	}
	if len(by) != 6 {
		t.Fatalf("one draft per delivery note and the manual one: %+v", by)
	}
	issued, err := bs.Issue.Handle(actx, bapp.IssueInvoice{ID: bdomain.InvoiceID{UUID: fw.MustParseUUID(by["A-9"].ID)}, Series: series.ID,
		Date: vocab.MustDate(2026, 10, 10)})
	h.ok(err)

	merge := func(ids ...string) (bapp.InvoiceDTO, error) {
		return bs.MergeDrafts.Handle(actx, bapp.MergeDrafts{Invoices: ids})
	}
	for code, ids := range map[string][]string{
		"billing.merge_too_few":        {by["A-1"].ID},
		"billing.merge_other_customer": {by["A-1"].ID, by["B-1"].ID},
		"billing.invoice_not_draft":    {by["A-1"].ID, issued.ID},
		"billing.merge_unsourced":      {by["A-1"].ID, manual.ID},
		"billing.merge_calculation":    {by["A-1"].ID, by["A-6"].ID},
	} {
		if _, err := merge(ids...); !violation(err, code) {
			t.Fatalf("%s: %v", code, err)
		}
	}
	if _, err := merge(by["A-1"].ID, by["A-1"].ID); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("the same draft twice: %v", err)
	}

	// Sin el permiso de editar borradores, no se unen (403); con él, sí.
	h.grant("clerk", acme)
	h.must(h.do("POST", "/api/billing/invoices/merge", "issuer", bapp.MergeDrafts{Invoices: []string{by["A-2"].ID, by["A-1"].ID}}, nil), http.StatusForbidden,
		"merge without the permission")
	var got bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/invoices/merge", "clerk", bapp.MergeDrafts{Invoices: []string{by["A-2"].ID, by["A-1"].ID}}, &got), http.StatusOK,
		"merge")
	if got.ID != by["A-2"].ID || len(got.Sources) != 2 || got.Sources[0].ID != a2 || got.Sources[1].ID != a1 || got.SourceID != a2 ||
		got.Description != "Albaranes A-2, A-1" || got.OperationDate != "2026-10-07" || len(got.Lines) != 2 || got.Net != "30.00" {
		t.Fatalf("merged draft: %+v", got)
	}
	if _, err := bs.GetInvoice.Handle(actx, bapp.GetInvoice{ID: bdomain.InvoiceID{UUID: fw.MustParseUUID(by["A-1"].ID)}}); !errors.Is(err, fw.ErrNotFound) {
		t.Fatalf("the merged draft is gone: %v", err)
	}
	if _, err := bs.Discard.Handle(actx, bapp.DiscardInvoice{ID: bdomain.InvoiceID{UUID: fw.MustParseUUID(got.ID)}}); !violation(err, "billing.sourced_draft") {
		t.Fatalf("a merged draft is not discarded: %v", err)
	}
	// El mismo albarán, entregado otra vez, no vuelve a tener borrador.
	data, _ := json.Marshal(map[string]any{"deliveryId": a1, "orderNumber": "P-A-1", "company": acme, "customer": ana, "number": "A-1",
		"date": "2026-10-03", "lines": []map[string]any{{"sku": "T", "description": "Tornillo", "taxCode": "G21", "quantity": "1", "netPrice": "10"}}})
	h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
		Source: "orders", Data: data}))
	if n := len(h.drafts(acme)); n != 4 {
		t.Fatalf("drafts after the merge: %d", n)
	}
}

// Un cliente con facturación mensual no recibe un borrador por albarán: sus albaranes quedan
// pendientes y el cierre del periodo prepara uno por cliente con los de hasta la fecha de corte. Un
// segundo cierre no prepara nada (D-146).
func TestBilling_ClosePeriod(t *testing.T) {
	h := compose(t)
	acme, ana := h.seller(t)
	bs, actx := h.billing.Service, h.adminCtx
	bea, err := h.parties.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Bea", FirstSurname: "Mensual"})
	h.ok(err)
	m1 := h.delivery(acme, ana, "M-1", "2026-10-02", "monthly", "1")
	m2 := h.delivery(acme, ana, "M-2", "2026-10-20", "monthly", "2")
	h.delivery(acme, ana, "M-3", "2026-11-02", "monthly", "4") // del periodo siguiente
	h.delivery(acme, bea.ID, "B-1", "2026-10-15", "monthly", "1")
	h.delivery(acme, ana, "M-1", "2026-10-02", "monthly", "1") // otro albarán con el mismo número: es otro Id
	if n := len(h.drafts(acme)); n != 0 {
		t.Fatalf("no draft per delivery note for a monthly customer: %d", n)
	}

	if _, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a close without its date: %v", err)
	}
	h.grant("clerk", acme)
	h.must(h.do("POST", "/api/billing/periods/close", "issuer", bapp.ClosePeriod{Seller: acme, Customer: ana, UpTo: vocab.MustDate(2026, 10, 31)}, nil),
		http.StatusForbidden, "close without the permission to prepare drafts")
	var closed []bapp.InvoiceDTO
	h.must(h.do("POST", "/api/billing/periods/close", "clerk", bapp.ClosePeriod{Seller: acme, Customer: ana, UpTo: vocab.MustDate(2026, 10, 31)}, &closed),
		http.StatusOK, "close")
	if len(closed) != 1 || len(closed[0].Sources) != 3 || closed[0].Sources[0].ID != m1 || closed[0].Sources[2].ID != m2 || closed[0].Customer != ana ||
		closed[0].Net != "40.00" || closed[0].OperationDate != "2026-10-20" || closed[0].Description != "Albaranes M-1, M-1, M-2" {
		t.Fatalf("the draft of the period: %+v", closed)
	}
	again, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, Customer: ana, UpTo: vocab.MustDate(2026, 10, 31)})
	if err != nil || len(again) != 0 {
		t.Fatalf("a second close prepares nothing: %+v %v", again, err)
	}
	// Sin cliente, el resto de clientes del vendedor.
	rest, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, UpTo: vocab.MustDate(2026, 10, 31)})
	if err != nil || len(rest) != 1 || rest[0].Customer != bea.ID || rest[0].Description != "Albarán B-1 (pedido P-B-1)" {
		t.Fatalf("the other customer: %+v %v", rest, err)
	}
	// Un albarán ya facturado que llega otra vez no queda pendiente.
	data, _ := json.Marshal(map[string]any{"deliveryId": m1, "orderNumber": "P-M-1", "company": acme, "customer": ana, "number": "M-1",
		"date": "2026-10-02", "billingPeriodicity": "monthly", "lines": []map[string]any{{"sku": "T", "description": "Tornillo", "taxCode": "G21",
			"quantity": "1", "netPrice": "10"}}})
	h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
		Source: "orders", Data: data}))
	next, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, UpTo: vocab.MustDate(2026, 11, 30)})
	if err != nil || len(next) != 1 || len(next[0].Sources) != 1 || next[0].SourceRef != "M-3" || next[0].Net != "40.00" {
		t.Fatalf("the next period: %+v %v", next, err)
	}
	if n := len(h.drafts(acme)); n != 3 {
		t.Fatalf("one draft per customer and period: %d", n)
	}
}

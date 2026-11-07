package billing_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
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
		got.Description != "Albaranes A-1, A-2" || got.OperationDate != "2026-10-07" || len(got.Lines) != 2 || got.Net != "30.00" {
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
// pendientes, se listan, y el cierre del periodo prepara uno por cliente con los de hasta la fecha
// de corte. Un segundo cierre no prepara nada; uno que llega tarde entra en el cierre siguiente
// (D-146).
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

	// Los pendientes se listan con el permiso de leer facturas, en el ámbito de quien lee.
	var pending struct {
		Items []bapp.PendingDeliveryDTO `json:"items"`
	}
	h.must(h.do("GET", "/api/billing/pending-deliveries?seller="+acme+"&customer="+ana, "clerk", nil, &pending), http.StatusOK, "pending outside the scope")
	if len(pending.Items) != 0 {
		t.Fatalf("pending outside the scope: %+v", pending.Items)
	}
	h.grant("clerk", acme)
	h.must(h.do("GET", "/api/billing/pending-deliveries?seller="+acme+"&customer="+ana, "clerk", nil, &pending), http.StatusOK, "pending")
	if len(pending.Items) != 4 || pending.Items[0].Delivery != m1 || pending.Items[3].Number != "M-3" || pending.Items[1].Net != "10.00" {
		t.Fatalf("pending delivery notes: %+v", pending.Items)
	}

	if _, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme}); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a close without its date: %v", err)
	}
	h.must(h.do("POST", "/api/billing/periods/close", "issuer", bapp.ClosePeriod{Seller: acme, Customer: ana, UpTo: vocab.MustDate(2026, 10, 31)}, nil),
		http.StatusForbidden, "close without the permission to prepare drafts")
	var res bapp.ClosePeriodResult
	h.must(h.do("POST", "/api/billing/periods/close", "clerk", bapp.ClosePeriod{Seller: acme, Customer: ana, UpTo: vocab.MustDate(2026, 10, 31)}, &res),
		http.StatusOK, "close")
	closed := res.Drafts
	if len(res.Failed) != 0 || len(closed) != 1 || len(closed[0].Sources) != 3 || closed[0].Sources[0].ID != m1 || closed[0].Sources[2].ID != m2 ||
		closed[0].Customer != ana || closed[0].Net != "40.00" || closed[0].OperationDate != "2026-10-20" || closed[0].Description != "Albaranes M-1, M-1, M-2" {
		t.Fatalf("the draft of the period: %+v", res)
	}
	again, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, Customer: ana, UpTo: vocab.MustDate(2026, 10, 31)})
	if err != nil || len(again.Drafts) != 0 || len(again.Failed) != 0 {
		t.Fatalf("a second close prepares nothing: %+v %v", again, err)
	}
	// Sin cliente, el resto de clientes del vendedor.
	rest, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, UpTo: vocab.MustDate(2026, 10, 31)})
	if err != nil || len(rest.Drafts) != 1 || rest.Drafts[0].Customer != bea.ID || rest.Drafts[0].Description != "Albarán B-1 (pedido P-B-1)" {
		t.Fatalf("the other customer: %+v %v", rest, err)
	}
	// Un albarán ya facturado que llega otra vez no queda pendiente.
	data, _ := json.Marshal(map[string]any{"deliveryId": m1, "orderNumber": "P-M-1", "company": acme, "customer": ana, "number": "M-1",
		"date": "2026-10-02", "billingPeriodicity": "monthly", "lines": []map[string]any{{"sku": "T", "description": "Tornillo", "taxCode": "G21",
			"quantity": "1", "netPrice": "10"}}})
	h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
		Source: "orders", Data: data}))
	// Uno que llega tarde, con fecha de un periodo ya cerrado, entra en el cierre siguiente.
	late := h.delivery(acme, ana, "M-0", "2026-10-30", "monthly", "1")
	next, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, UpTo: vocab.MustDate(2026, 11, 30)})
	if err != nil || len(next.Drafts) != 1 || len(next.Drafts[0].Sources) != 2 || next.Drafts[0].Sources[0].ID != late || next.Drafts[0].Net != "50.00" ||
		next.Drafts[0].OperationDate != "2026-11-02" {
		t.Fatalf("the next period: %+v %v", next, err)
	}
	if n := len(h.drafts(acme)); n != 3 {
		t.Fatalf("one draft per customer and period: %d", n)
	}
}

// pendingDelivery entrega a Billing un albarán mensual con lines líneas.
func (h *host) pendingDelivery(acme, customer, number, taxCode string, lines int) string {
	h.t.Helper()
	id, err := h.monthlyDelivery(acme, customer, number, taxCode, lines)
	h.ok(err)
	return id
}

// monthlyDelivery es pendingDelivery con el error de Billing al recibir el mensaje.
func (h *host) monthlyDelivery(acme, customer, number, taxCode string, lines int) (string, error) {
	ls := make([]map[string]any, 0, lines)
	for range lines {
		ls = append(ls, map[string]any{"sku": "T", "description": "Tornillo", "taxCode": taxCode, "quantity": "1", "netPrice": "1"})
	}
	id := fw.NewUUID().String()
	data, _ := json.Marshal(map[string]any{"deliveryId": id, "orderNumber": "P-" + number, "company": acme, "customer": customer,
		"number": number, "date": "2026-10-10", "billingPeriodicity": "monthly", "lines": ls})
	return id, h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
		Source: "orders", Data: data})
}

// El cierre va cliente a cliente: con muchos albaranes la descripción se acorta; con más de 500
// líneas el borrador se parte sin partir un albarán; un albarán mensual que no se podría facturar se
// rechaza al llegar y no queda pendiente; un pendiente defectuoso (de antes de esa comprobación) hace
// fallar sólo a su cliente, cuyos albaranes siguen pendientes; y un pendiente que ya está en una
// factura se borra sin otro borrador. Sobre SQLite migrada, para poder guardar el pendiente
// defectuoso sin pasar por el mensaje.
func TestBilling_ClosePeriodPerCustomer(t *testing.T) {
	h := compose(t)
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "close.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
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
	bs, actx, ps := h.billing.Service, h.adminCtx, h.parties.Service
	person := func(name string) string {
		p, err := ps.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: name, FirstSurname: "Cierre"})
		h.ok(err)
		return p.ID
	}
	many, big, bad, old := person("Muchos"), person("Grande"), person("Mala"), person("Antigua")
	for k := range 40 {
		h.pendingDelivery(acme, many, fmt.Sprintf("ALB-2026-%06d", 101+k), "G21", 1)
	}
	h.pendingDelivery(acme, big, "G-1", "G21", 200)
	h.pendingDelivery(acme, big, "G-2", "G21", 200)
	h.pendingDelivery(acme, big, "G-3", "G21", 200)
	// Sin código de impuesto la factura no admite la línea: el mensaje se rechaza y no queda pendiente.
	if _, err := h.monthlyDelivery(acme, bad, "X-1", "", 1); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("a delivery note that could not be invoiced: %v", err)
	}
	h.pendingDelivery(acme, bad, "X-2", "G21", 1)
	// Uno así guardado antes de que se comprobara al llegar: falla su cliente, no el cierre.
	repo, err := binfra.PendingDeliveryRepositoryFactory(db)
	h.ok(err)
	defective, err := bdomain.ReconstitutePendingDelivery(bdomain.PendingDeliveryID{UUID: fw.NewUUID()}, bdomain.PendingDeliveryState{
		Seller: bdomain.OrganizationID{UUID: fw.MustParseUUID(acme)}, Customer: bdomain.PartyID{UUID: fw.MustParseUUID(old)}, Number: "V-1",
		Date: vocab.MustDate(2026, 10, 10), Lines: []bdomain.LineInput{{Description: "Tornillo", Quantity: vocab.MustDecimal("1"), UnitPrice: vocab.MustDecimal("1")}}})
	h.ok(err)
	h.ok(repo.Save(context.Background(), defective))
	ahead, err := bs.SearchPendingDeliveries.Handle(actx, bapp.SearchPendingDeliveries{Seller: acme, Customer: bad, UpTo: "2026-10-31"})
	if err != nil || len(ahead.Items) != 1 || ahead.Items[0].Number != "X-2" {
		t.Fatalf("what a close would invoice: %+v %v", ahead.Items, err)
	}
	if none, err := bs.SearchPendingDeliveries.Handle(actx, bapp.SearchPendingDeliveries{Seller: acme, UpTo: "2026-10-09"}); err != nil || len(none.Items) != 0 {
		t.Fatalf("nothing before the 10th: %+v %v", none.Items, err)
	}
	// Un pendiente que ya está en una factura: llegó como mensual y otra vez como por albarán.
	twice := h.pendingDelivery(acme, ana, "T-1", "G21", 1)
	data, _ := json.Marshal(map[string]any{"deliveryId": twice, "orderNumber": "P-T-1", "company": acme, "customer": ana, "number": "T-1",
		"date": "2026-10-10", "billingPeriodicity": "per_delivery", "lines": []map[string]any{{"sku": "T", "description": "Tornillo", "taxCode": "G21",
			"quantity": "1", "netPrice": "1"}}})
	h.ok(h.billing.Consumer.HandleMessage(context.Background(), application.Envelope{ID: fw.NewUUID().String(), Type: "orders.delivery-issued.v1",
		Source: "orders", Data: data}))

	res, err := bs.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme, UpTo: vocab.MustDate(2026, 10, 31)})
	h.ok(err)
	by := map[string][]bapp.InvoiceDTO{}
	for _, d := range res.Drafts {
		by[d.Customer] = append(by[d.Customer], d)
	}
	if m := by[many]; len(m) != 1 || len(m[0].Sources) != 40 || m[0].Description != "Albaranes ALB-2026-000101 a ALB-2026-000140 (40)" {
		t.Fatalf("forty delivery notes: %+v", m)
	}
	if b := by[big]; len(b) != 2 || len(b[0].Lines) != 400 || len(b[0].Sources) != 2 || len(b[1].Lines) != 200 || b[1].SourceRef != "G-3" ||
		b[1].Description != "Albarán G-3 (pedido P-G-3)" {
		t.Fatalf("600 lines in two drafts: %d", len(b))
	}
	if x := by[bad]; len(x) != 1 || x[0].SourceRef != "X-2" {
		t.Fatalf("the customer with a refused delivery note still closes: %+v", x)
	}
	if len(by[ana]) != 0 || len(res.Failed) != 1 || res.Failed[0].Customer != old || res.Failed[0].Code != "billing.close_invalid_line" {
		t.Fatalf("the failed customer: %+v", res.Failed)
	}
	page, err := bs.SearchPendingDeliveries.Handle(actx, bapp.SearchPendingDeliveries{Seller: acme})
	if err != nil || len(page.Items) != 1 || page.Items[0].Customer != old {
		t.Fatalf("what stays pending: %+v %v", page.Items, err)
	}
	// Cuarenta borradores unidos a mano también se describen en corto.
	var ids []string
	for k := range 40 {
		h.delivery(acme, ana, fmt.Sprintf("ALB-2026-%06d", 201+k), "2026-10-11", "per_delivery", "1")
	}
	for _, d := range h.drafts(acme) {
		if d.Customer == ana && len(d.Sources) == 1 && d.SourceRef != "T-1" {
			ids = append(ids, d.ID)
		}
	}
	merged, err := bs.MergeDrafts.Handle(actx, bapp.MergeDrafts{Invoices: ids})
	if err != nil || len(merged.Sources) != 40 || len(merged.Description) > 500 {
		t.Fatalf("forty drafts merged: %q %v", merged.Description, err)
	}
}

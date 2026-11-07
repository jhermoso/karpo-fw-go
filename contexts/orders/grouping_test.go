package orders_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	iinfra "github.com/jhermoso/karpo-fw-go/contexts/inventory/infrastructure"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	oinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// grouping recorre los dos modos de facturar varios albaranes en una factura (D-146): Ana, por
// albarán, une a mano dos borradores y emite una factura que factura los dos albaranes; Bea, mensual,
// no recibe borradores y el cierre del periodo le prepara uno con sus tres albaranes, una sola vez.
func (c *chain) grouping(tag string) {
	t, ctx := c.t, c.ctx
	nif := map[string][3]string{"m": {"A28017895", "11111111H", "22222222J"}, "s": {"B12345674", "87654321X", "33333333P"}}[tag]
	acme, err := c.pm.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Agrupa " + tag,
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	c.ok(err)
	ana, err := c.pm.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Albaranes " + tag})
	c.ok(err)
	bea, err := c.pm.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Bea", FirstSurname: "Mensual " + tag})
	c.ok(err)
	for party, d := range map[string][2]string{acme.ID: {"c0000000-0004-0000-0000-000000000003", nif[0]},
		ana.ID: {"c0000000-0004-0000-0000-000000000002", nif[1]}, bea.ID: {"c0000000-0004-0000-0000-000000000002", nif[2]}} {
		id, _ := pdomain.ParsePartyID(party)
		_, err := c.pm.Service.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: d[0], Country: "ES", Number: d[1], Primary: true})
		c.ok(err)
		// El domicilio fiscal: sin él no se emite una factura completa (D-147).
		_, err = c.pm.Service.AddContact.Handle(ctx, papp.AddContact{PartyID: id, Kind: "postal", Purposes: []string{"default"},
			Address: &papp.AddressDTO{Line1: "Calle Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
		c.ok(err)
	}
	_, err = c.fm.Service.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	c.ok(err)
	_, err = c.fm.Service.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	c.ok(err)
	series, err := c.bm.Service.OpenSeries.Handle(ctx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
	c.ok(err)
	montaje, err := c.dm.Service.RegisterProduct.Handle(ctx, dapp.RegisterProduct{Company: acme.ID, SKU: "MONT", DetailsInput: dapp.DetailsInput{
		Name: "Montaje " + tag, Kind: "service", UoM: "h", TaxCode: "G21", BasePrice: "35", ForSale: true}})
	c.ok(err)
	terms, err := c.om.Service.SetTerms.Handle(ctx, oapp.SetTerms{Company: acme.ID, Customer: bea.ID, BillingPeriodicity: "monthly"})
	if err != nil || terms.BillingPeriodicity != "monthly" {
		t.Fatalf("monthly terms: %+v %v", terms, err)
	}

	// Un pedido de horas de montaje, entregado entero en un albarán.
	delivered := func(customer, hours string, day int) oapp.DeliveryDTO {
		t.Helper()
		o, err := c.om.Service.DraftOrder.Handle(ctx, oapp.DraftOrder{Company: acme.ID, Customer: customer, Date: vocab.MustDate(2026, 10, 1)})
		c.ok(err)
		id, _ := odomain.ParseOrderID(o.ID)
		_, err = c.om.Service.AddLine.Handle(ctx, oapp.AddLine{ID: id, Product: montaje.ID, Quantity: hours})
		c.ok(err)
		_, err = c.om.Service.Confirm.Handle(ctx, oapp.ConfirmOrder{ID: id})
		c.ok(err)
		c.deliver()
		note, err := c.om.Service.Deliver.Handle(ctx, oapp.Deliver{ID: id, Date: vocab.MustDate(2026, 10, day)})
		c.ok(err)
		c.deliver()
		return note
	}
	drafts := func(customer string) []bapp.InvoiceDTO {
		t.Helper()
		page, err := c.bm.Service.SearchInvoices.Handle(ctx, bapp.SearchInvoices{Seller: acme.ID, Customer: customer, Status: "draft"})
		c.ok(err)
		return page.Items
	}
	invoiced := func(invoice bapp.InvoiceDTO, notes ...oapp.DeliveryDTO) {
		t.Helper()
		for _, n := range notes {
			id, _ := odomain.ParseDeliveryID(n.ID)
			got, err := c.om.Service.GetDelivery.Handle(ctx, oapp.GetDelivery{ID: id})
			if err != nil || got.Invoice != invoice.ID || got.InvoiceNo != invoice.Number {
				t.Fatalf("delivery note %s invoiced by %s: %+v %v", n.Number, invoice.Number, got, err)
			}
		}
	}

	// A mano: dos albaranes de Ana, dos borradores, uno solo al unirlos.
	n1, n2 := delivered(ana.ID, "1", 5), delivered(ana.ID, "2", 6)
	two := drafts(ana.ID)
	if len(two) != 2 {
		t.Fatalf("one draft per delivery note: %+v", two)
	}
	merged, err := c.bm.Service.MergeDrafts.Handle(ctx, bapp.MergeDrafts{Invoices: []string{two[0].ID, two[1].ID}})
	c.ok(err)
	if len(merged.Sources) != 2 || merged.Net != "105.00" || merged.OperationDate != "2026-10-06" || len(drafts(ana.ID)) != 1 {
		t.Fatalf("merged draft: %+v", merged)
	}
	id, _ := bdomain.ParseInvoiceID(merged.ID)
	inv, err := c.bm.Service.Issue.Handle(ctx, bapp.IssueInvoice{ID: id, Series: series.ID, Date: vocab.MustDate(2026, 10, 7)})
	c.ok(err)
	c.deliver()
	if inv.Taxes.Total != "127.05" {
		t.Fatalf("issued: %+v", inv)
	}
	invoiced(inv, n1, n2)
	exp, err := c.rm.Credit.Exposure(context.Background(), acme.ID, ana.ID, "2026-10-07")
	if err != nil || exp.Open != "127.05" {
		t.Fatalf("receivable of the merged invoice: %+v %v", exp, err)
	}

	// Por periodo: los tres albaranes de Bea quedan pendientes; el cierre prepara un borrador.
	m1, m2, m3 := delivered(bea.ID, "1", 2), delivered(bea.ID, "1", 15), delivered(bea.ID, "1", 28)
	if d := drafts(bea.ID); len(d) != 0 {
		t.Fatalf("no draft per delivery note for a monthly customer: %+v", d)
	}
	open, err := c.om.Service.SearchDeliveries.Handle(ctx, oapp.SearchDeliveries{Company: acme.ID, Customer: bea.ID, Uninvoiced: true})
	if err != nil || len(open.Items) != 3 {
		t.Fatalf("delivery notes to invoice: %+v %v", open.Items, err)
	}
	res, err := c.bm.Service.ClosePeriod.Handle(ctx, bapp.ClosePeriod{Seller: acme.ID, UpTo: vocab.MustDate(2026, 10, 31)})
	c.ok(err)
	closed := res.Drafts
	if len(closed) != 1 || closed[0].Customer != bea.ID || len(closed[0].Sources) != 3 || closed[0].Net != "105.00" ||
		closed[0].Description != "Albaranes "+m1.Number+", "+m2.Number+", "+m3.Number {
		t.Fatalf("the draft of the period: %+v", closed)
	}
	again, err := c.bm.Service.ClosePeriod.Handle(ctx, bapp.ClosePeriod{Seller: acme.ID, UpTo: vocab.MustDate(2026, 10, 31)})
	if err != nil || len(again.Drafts) != 0 || len(again.Failed) != 0 || len(drafts(bea.ID)) != 1 {
		t.Fatalf("a second close does not repeat: %+v %v", again, err)
	}
	id, _ = bdomain.ParseInvoiceID(closed[0].ID)
	inv, err = c.bm.Service.Issue.Handle(ctx, bapp.IssueInvoice{ID: id, Series: series.ID, Date: vocab.MustDate(2026, 10, 31)})
	c.ok(err)
	c.deliver()
	invoiced(inv, m1, m2, m3)
	if open, err := c.om.Service.SearchDeliveries.Handle(ctx, oapp.SearchDeliveries{Company: acme.ID, Uninvoiced: true}); err != nil || len(open.Items) != 0 {
		t.Fatalf("nothing left to invoice: %+v %v", open.Items, err)
	}
}

func TestOrders_DeliveryNotesAreGrouped_MemoryThenSQLite(t *testing.T) {
	c := composeChain(t)
	c.grouping("m")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "grouping.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), dinfra.Migrations(), iinfra.Migrations(),
		rinfra.Migrations(), oinfra.Migrations(), binfra.Migrations()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.sw.Swap(ctx, db); err != nil {
		t.Fatal(err)
	}
	c.grouping("s")
}

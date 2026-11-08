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

// chainedDiscount vende 1000 unidades a 1 € con un 5 % del precio especial y un 2,5 % del cliente,
// las entrega en un albarán y emite su factura: el pedido, el albarán y la factura valen 926,25
// (descuento encadenado 7,375 %), no los 926,30 del precio neto unitario 0,9263 (D-149).
func (c *chain) chainedDiscount(tag string) {
	t, ctx := c.t, c.ctx
	acme, err := c.pm.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme " + tag,
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	c.ok(err)
	ana, err := c.pm.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Núñez " + tag})
	c.ok(err)
	for party, d := range map[string][2]string{acme.ID: {"c0000000-0004-0000-0000-000000000003", "A58818501"},
		ana.ID: {"c0000000-0004-0000-0000-000000000002", "12345678Z"}} {
		id, _ := pdomain.ParsePartyID(party)
		_, err := c.pm.Service.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: d[0], Country: "ES", Number: d[1], Primary: true})
		c.ok(err)
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
		Name: "Montaje " + tag, Kind: "service", UoM: "h", TaxCode: "G21", BasePrice: "3", ForSale: true}})
	c.ok(err)
	_, err = c.dm.Service.SetSpecialPrice.Handle(ctx, dapp.SetSpecialPrice{Company: acme.ID, Customer: ana.ID, Product: montaje.ID, UnitPrice: "1",
		Discount: "5", From: vocab.MustDate(2026, 1, 1)})
	c.ok(err)
	_, err = c.om.Service.SetTerms.Handle(ctx, oapp.SetTerms{Company: acme.ID, Customer: ana.ID, Discount: "2.5"})
	c.ok(err)

	o, err := c.om.Service.DraftOrder.Handle(ctx, oapp.DraftOrder{Company: acme.ID, Customer: ana.ID, Date: vocab.MustDate(2026, 10, 4)})
	c.ok(err)
	oid, _ := odomain.ParseOrderID(o.ID)
	o, err = c.om.Service.AddLine.Handle(ctx, oapp.AddLine{ID: oid, Product: montaje.ID, Quantity: "1000"})
	c.ok(err)
	if l := o.Lines[0]; l.UnitPrice != "1.0000" || l.Discount != "5.00" || l.ChainedDiscount != "7.3750" || l.NetPrice != "0.9263" || l.Amount != "926.25" ||
		o.Total != "926.25" {
		t.Fatalf("order: %+v", o)
	}
	_, err = c.om.Service.Confirm.Handle(ctx, oapp.ConfirmOrder{ID: oid})
	c.ok(err)
	note, err := c.om.Service.Deliver.Handle(ctx, oapp.Deliver{ID: oid, Date: vocab.MustDate(2026, 10, 5)})
	c.ok(err)
	if l := note.Lines[0]; l.UnitPrice != "1.0000" || l.Discount != "7.375" || l.Amount != "926.25" || note.Total != "926.25" {
		t.Fatalf("delivery note: %+v", note)
	}
	c.deliver()
	drafts, err := c.bm.Service.SearchInvoices.Handle(ctx, bapp.SearchInvoices{Seller: acme.ID, Status: "draft"})
	c.ok(err)
	if len(drafts.Items) != 1 || drafts.Items[0].Net != "926.25" || drafts.Items[0].Lines[0].Discount != "7.375" {
		t.Fatalf("draft from the delivery note: %+v", drafts.Items)
	}
	iid, _ := bdomain.ParseInvoiceID(drafts.Items[0].ID)
	inv, err := c.bm.Service.Issue.Handle(ctx, bapp.IssueInvoice{ID: iid, Series: series.ID, Date: vocab.MustDate(2026, 10, 6)})
	c.ok(err)
	if inv.Net != "926.25" || inv.Taxes.Net != "926.25" || inv.Taxes.Total != "1120.76" {
		t.Fatalf("issued invoice: %+v %+v", inv, inv.Taxes)
	}
}

func TestOrders_ChainedDiscountLikeTheInvoice_MemoryThenSQLite(t *testing.T) {
	c := composeChain(t)
	c.chainedDiscount("m")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "chained.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
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
	c.chainedDiscount("s")
}

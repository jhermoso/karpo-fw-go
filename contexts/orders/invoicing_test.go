package orders_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/inventory"
	iapp "github.com/jhermoso/karpo-fw-go/contexts/inventory/application"
	iinfra "github.com/jhermoso/karpo-fw-go/contexts/inventory/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/orders"
	oapp "github.com/jhermoso/karpo-fw-go/contexts/orders/application"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	oinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/products"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// chain composes the whole sales cycle on one backend: Parties, Fiscal, Products, Inventory,
// Receivables, Orders and Billing, with one broker between them.
type chain struct {
	t      *testing.T
	sw     *hotswap.Switch
	ctx    context.Context
	pm     *parties.Module
	fm     *fiscal.Module
	dm     *products.Module
	im     *inventory.Module
	rm     *receivables.Module
	om     *orders.Module
	bm     *billing.Module
	broker *inprocess.Broker
}

func composeChain(t *testing.T) *chain {
	sw := hotswap.New(memory.NewStore("memory"))
	pm := parties.Compose(sw, nil)
	fm := fiscal.Compose(sw, finfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	dm := products.Compose(sw)
	im := inventory.Compose(sw, iinfra.ProductsCatalog{Catalog: dm.Catalog})
	rm := receivables.Compose(sw, nil)
	om := orders.Compose(sw, oinfra.ProductsCatalog{Catalog: dm.Catalog, Pricing: dm.Pricing}, oinfra.ReceivablesCredit{Exposure: rm.Credit})
	bm := billing.Compose(sw, binfra.FiscalTaxes{Engine: fm.TaxEngine}, binfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
	broker := inprocess.NewBroker()
	broker.Subscribe("inventory", im.Consumer)
	broker.Subscribe("orders", om.Consumer)
	broker.Subscribe("billing", bm.Consumer)
	broker.Subscribe("receivables", rm.Consumer)
	admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "setup", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
	admin.GlobalAdmin = true
	t.Cleanup(func() { _ = sw.Close(context.Background()) })
	return &chain{t: t, sw: sw, ctx: authz.WithContext(context.Background(), admin), pm: pm, fm: fm, dm: dm, im: im, rm: rm, om: om, bm: bm, broker: broker}
}

func (c *chain) ok(err error) {
	c.t.Helper()
	if err != nil {
		c.t.Fatal(err)
	}
}

func (c *chain) deliver() {
	for moved := true; moved; {
		moved = false
		for _, r := range []interface {
			RelayOnce(context.Context) (int, error)
		}{c.om.Relay(c.broker), c.im.Relay(c.broker), c.bm.Relay(c.broker)} {
			n, err := r.RelayOnce(context.Background())
			c.ok(err)
			moved = moved || n > 0
		}
	}
}

func (c *chain) scenario(tag string) {
	t, ctx := c.t, c.ctx
	acme, err := c.pm.Service.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme " + tag,
		Roles: []string{pdomain.RoleInternalOrganization.String()}})
	c.ok(err)
	ana, err := c.pm.Service.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Núñez " + tag})
	c.ok(err)
	// Each scenario has its own company, with a tax number of its own.
	nif := map[string][2]string{"m": {"A58818501", "12345678Z"}, "s": {"B86561412", "00000000T"}}[tag]
	for party, d := range map[string][2]string{acme.ID: {"c0000000-0004-0000-0000-000000000003", nif[0]}, ana.ID: {"c0000000-0004-0000-0000-000000000002", nif[1]}} {
		id, _ := pdomain.ParsePartyID(party)
		_, err := c.pm.Service.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: id, DocumentType: d[0], Country: "ES", Number: d[1], Primary: true})
		c.ok(err)
	}
	// The rates are a catalog of the backend: each one (memory, then SQLite) gets its own.
	_, err = c.fm.Service.CreateRate.Handle(ctx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
		From: vocab.MustDate(2012, 9, 1)})
	c.ok(err)
	_, err = c.fm.Service.RegisterTaxpayer.Handle(ctx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
	c.ok(err)
	series, err := c.bm.Service.OpenSeries.Handle(ctx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
	c.ok(err)
	product := func(sku string, d dapp.DetailsInput) dapp.ProductDTO {
		d.Name = sku + " " + tag
		p, err := c.dm.Service.RegisterProduct.Handle(ctx, dapp.RegisterProduct{Company: acme.ID, SKU: sku, DetailsInput: d})
		c.ok(err)
		return p
	}
	tornillo := product("TOR-M8", dapp.DetailsInput{Kind: "good", UoM: "ea", TaxCode: "G21", BasePrice: "1", ForSale: true, Stocked: true})
	montaje := product("MONT", dapp.DetailsInput{Kind: "service", UoM: "h", TaxCode: "G21", BasePrice: "35", ForSale: true})
	sinIVA := product("RARO", dapp.DetailsInput{Kind: "service", UoM: "h", BasePrice: "10", ForSale: true})
	wh, err := c.im.Service.CreateWarehouse.Handle(ctx, iapp.CreateWarehouse{Company: acme.ID, Code: "AL1", Name: "Central"})
	c.ok(err)
	_, err = c.im.Service.Receive.Handle(ctx, iapp.Receive{Warehouse: wh.ID, Product: tornillo.ID, Quantity: "100", UnitCost: "0.4", Date: vocab.MustDate(2026, 10, 1)})
	c.ok(err)
	limit := "200"
	_, err = c.rm.Service.SetCredit.Handle(ctx, rapp.SetCredit{Seller: acme.ID, Customer: ana.ID, Limit: &limit})
	c.ok(err)

	order := func(lines map[string]string) (odomain.OrderID, error) {
		o, err := c.om.Service.DraftOrder.Handle(ctx, oapp.DraftOrder{Company: acme.ID, Customer: ana.ID, Date: vocab.MustDate(2026, 10, 4), Warehouse: wh.ID})
		c.ok(err)
		id, _ := odomain.ParseOrderID(o.ID)
		for _, p := range []string{tornillo.ID, montaje.ID, sinIVA.ID} {
			if q, ok := lines[p]; ok {
				if _, err := c.om.Service.AddLine.Handle(ctx, oapp.AddLine{ID: id, Product: p, Quantity: q}); err != nil {
					return id, err
				}
			}
		}
		_, err = c.om.Service.Confirm.Handle(ctx, oapp.ConfirmOrder{ID: id})
		return id, err
	}

	// A product without a tax code cannot be sold: its delivery could not be invoiced.
	if _, err := order(map[string]string{sinIVA.ID: "1"}); !isViolation(err, "orders.no_tax_code") {
		t.Fatalf("no tax code: %v", err)
	}

	// Order, delivery note and, from it, the draft invoice.
	oid, err := order(map[string]string{tornillo.ID: "60", montaje.ID: "1"})
	c.ok(err)
	c.deliver()
	note, err := c.om.Service.Deliver.Handle(ctx, oapp.Deliver{ID: oid, Date: vocab.MustDate(2026, 10, 5)})
	c.ok(err)
	c.deliver()
	drafts, err := c.bm.Service.SearchInvoices.Handle(ctx, bapp.SearchInvoices{Seller: acme.ID, Status: "draft"})
	c.ok(err)
	if len(drafts.Items) != 1 {
		t.Fatalf("one draft per delivery note: %+v", drafts.Items)
	}
	inv := drafts.Items[0]
	if inv.SourceType != "orders.delivery" || inv.SourceID != note.ID || inv.SourceRef != note.Number || inv.Customer != ana.ID || inv.OperationDate != "2026-10-05" ||
		inv.Description != "Albarán "+note.Number+" (pedido "+note.OrderNumber+")" || len(inv.Lines) != 2 || inv.Lines[0].Quantity != "60" ||
		inv.Lines[0].UnitPrice != "1" || inv.Lines[0].TaxCode != "G21" || inv.Net != "95.00" || inv.Net != note.Total {
		t.Fatalf("draft from the delivery note: %+v", inv)
	}
	iid, _ := bdomain.ParseInvoiceID(inv.ID)
	if _, err := c.bm.Service.Discard.Handle(ctx, bapp.DiscardInvoice{ID: iid}); !isViolation(err, "billing.sourced_draft") {
		t.Fatalf("a draft from a delivery note is not discarded: %v", err)
	}
	open, err := c.om.Service.SearchDeliveries.Handle(ctx, oapp.SearchDeliveries{Company: acme.ID, Uninvoiced: true})
	if err != nil || len(open.Items) != 1 {
		t.Fatalf("delivery notes to invoice: %+v %v", open.Items, err)
	}

	// Issued: the delivery note knows its invoice and Receivables what the customer owes.
	inv, err = c.bm.Service.Issue.Handle(ctx, bapp.IssueInvoice{ID: iid, Series: series.ID, Date: vocab.MustDate(2026, 10, 6)})
	c.ok(err)
	c.deliver()
	did, _ := odomain.ParseDeliveryID(note.ID)
	note, err = c.om.Service.GetDelivery.Handle(ctx, oapp.GetDelivery{ID: did})
	if err != nil || note.Invoice != inv.ID || note.InvoiceNo != inv.Number || inv.Taxes.Total != "114.95" {
		t.Fatalf("invoiced delivery note: %+v / %+v %v", note, inv, err)
	}
	if open, err := c.om.Service.SearchDeliveries.Handle(ctx, oapp.SearchDeliveries{Company: acme.ID, Uninvoiced: true}); err != nil || len(open.Items) != 0 {
		t.Fatalf("nothing left to invoice: %+v %v", open.Items, err)
	}
	exp, err := c.rm.Credit.Exposure(context.Background(), acme.ID, ana.ID, "2026-10-06")
	if err != nil || exp.Open != "114.95" || exp.Available != "85.05" {
		t.Fatalf("exposure: %+v %v", exp, err)
	}

	// The next order counts what is already owed: 110.00 does not fit in the 85.05 available.
	if _, err := order(map[string]string{tornillo.ID: "40", montaje.ID: "2"}); !isViolation(err, "orders.credit_exceeded") {
		t.Fatalf("credit with the open invoice: %v", err)
	}
	if _, err := order(map[string]string{tornillo.ID: "40", montaje.ID: "1"}); err != nil {
		t.Fatalf("75.00 fits: %v", err)
	}
}

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func TestOrders_DeliveryNotesAreInvoiced_MemoryThenSQLite(t *testing.T) {
	c := composeChain(t)
	c.scenario("m")

	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "host.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
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
	c.scenario("s")
}

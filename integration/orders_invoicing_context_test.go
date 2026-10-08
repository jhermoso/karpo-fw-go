package integration

import (
	"context"
	"errors"
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
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestOrdersInvoicing runs the sales cycle on every engine with seven contexts migrated in the
// same database (the Billing and Orders migrations that add columns included): an order, its
// delivery note, the invoice Billing drafts from it through its SQL inbox with the source of the
// invoice round trip, the issued invoice recorded on the delivery note and the receivable opened.
func TestOrdersInvoicing(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			oinfra.DropAll(ctx, db)
			iinfra.DropAll(ctx, db)
			dinfra.DropAll(ctx, db)
			rinfra.DropAll(ctx, db)
			binfra.DropAll(ctx, db)
			finfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), dinfra.Migrations(), iinfra.Migrations(),
				rinfra.Migrations(), oinfra.Migrations(), binfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
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
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			deliver := func() {
				for moved := true; moved; {
					moved = false
					for _, r := range []interface {
						RelayOnce(context.Context) (int, error)
					}{om.Relay(broker), im.Relay(broker), bm.Relay(broker)} {
						n, err := r.RelayOnce(ctx)
						must(err)
						moved = moved || n > 0
					}
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme, S.A.", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			ana, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Muñoz"})
			must(err)
			for party, d := range map[string][2]string{acme.ID: {"c0000000-0004-0000-0000-000000000003", "A58818501"}, ana.ID: {"c0000000-0004-0000-0000-000000000002", "12345678Z"}} {
				id, _ := pdomain.ParsePartyID(party)
				_, err := pm.Service.AddIdentification.Handle(actx, papp.AddIdentification{PartyID: id, DocumentType: d[0], Country: "ES", Number: d[1], Primary: true})
				must(err)
			}
			_, err = fm.Service.CreateRate.Handle(actx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
				From: vocab.MustDate(2012, 9, 1)})
			must(err)
			_, err = fm.Service.RegisterTaxpayer.Handle(actx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
			must(err)
			series, err := bm.Service.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
			must(err)
			tornillo, err := dm.Service.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme.ID, SKU: "TOR-M8", DetailsInput: dapp.DetailsInput{
				Name: "Tornillo M8", Kind: "good", UoM: "ea", TaxCode: "G21", BasePrice: "0.3333", ForSale: true, Stocked: true}})
			must(err)
			wh, err := im.Service.CreateWarehouse.Handle(actx, iapp.CreateWarehouse{Company: acme.ID, Code: "AL1", Name: "Central"})
			must(err)
			_, err = im.Service.Receive.Handle(actx, iapp.Receive{Warehouse: wh.ID, Product: tornillo.ID, Quantity: "100", UnitCost: "0.1", Date: vocab.MustDate(2026, 10, 1)})
			must(err)

			o, err := om.Service.DraftOrder.Handle(actx, oapp.DraftOrder{Company: acme.ID, Customer: ana.ID, Date: vocab.MustDate(2026, 10, 4), Warehouse: wh.ID})
			must(err)
			oid, _ := odomain.ParseOrderID(o.ID)
			_, err = om.Service.AddLine.Handle(actx, oapp.AddLine{ID: oid, Product: tornillo.ID, Quantity: "100"})
			must(err)
			_, err = om.Service.Confirm.Handle(actx, oapp.ConfirmOrder{ID: oid})
			must(err)
			deliver()
			note, err := om.Service.Deliver.Handle(actx, oapp.Deliver{ID: oid, Date: vocab.MustDate(2026, 10, 5)})
			must(err)
			deliver()

			drafts, err := bm.Service.SearchInvoices.Handle(actx, bapp.SearchInvoices{Seller: acme.ID, Status: "draft"})
			must(err)
			// 100 × 0.3333 = 33.33.
			if len(drafts.Items) != 1 || drafts.Items[0].SourceType != "orders.delivery" || drafts.Items[0].SourceID != note.ID ||
				drafts.Items[0].SourceRef != "ALB-2026-000001" || drafts.Items[0].OperationDate != "2026-10-05" || drafts.Items[0].Net != "33.33" ||
				drafts.Items[0].Lines[0].UnitPrice != "0.3333" || drafts.Items[0].Lines[0].Description != "TOR-M8 Tornillo M8" {
				t.Fatalf("draft from the delivery note: %+v", drafts.Items)
			}
			iid, _ := bdomain.ParseInvoiceID(drafts.Items[0].ID)
			if _, err := bm.Service.Discard.Handle(actx, bapp.DiscardInvoice{ID: iid}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("discard a sourced draft: %v", err)
			}
			inv, err := bm.Service.Issue.Handle(actx, bapp.IssueInvoice{ID: iid, Series: series.ID, Date: vocab.MustDate(2026, 10, 6)})
			must(err)
			deliver()
			did, _ := odomain.ParseDeliveryID(note.ID)
			note, err = om.Service.GetDelivery.Handle(actx, oapp.GetDelivery{ID: did})
			if err != nil || note.Invoice != inv.ID || note.InvoiceNo != "FA-2026-000001" || inv.SourceRef != "ALB-2026-000001" || inv.Taxes.Total != "40.33" {
				t.Fatalf("invoiced delivery note: %+v / %+v %v", note, inv, err)
			}
			pending, err := om.Service.SearchDeliveries.Handle(actx, oapp.SearchDeliveries{Company: acme.ID, Uninvoiced: true})
			if err != nil || len(pending.Items) != 0 {
				t.Fatalf("to invoice: %+v %v", pending.Items, err)
			}
			exp, err := rm.Credit.Exposure(ctx, acme.ID, ana.ID, "2026-10-06")
			if err != nil || exp.Open != "40.33" {
				t.Fatalf("receivable: %+v %v", exp, err)
			}
		})
	}
}

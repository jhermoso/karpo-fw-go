package integration

import (
	"context"
	"errors"
	"testing"
	"time"

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

// TestOrdersInvoicing ejecuta el ciclo de venta en cada motor con siete contextos migrados en la
// misma base de datos (incluidas las migraciones de Billing y Orders que añaden columnas): un
// pedido, su albarán, la factura que Billing prepara en borrador a partir de él a través de su inbox
// SQL con ida y vuelta del origen de la factura, la factura emitida anotada en el albarán y la
// cuenta por cobrar abierta. Al final, D-149: un pedido con dos descuentos encadenados y cantidad
// grande vale lo mismo en el pedido, en el albarán y en la factura del cierre, pasando por las
// columnas nuevas (migración 8 de Orders y 13 de Billing).
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
			pm := parties.Compose(sw)
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
				// El domicilio fiscal: sin él no se emite una factura completa (D-147). Vale desde
				// antes de cualquier hora que finja la prueba.
				since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				_, err = pm.Service.AddContact.Handle(actx, papp.AddContact{PartyID: id, Kind: "postal", Purposes: []string{"default"}, From: &since,
					Address: &papp.AddressDTO{Line1: "Calle Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
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

			// Varios albaranes en una factura (D-146), con las migraciones 10 y 11 de Billing y la 7 de
			// Orders: dos borradores de Ana unidos a mano y emitidos, que facturan sus dos albaranes por
			// la tabla de orígenes; y Bea, mensual, cuyos tres albaranes esperan en la tabla de pendientes
			// hasta un cierre que prepara un único borrador, y un segundo cierre que no prepara nada.
			montaje, err := dm.Service.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme.ID, SKU: "MONT", DetailsInput: dapp.DetailsInput{
				Name: "Montaje", Kind: "service", UoM: "h", TaxCode: "G21", BasePrice: "35", ForSale: true}})
			must(err)
			bea, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Bea", FirstSurname: "Mensual"})
			must(err)
			terms, err := om.Service.SetTerms.Handle(actx, oapp.SetTerms{Company: acme.ID, Customer: bea.ID, BillingPeriodicity: "monthly"})
			if err != nil || terms.BillingPeriodicity != "monthly" {
				t.Fatalf("monthly terms: %+v %v", terms, err)
			}
			delivered := func(customer string, day int) oapp.DeliveryDTO {
				t.Helper()
				o, err := om.Service.DraftOrder.Handle(actx, oapp.DraftOrder{Company: acme.ID, Customer: customer, Date: vocab.MustDate(2026, 10, 1)})
				must(err)
				id, _ := odomain.ParseOrderID(o.ID)
				_, err = om.Service.AddLine.Handle(actx, oapp.AddLine{ID: id, Product: montaje.ID, Quantity: "1"})
				must(err)
				_, err = om.Service.Confirm.Handle(actx, oapp.ConfirmOrder{ID: id})
				must(err)
				deliver()
				note, err := om.Service.Deliver.Handle(actx, oapp.Deliver{ID: id, Date: vocab.MustDate(2026, 10, day)})
				must(err)
				deliver()
				return note
			}
			draftsOf := func(customer string) []bapp.InvoiceDTO {
				t.Helper()
				page, err := bm.Service.SearchInvoices.Handle(actx, bapp.SearchInvoices{Seller: acme.ID, Customer: customer, Status: "draft"})
				must(err)
				return page.Items
			}
			a1, a2 := delivered(ana.ID, 7), delivered(ana.ID, 8)
			two := draftsOf(ana.ID)
			if len(two) != 2 {
				t.Fatalf("one draft per delivery note: %+v", two)
			}
			merged, err := bm.Service.MergeDrafts.Handle(actx, bapp.MergeDrafts{Invoices: []string{two[0].ID, two[1].ID}})
			must(err)
			if len(merged.Sources) != 2 || merged.Net != "70.00" || len(draftsOf(ana.ID)) != 1 {
				t.Fatalf("merged draft: %+v", merged)
			}
			mid, _ := bdomain.ParseInvoiceID(merged.ID)
			got, err := bm.Service.GetInvoice.Handle(actx, bapp.GetInvoice{ID: mid})
			if err != nil || len(got.Sources) != 2 || got.Sources[0].ID != merged.Sources[0].ID || got.Sources[1].Ref != merged.Sources[1].Ref {
				t.Fatalf("sources round trip: %+v %v", got, err)
			}
			inv2, err := bm.Service.Issue.Handle(actx, bapp.IssueInvoice{ID: mid, Series: series.ID, Date: vocab.MustDate(2026, 10, 9)})
			must(err)
			deliver()
			for _, n := range []oapp.DeliveryDTO{a1, a2} {
				id, _ := odomain.ParseDeliveryID(n.ID)
				note, err := om.Service.GetDelivery.Handle(actx, oapp.GetDelivery{ID: id})
				if err != nil || note.Invoice != inv2.ID {
					t.Fatalf("delivery note %s invoiced: %+v %v", n.Number, note, err)
				}
			}
			if exp, err := rm.Credit.Exposure(ctx, acme.ID, ana.ID, "2026-10-09"); err != nil || exp.Open != "125.03" {
				t.Fatalf("receivable after the merged invoice: %+v %v", exp, err)
			}

			delivered(bea.ID, 2)
			delivered(bea.ID, 15)
			delivered(bea.ID, 28)
			if d := draftsOf(bea.ID); len(d) != 0 {
				t.Fatalf("no draft per delivery note for a monthly customer: %+v", d)
			}
			res, err := bm.Service.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme.ID, UpTo: vocab.MustDate(2026, 10, 31)})
			must(err)
			closed := res.Drafts
			if len(closed) != 1 || closed[0].Customer != bea.ID || len(closed[0].Sources) != 3 || closed[0].Net != "105.00" || closed[0].OperationDate != "2026-10-28" {
				t.Fatalf("the draft of the period: %+v", closed)
			}
			again, err := bm.Service.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme.ID, UpTo: vocab.MustDate(2026, 10, 31)})
			if err != nil || len(again.Drafts) != 0 || len(again.Failed) != 0 || len(draftsOf(bea.ID)) != 1 {
				t.Fatalf("a second close does not repeat: %+v %v", again, err)
			}

			// D-149: 1000 h a 1 € con un 5 % del precio especial y un 2,5 % de Carla, mensual. El pedido
			// guarda el descuento encadenado (ord_order_lines.chained_discount), el albarán el precio y
			// ese descuento (ord_delivery_lines.unit_price y discount), el pendiente su descuento
			// (bil_pending_delivery_lines.discount) y la factura del cierre vale lo mismo: 926,25.
			carla, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Carla", FirstSurname: "Encadenada"})
			must(err)
			_, err = om.Service.SetTerms.Handle(actx, oapp.SetTerms{Company: acme.ID, Customer: carla.ID, Discount: "2.5", BillingPeriodicity: "monthly"})
			must(err)
			_, err = dm.Service.SetSpecialPrice.Handle(actx, dapp.SetSpecialPrice{Company: acme.ID, Customer: carla.ID, Product: montaje.ID, UnitPrice: "1",
				Discount: "5", From: vocab.MustDate(2026, 1, 1)})
			must(err)
			co, err := om.Service.DraftOrder.Handle(actx, oapp.DraftOrder{Company: acme.ID, Customer: carla.ID, Date: vocab.MustDate(2026, 10, 1)})
			must(err)
			coid, _ := odomain.ParseOrderID(co.ID)
			_, err = om.Service.AddLine.Handle(actx, oapp.AddLine{ID: coid, Product: montaje.ID, Quantity: "1000"})
			must(err)
			co, err = om.Service.GetOrder.Handle(actx, oapp.GetOrder{ID: coid})
			if l := co.Lines[0]; err != nil || l.UnitPrice != "1.0000" || l.ChainedDiscount != "7.3750" || l.Amount != "926.25" || co.Total != "926.25" {
				t.Fatalf("order with a chained discount: %+v %v", co, err)
			}
			_, err = om.Service.Confirm.Handle(actx, oapp.ConfirmOrder{ID: coid})
			must(err)
			deliver()
			cnote, err := om.Service.Deliver.Handle(actx, oapp.Deliver{ID: coid, Date: vocab.MustDate(2026, 10, 20)})
			must(err)
			deliver()
			cdid, _ := odomain.ParseDeliveryID(cnote.ID)
			cnote, err = om.Service.GetDelivery.Handle(actx, oapp.GetDelivery{ID: cdid})
			if l := cnote.Lines[0]; err != nil || l.UnitPrice != "1.0000" || l.Discount != "7.375" || l.Amount != "926.25" || cnote.Total != "926.25" {
				t.Fatalf("delivery note with a chained discount: %+v %v", cnote, err)
			}
			cpending, err := bm.Service.SearchPendingDeliveries.Handle(actx, bapp.SearchPendingDeliveries{Seller: acme.ID, Customer: carla.ID})
			if err != nil || len(cpending.Items) != 1 || cpending.Items[0].Net != "926.25" {
				t.Fatalf("pending delivery note with a chained discount: %+v %v", cpending, err)
			}
			cres, err := bm.Service.ClosePeriod.Handle(actx, bapp.ClosePeriod{Seller: acme.ID, Customer: carla.ID, UpTo: vocab.MustDate(2026, 10, 31)})
			if err != nil || len(cres.Drafts) != 1 || cres.Drafts[0].Net != "926.25" || cres.Drafts[0].Lines[0].UnitPrice != "1" ||
				cres.Drafts[0].Lines[0].Discount != "7.375" {
				t.Fatalf("the invoice of the delivery note with a chained discount: %+v %v", cres, err)
			}
		})
	}
}

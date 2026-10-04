package integration

import (
	"context"
	"errors"
	"testing"

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
	ddomain "github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestOrdersContext runs Orders with Inventory, Products, Receivables and Parties on every
// engine: customer terms round trip, an order priced by Products, the credit check of
// Receivables, confirmation with its number, the stock held, issued and released by Inventory
// through the SQL inboxes, two delivery notes, the order and delivery round trips and a
// cancellation.
func TestOrdersContext(t *testing.T) {
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
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), dinfra.Migrations(), iinfra.Migrations(), rinfra.Migrations(),
				oinfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
			pm := parties.Compose(sw, nil)
			dm := products.Compose(sw)
			im := inventory.Compose(sw, iinfra.ProductsCatalog{Catalog: dm.Catalog})
			rm := receivables.Compose(sw, nil)
			om := orders.Compose(sw, oinfra.ProductsCatalog{Catalog: dm.Catalog, Pricing: dm.Pricing}, oinfra.ReceivablesCredit{Exposure: rm.Credit})
			broker := inprocess.NewBroker()
			broker.Subscribe("inventory", im.Consumer)
			broker.Subscribe("orders", om.Consumer)
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
					}{om.Relay(broker), im.Relay(broker)} {
						n, err := r.RelayOnce(ctx)
						must(err)
						moved = moved || n > 0
					}
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := om.Service

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme, S.A.", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			ana, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Muñoz"})
			must(err)
			tornillo, err := dm.Service.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme.ID, SKU: "TOR-M8", DetailsInput: dapp.DetailsInput{
				Name: "Tornillo M8", Kind: "good", UoM: "ea", TaxCode: "G21", BasePrice: "0.125", ForSale: true, Stocked: true}})
			must(err)
			montaje, err := dm.Service.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme.ID, SKU: "MONT", DetailsInput: dapp.DetailsInput{
				Name: "Montaje", Kind: "service", UoM: "h", TaxCode: "G21", BasePrice: "35", ForSale: true}})
			must(err)
			list, err := dm.Service.CreatePriceList.Handle(actx, dapp.CreatePriceList{Company: acme.ID, Code: "MAYOR", Name: "Mayoristas"})
			must(err)
			lid, _ := ddomain.ParsePriceListID(list.ID)
			_, err = dm.Service.SetPrice.Handle(actx, dapp.SetPrice{ID: lid, Product: tornillo.ID, MinQuantity: "1000", UnitPrice: "0.08", Discount: "5",
				From: vocab.MustDate(2026, 1, 1)})
			must(err)
			wh, err := im.Service.CreateWarehouse.Handle(actx, iapp.CreateWarehouse{Company: acme.ID, Code: "AL1", Name: "Central"})
			must(err)
			_, err = im.Service.Receive.Handle(actx, iapp.Receive{Warehouse: wh.ID, Product: tornillo.ID, Quantity: "600", UnitCost: "0.04", Date: vocab.MustDate(2026, 10, 1)})
			must(err)
			stock := func() (string, string) {
				s, err := im.Availability.Stock(ctx, acme.ID, tornillo.ID, wh.ID)
				must(err)
				return s.OnHand, s.Reserved
			}

			terms, err := svc.SetTerms.Handle(actx, oapp.SetTerms{Company: acme.ID, Customer: ana.ID, PriceList: list.ID, Discount: "10"})
			must(err)
			terms, err = svc.SetTerms.Handle(actx, oapp.SetTerms{Company: acme.ID, Customer: ana.ID, PriceList: list.ID, Discount: "10", BlockDelivery: false})
			if err != nil || terms.Version != 2 || terms.PriceList != list.ID || terms.Discount != "10.00" {
				t.Fatalf("terms round trip: %+v %v", terms, err)
			}

			o, err := svc.DraftOrder.Handle(actx, oapp.DraftOrder{Company: acme.ID, Customer: ana.ID, Date: vocab.MustDate(2026, 10, 4), Warehouse: wh.ID,
				Reference: "Su pedido 77", Notes: "Entregar por la mañana"})
			must(err)
			oid, _ := odomain.ParseOrderID(o.ID)
			_, err = svc.AddLine.Handle(actx, oapp.AddLine{ID: oid, Product: tornillo.ID, Quantity: "1000"})
			must(err)
			_, err = svc.AddLine.Handle(actx, oapp.AddLine{ID: oid, Product: montaje.ID, Quantity: "2.5"})
			must(err)
			limit := "50"
			_, err = rm.Service.SetCredit.Handle(actx, rapp.SetCredit{Seller: acme.ID, Customer: ana.ID, Limit: &limit})
			must(err)
			if _, err := svc.Confirm.Handle(actx, oapp.ConfirmOrder{ID: oid}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("credit exceeded: %v", err)
			}
			limit = "5000"
			_, err = rm.Service.SetCredit.Handle(actx, rapp.SetCredit{Seller: acme.ID, Customer: ana.ID, Limit: &limit})
			must(err)
			_, err = svc.Confirm.Handle(actx, oapp.ConfirmOrder{ID: oid})
			must(err)
			deliver()
			o, err = svc.GetOrder.Handle(actx, oapp.GetOrder{ID: oid})
			// 0.08 × 0.95 × 0.90 = 0.0684 → 68.40; 35 × 0.90 × 2.5 = 78.75.
			if err != nil || o.Number != "PED-2026-000001" || o.Status != "confirmed" || o.Reference != "Su pedido 77" || o.Notes != "Entregar por la mañana" ||
				o.Warehouse != wh.ID || o.PriceList != list.ID || len(o.Lines) != 2 || o.Lines[0].NetPrice != "0.0684" || o.Lines[0].Amount != "68.40" ||
				o.Lines[0].Reserved != "600" || o.Lines[0].Short != "400" || !o.Lines[0].Stocked || o.Lines[1].Quantity != "2.5" || o.Lines[1].Amount != "78.75" ||
				o.Total != "147.15" {
				t.Fatalf("order round trip: %+v %v", o, err)
			}
			if on, res := stock(); on != "600" || res != "600" {
				t.Fatalf("held: %s %s", on, res)
			}
			if _, err := svc.Deliver.Handle(actx, oapp.Deliver{ID: oid, Lines: []oapp.PickInput{{Line: 1, Quantity: "601"}}}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("more than held: %v", err)
			}
			note, err := svc.Deliver.Handle(actx, oapp.Deliver{ID: oid, Date: vocab.MustDate(2026, 10, 5)})
			must(err)
			deliver()
			did, _ := odomain.ParseDeliveryID(note.ID)
			note, err = svc.GetDelivery.Handle(actx, oapp.GetDelivery{ID: did})
			// 600 × 0.0684 = 41.04, plus 78.75.
			if err != nil || note.Number != "ALB-2026-000001" || note.OrderNumber != "PED-2026-000001" || note.Date != "2026-10-05" || len(note.Lines) != 2 ||
				note.Lines[0].Quantity != "600" || note.Lines[0].Amount != "41.04" || note.Lines[1].Quantity != "2.5" || note.Total != "119.79" {
				t.Fatalf("delivery round trip: %+v %v", note, err)
			}
			if on, res := stock(); on != "0" || res != "0" {
				t.Fatalf("issued: %s %s", on, res)
			}

			_, err = im.Service.Receive.Handle(actx, iapp.Receive{Warehouse: wh.ID, Product: tornillo.ID, Quantity: "250", UnitCost: "0.05", Date: vocab.MustDate(2026, 10, 6)})
			must(err)
			_, err = svc.RequestStock.Handle(actx, oapp.RequestStock{ID: oid})
			must(err)
			deliver()
			if _, res := stock(); res != "250" {
				t.Fatalf("held later: %s", res)
			}
			o, err = svc.Close.Handle(actx, oapp.EndOrder{ID: oid, Reason: "El cliente renuncia al resto"})
			if err != nil || o.Status != "closed" || o.CloseReason != "El cliente renuncia al resto" || o.Lines[0].Reserved != "0" {
				t.Fatalf("close: %+v %v", o, err)
			}
			deliver()
			if on, res := stock(); on != "250" || res != "0" {
				t.Fatalf("released: %s %s", on, res)
			}
			card, err := im.Service.Ledger.Handle(actx, iapp.GetLedger{Company: acme.ID, Warehouse: wh.ID, Product: tornillo.ID})
			if err != nil || len(card.Items) != 3 || card.Items[1].Kind != "issue" || card.Items[1].Note != "Albarán ALB-2026-000001" {
				t.Fatalf("stock card: %+v %v", card.Items, err)
			}
			closed, err := svc.SearchOrders.Handle(actx, oapp.SearchOrders{Company: acme.ID, Customer: ana.ID, Status: "closed"})
			if err != nil || len(closed.Items) != 1 {
				t.Fatalf("search: %+v %v", closed.Items, err)
			}
			notes, err := svc.SearchDeliveries.Handle(actx, oapp.SearchDeliveries{Company: acme.ID, Order: o.ID})
			if err != nil || len(notes.Items) != 1 {
				t.Fatalf("delivery notes: %+v %v", notes.Items, err)
			}
		})
	}
}

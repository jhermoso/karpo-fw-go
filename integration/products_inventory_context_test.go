package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/inventory"
	iapp "github.com/jhermoso/karpo-fw-go/contexts/inventory/application"
	idomain "github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	iinfra "github.com/jhermoso/karpo-fw-go/contexts/inventory/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/products"
	dapp "github.com/jhermoso/karpo-fw-go/contexts/products/application"
	ddomain "github.com/jhermoso/karpo-fw-go/contexts/products/domain"
	dinfra "github.com/jhermoso/karpo-fw-go/contexts/products/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestProductsAndInventoryContexts runs Products and Inventory with Parties on every engine.
// Products: categories, a product with barcodes, components and suppliers round trip, search by
// barcode (EXISTS on the child table) and by name, unique SKU, price list with tiers and the
// quote. Inventory: receipts with weighted average cost, idempotent by source, issue, reservation
// and its issue, transfer, count, the stock card in posting order, valuation and availability.
func TestProductsAndInventoryContexts(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			iinfra.DropAll(ctx, db)
			dinfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), dinfra.Migrations(), iinfra.Migrations()})
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
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			ps, is := dm.Service, im.Service

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme, S.A.", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			proveedor, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Tornillería, S.L."})
			must(err)

			// Products.
			cat, err := ps.CreateCategory.Handle(actx, dapp.CreateCategory{Company: acme.ID, Code: "torn", Name: "Tornillería"})
			must(err)
			tuerca, err := ps.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme.ID, SKU: "TUE-M8", DetailsInput: dapp.DetailsInput{
				Name: "Tuerca M8", Kind: "good", UoM: "ea", BasePrice: "0.05", Stocked: true}})
			must(err)
			tornillo, err := ps.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme.ID, SKU: "tor-m8", DetailsInput: dapp.DetailsInput{
				Name: "Tornillo M8 zincado", Description: "DIN 933", Kind: "good", UoM: "ea", Category: cat.ID, TaxCode: "G21", ExpenseCategory: "goods",
				BasePrice: "0.125", StandardCost: "0.04", ForSale: true, ForPurchase: true, Stocked: true, Tracking: "lot",
				Barcodes:   []dapp.BarcodeInput{{Type: "ean-13", Value: "8412345678905"}, {Type: "internal", Value: "T-M8"}},
				Components: []dapp.ComponentInput{{Product: tuerca.ID, Quantity: "1.5"}},
				Suppliers:  []dapp.SupplierInput{{Supplier: proveedor.ID, Code: "M8-ZN", LeadDays: 7, Preferred: true}}}})
			must(err)
			if _, err := ps.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme.ID, SKU: "TOR-M8", DetailsInput: dapp.DetailsInput{
				Name: "Otro", Kind: "good", UoM: "ea"}}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("duplicate SKU: %v", err)
			}
			if _, err := ps.RegisterProduct.Handle(actx, dapp.RegisterProduct{Company: acme.ID, SKU: "X", DetailsInput: dapp.DetailsInput{
				Name: "Otro", Kind: "good", UoM: "ea", Barcodes: []dapp.BarcodeInput{{Type: "internal", Value: "T-M8"}}}}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("duplicate barcode: %v", err)
			}
			tid, _ := ddomain.ParseProductID(tornillo.ID)
			got, err := ps.GetProduct.Handle(actx, dapp.GetProduct{ID: tid})
			if err != nil || got.SKU != "TOR-M8" || got.Description != "DIN 933" || got.Category != cat.ID || got.BasePrice != "0.1250" || got.Tracking != "lot" ||
				len(got.Barcodes) != 2 || got.Barcodes[1].Value != "T-M8" || len(got.Components) != 1 || got.Components[0].Quantity != "1.5" ||
				len(got.Suppliers) != 1 || !got.Suppliers[0].Preferred || got.Suppliers[0].LeadDays != 7 {
				t.Fatalf("product round trip: %+v %v", got, err)
			}
			for query, want := range map[dapp.SearchProducts]int{{Company: acme.ID, Barcode: "8412345678905"}: 1, {Company: acme.ID, Text: "ZINCADO"}: 1,
				{Company: acme.ID, Text: "tue-m8"}: 1, {Company: acme.ID, Category: cat.ID}: 1, {Company: acme.ID, Kind: "good"}: 2, {Company: acme.ID, Barcode: "nope"}: 0} {
				page, err := ps.SearchProducts.Handle(actx, query)
				if err != nil || len(page.Items) != want {
					t.Fatalf("search %+v: %d %v", query, len(page.Items), err)
				}
			}
			pl, err := ps.CreatePriceList.Handle(actx, dapp.CreatePriceList{Company: acme.ID, Code: "mayor", Name: "Mayoristas"})
			must(err)
			plid, _ := ddomain.ParsePriceListID(pl.ID)
			_, err = ps.SetPrice.Handle(actx, dapp.SetPrice{ID: plid, Product: tornillo.ID, UnitPrice: "0.10", From: vocab.MustDate(2026, 1, 1), To: vocab.MustDate(2026, 12, 31)})
			must(err)
			_, err = ps.SetPrice.Handle(actx, dapp.SetPrice{ID: plid, Product: tornillo.ID, MinQuantity: "1000", UnitPrice: "0.08", Discount: "5", From: vocab.MustDate(2026, 1, 1)})
			must(err)
			if _, err := ps.SetPrice.Handle(actx, dapp.SetPrice{ID: plid, Product: tornillo.ID, UnitPrice: "0.09", From: vocab.MustDate(2026, 6, 1)}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("overlap: %v", err)
			}
			pl, err = ps.GetPriceList.Handle(actx, dapp.GetPriceList{ID: plid})
			if err != nil || len(pl.Lines) != 2 || pl.Lines[0].To != "2026-12-31" || pl.Lines[1].MinQuantity != "1000" || pl.Lines[1].To != "" {
				t.Fatalf("price list round trip: %+v %v", pl, err)
			}
			q, err := dm.Pricing.Quote(ctx, acme.ID, tornillo.ID, pl.ID, "2500", "2026-10-04")
			if err != nil || q.Net != "0.0760" || q.Source != "list" {
				t.Fatalf("quote: %+v %v", q, err)
			}

			// Inventory.
			al1, err := is.CreateWarehouse.Handle(actx, iapp.CreateWarehouse{Company: acme.ID, Code: "al1", Name: "Central"})
			must(err)
			al2, err := is.CreateWarehouse.Handle(actx, iapp.CreateWarehouse{Company: acme.ID, Code: "al2", Name: "Tienda"})
			must(err)
			if _, err := is.CreateWarehouse.Handle(actx, iapp.CreateWarehouse{Company: acme.ID, Code: "AL1", Name: "Otro"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("duplicate warehouse: %v", err)
			}
			if _, err := is.Receive.Handle(actx, iapp.Receive{Warehouse: al1.ID, Product: tornillo.ID, Quantity: "100", UnitCost: "2"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("lot required: %v", err)
			}
			r1 := iapp.Receive{Warehouse: al1.ID, Product: tornillo.ID, Quantity: "100", UnitCost: "2", Date: vocab.MustDate(2026, 10, 1), Lot: "L-1",
				SourceType: "purchases.receipt", SourceID: "ALB-1"}
			a, err := is.Receive.Handle(actx, r1)
			must(err)
			b, err := is.Receive.Handle(actx, r1)
			if err != nil || a.ID != b.ID {
				t.Fatalf("idempotent receipt: %v", err)
			}
			_, err = is.Receive.Handle(actx, iapp.Receive{Warehouse: al1.ID, Product: tornillo.ID, Quantity: "300", UnitCost: "3", Date: vocab.MustDate(2026, 10, 1), Lot: "L-2"})
			must(err)
			out, err := is.Issue.Handle(actx, iapp.Issue{Warehouse: al1.ID, Product: tornillo.ID, Quantity: "150.5", Date: vocab.MustDate(2026, 10, 2), Lot: "L-1"})
			if err != nil || out.UnitCost != "2.7500" || out.Value != "-413.88" || out.Balance != "249.5" {
				t.Fatalf("issue: %+v %v", out, err)
			}
			if _, err := is.Issue.Handle(actx, iapp.Issue{Warehouse: al1.ID, Product: tornillo.ID, Quantity: "250", Lot: "L-1"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("insufficient stock: %v", err)
			}
			res, err := is.Reserve.Handle(actx, iapp.Reserve{Warehouse: al1.ID, Product: tornillo.ID, Quantity: "200", SourceType: "orders.sales-order", SourceID: "SO-1"})
			must(err)
			if _, err := is.Issue.Handle(actx, iapp.Issue{Warehouse: al1.ID, Product: tornillo.ID, Quantity: "50", Lot: "L-1"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("reserved stock is not taken: %v", err)
			}
			_, err = is.Issue.Handle(actx, iapp.Issue{Warehouse: al1.ID, Product: tornillo.ID, Quantity: "120", Lot: "L-2", Reservation: res.ID, Date: vocab.MustDate(2026, 10, 2)})
			must(err)
			rid, _ := idomain.ParseReservationID(res.ID)
			res, err = is.Release.Handle(actx, iapp.Release{ID: rid})
			if err != nil || res.Open != "0" {
				t.Fatalf("release: %+v %v", res, err)
			}
			moves, err := is.Transfer.Handle(actx, iapp.Transfer{From: al1.ID, To: al2.ID, Product: tornillo.ID, Quantity: "29.5", Date: vocab.MustDate(2026, 10, 3), Lot: "L-2"})
			if err != nil || len(moves) != 2 || moves[1].UnitCost != "2.7500" {
				t.Fatalf("transfer: %+v %v", moves, err)
			}
			lvl, err := is.Adjust.Handle(actx, iapp.Adjust{Warehouse: al1.ID, Product: tornillo.ID, Counted: "98", Date: vocab.MustDate(2026, 10, 4), Reason: "Recuento"})
			if err != nil || lvl.OnHand != "98" || lvl.Reserved != "0" || lvl.AverageCost != "2.7500" || lvl.SKU != "TOR-M8" {
				t.Fatalf("count: %+v %v", lvl, err)
			}
			card, err := is.Ledger.Handle(actx, iapp.GetLedger{Company: acme.ID, Warehouse: al1.ID, Product: tornillo.ID})
			must(err)
			if len(card.Items) != 6 || card.Items[0].Lot != "L-1" || card.Items[0].SourceID != "ALB-1" || card.Items[4].Kind != "transfer-out" ||
				card.Items[5].Kind != "adjustment" || card.Items[5].Quantity != "-2" || card.Items[5].Seq != 6 {
				t.Fatalf("stock card: %+v", card.Items)
			}
			oct3, err := is.Ledger.Handle(actx, iapp.GetLedger{Company: acme.ID, From: "2026-10-03"})
			if err != nil || len(oct3.Items) != 3 {
				t.Fatalf("ledger from 3 October: %+v %v", oct3.Items, err)
			}
			val, err := is.Valuation.Handle(actx, iapp.GetValuation{Company: acme.ID})
			// (98 + 29.5) × 2.75 = 269.50 + 81.13
			if err != nil || val.Total != "350.63" || len(val.Lines) != 1 || val.Lines[0].OnHand != "127.5" {
				t.Fatalf("valuation: %+v %v", val, err)
			}
			av, err := im.Availability.Stock(ctx, acme.ID, tornillo.ID, al2.ID)
			if err != nil || av.Available != "29.5" {
				t.Fatalf("availability: %+v %v", av, err)
			}
			wid, _ := idomain.ParseWarehouseID(al2.ID)
			if _, err := is.CloseWarehouse.Handle(actx, iapp.CloseWarehouse{ID: wid}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("close a warehouse with stock: %v", err)
			}
		})
	}
}

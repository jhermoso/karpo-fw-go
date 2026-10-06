package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/assets"
	asapp "github.com/jhermoso/karpo-fw-go/contexts/assets/application"
	asdomain "github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
	asinfra "github.com/jhermoso/karpo-fw-go/contexts/assets/infrastructure"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fcinfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	hinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payments/infrastructure"
	prinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	uinfra "github.com/jhermoso/karpo-fw-go/contexts/purchases/infrastructure"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	tinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestAssetsContext runs Assets with Purchases, Fiscal, Accounting and Parties on every engine:
// the purchase of an asset booked as an investment, the register round trip with its charges in
// order, the depreciation run (charged once), a scrapping and a sale, and the entries Accounting
// posts for each month and each disposal through its SQL inbox.
func TestAssetsContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			asinfra.DropAll(ctx, db)
			uinfra.DropAll(ctx, db)
			yinfra.DropAll(ctx, db)
			ainfra.DropAll(ctx, db)
			tinfra.DropAll(ctx, db)
			rinfra.DropAll(ctx, db)
			binfra.DropAll(ctx, db)
			finfra.DropAll(ctx, db)
			prinfra.DropAll(ctx, db)
			hinfra.DropAll(ctx, db)
			fcinfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), uinfra.Migrations(), asinfra.Migrations(),
				ainfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
			pm := parties.Compose(sw, nil)
			fm := fiscal.Compose(sw, finfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
			um := purchases.Compose(sw, uinfra.FiscalTaxes{Engine: fm.TaxEngine})
			sm := assets.Compose(sw)
			am := accounting.Compose(sw)
			broker := inprocess.NewBroker()
			broker.Subscribe("accounting", am.Consumer)
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
					}{um.Relay(broker), sm.Relay(broker)} {
						n, err := r.RelayOnce(ctx)
						must(err)
						moved = moved || n > 0
					}
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := sm.Service

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme, S.A.", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			dealer, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Concesionario Ruiz, S.L."})
			must(err)
			_, err = fm.Service.CreateRate.Handle(actx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "G21", Rate: "21",
				From: vocab.MustDate(2012, 9, 1)})
			must(err)
			_, err = fm.Service.RegisterTaxpayer.Handle(actx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
			must(err)
			for code, name := range map[string]string{"4000": "Proveedores", "4720": "IVA soportado", "2180": "Inmovilizado", "2810": "Amortización acumulada",
				"6810": "Amortización", "5430": "Créditos por enajenación", "6710": "Pérdidas del inmovilizado", "7710": "Beneficios del inmovilizado"} {
				_, err := am.Service.CreateAccount.Handle(actx, aapp.CreateAccount{Company: acme.ID, Code: code, Name: name, Postable: true})
				must(err)
			}
			_, err = am.Service.OpenLedger.Handle(actx, aapp.OpenLedger{Company: acme.ID, StartMonth: 1, Accounts: map[string]string{"suppliers": "4000",
				"input-tax": "4720", "fixed-assets": "2180", "accumulated-depreciation": "2810", "depreciation-expense": "6810",
				"asset-sale-receivable": "5430", "asset-disposal-loss": "6710", "asset-disposal-gain": "7710"}})
			must(err)
			balances := func(want map[string]string) {
				t.Helper()
				rows, err := am.Service.TrialBalance.Handle(actx, aapp.TrialBalance{Company: acme.ID})
				must(err)
				got := map[string]string{}
				for _, r := range rows {
					got[r.Account] = r.Balance
				}
				for a, b := range want {
					if got[a] != b {
						t.Fatalf("balance of %s = %q, want %s (all: %v)", a, got[a], b, got)
					}
				}
			}

			// The purchase of the van and the computer is an investment.
			_, err = um.Service.RegisterInvoice.Handle(actx, uapp.RegisterInvoice{Company: acme.ID, Supplier: dealer.ID, SupplierNumber: "V-2026-17",
				Issued: vocab.MustDate(2026, 1, 9), Received: vocab.MustDate(2026, 1, 10), Due: vocab.MustDate(2026, 2, 9), Total: "15972.00",
				Lines: []uapp.LineInput{{Category: "fixed-asset", Base: "12000", TaxCode: "G21"}, {Category: "fixed-asset", Base: "1200", TaxCode: "G21"}}})
			must(err)
			deliver()
			balances(map[string]string{"2180": "13200.00", "4720": "2772.00", "4000": "-15972.00"})

			van, err := svc.Register.Handle(actx, asapp.RegisterAsset{Company: acme.ID, Code: "fur-01", Name: "Furgoneta", Class: "vehicles", Serial: "1234-ABC",
				Supplier: dealer.ID, Document: "V-2026-17", Acquired: vocab.MustDate(2026, 1, 10), InService: vocab.MustDate(2026, 1, 16), Cost: "12000",
				Residual: "2000", LifeMonths: 48})
			must(err)
			if _, err := svc.Register.Handle(actx, asapp.RegisterAsset{Company: acme.ID, Code: "FUR-01", Name: "Otra", Class: "vehicles",
				Acquired: vocab.MustDate(2026, 1, 10), Cost: "1", LifeMonths: 12}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("code used once: %v", err)
			}
			pc, err := svc.Register.Handle(actx, asapp.RegisterAsset{Company: acme.ID, Code: "ORD-01", Name: "Ordenador", Class: "computers",
				Acquired: vocab.MustDate(2026, 2, 1), Cost: "1200", LifeMonths: 24})
			must(err)
			land, err := svc.Register.Handle(actx, asapp.RegisterAsset{Company: acme.ID, Code: "TER-01", Name: "Solar", Class: "land",
				Acquired: vocab.MustDate(2020, 5, 1), Cost: "50000"})
			must(err)

			run, err := svc.Depreciate.Handle(actx, asapp.RunDepreciation{Company: acme.ID, Year: 2026, Month: 3})
			if err != nil || run.Assets != 2 || run.Charges != 5 || run.Total != "624.19" {
				t.Fatalf("first quarter: %+v %v", run, err)
			}
			run, err = svc.Depreciate.Handle(actx, asapp.RunDepreciation{Company: acme.ID, Year: 2026, Month: 3})
			if err != nil || run.Charges != 0 {
				t.Fatalf("a month is charged once: %+v %v", run, err)
			}
			vid, _ := asdomain.ParseAssetID(van.ID)
			got, err := svc.GetAsset.Handle(actx, asapp.GetAsset{ID: vid})
			if err != nil || got.Code != "FUR-01" || got.Serial != "1234-ABC" || got.Supplier != dealer.ID || got.Document != "V-2026-17" ||
				got.Acquired != "2026-01-10" || got.InService != "2026-01-16" || got.Cost != "12000.00" || got.Residual != "2000.00" || got.LifeMonths != 48 ||
				len(got.Charges) != 3 || got.Charges[0].Period != "2026-01" || got.Charges[0].Amount != "107.53" || got.Charges[2].Period != "2026-03" ||
				got.Charges[2].Amount != "208.33" || got.Accumulated != "524.19" || got.Status != "in-service" || got.Disposed != "" {
				t.Fatalf("van round trip: %+v %v", got, err)
			}
			deliver()
			balances(map[string]string{"6810": "624.19", "2810": "-624.19"})

			pid, _ := asdomain.ParseAssetID(pc.ID)
			scrapped, err := svc.Dispose.Handle(actx, asapp.DisposeAsset{ID: pid, Date: vocab.MustDate(2026, 4, 15), Kind: "scrap"})
			if err != nil || scrapped.Result != "-1100.00" || scrapped.Status != "disposed" {
				t.Fatalf("scrap: %+v %v", scrapped, err)
			}
			sold, err := svc.Dispose.Handle(actx, asapp.DisposeAsset{ID: vid, Date: vocab.MustDate(2026, 7, 10), Kind: "sale", Proceeds: "11000"})
			if err != nil || sold.Accumulated != "1149.18" || sold.Result != "149.18" || len(sold.Charges) != 6 {
				t.Fatalf("sale: %+v %v", sold, err)
			}
			got, err = svc.GetAsset.Handle(actx, asapp.GetAsset{ID: vid})
			if err != nil || got.Disposed != "2026-07-10" || got.Disposal != "sale" || got.Proceeds != "11000.00" || got.Result != "149.18" ||
				len(got.Charges) != 6 || got.Charges[5].Period != "2026-06" || got.Version != sold.Version {
				t.Fatalf("sold round trip: %+v %v", got, err)
			}
			deliver()
			balances(map[string]string{"2180": "0.00", "2810": "0.00", "6810": "1249.18", "5430": "11000.00", "6710": "1100.00", "7710": "-149.18"})

			lid, _ := asdomain.ParseAssetID(land.ID)
			changed, err := svc.Change.Handle(actx, asapp.ChangeAsset{ID: lid, Name: "Solar norte"})
			if err != nil || changed.Name != "Solar norte" || changed.Version != 2 || changed.Monthly != "0.00" {
				t.Fatalf("change: %+v %v", changed, err)
			}
			book, err := svc.Book.Handle(actx, asapp.GetBook{Company: acme.ID})
			if err != nil || len(book.Assets) != 1 || book.Cost != "50000.00" || book.NetBookValue != "50000.00" {
				t.Fatalf("book: %+v %v", book, err)
			}
			page, err := svc.SearchAssets.Handle(actx, asapp.SearchAssets{Company: acme.ID})
			if err != nil || len(page.Items) != 3 || page.Items[0].Code != "FUR-01" || page.Items[2].Code != "TER-01" {
				t.Fatalf("register: %+v %v", page.Items, err)
			}
			page, err = svc.SearchAssets.Handle(actx, asapp.SearchAssets{Company: acme.ID, Class: "computers", InService: true})
			if err != nil || len(page.Items) != 0 {
				t.Fatalf("computers in service: %+v %v", page.Items, err)
			}
		})
	}
}

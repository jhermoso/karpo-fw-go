package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fcinfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	hinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/payments"
	yapp "github.com/jhermoso/karpo-fw-go/contexts/payments/application"
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payments/infrastructure"
	prinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases"
	uapp "github.com/jhermoso/karpo-fw-go/contexts/purchases/application"
	udomain "github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
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

// TestPurchasesContext runs Purchases with Fiscal, Payments, Accounting and Parties on every
// engine: supplier profile round trip, invoices booked with the Fiscal tax engine (two rates,
// withholding, non-deductible tax, corrective), their round trip with lines, breakdown and
// accounts, the payables, entries and withholdings fed through the SQL inboxes, and a cancellation.
func TestPurchasesContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
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
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), uinfra.Migrations(), yinfra.Migrations(),
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
			ym := payments.Compose(sw, nil)
			am := accounting.Compose(sw)
			broker := inprocess.NewBroker()
			broker.Subscribe("payments", ym.Consumer)
			broker.Subscribe("accounting", am.Consumer)
			broker.Subscribe("fiscal", fm.Consumer)
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
					}{um.Relay(broker), ym.Relay(broker)} {
						n, err := r.RelayOnce(ctx)
						must(err)
						moved = moved || n > 0
					}
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := um.Service

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme, S.A.", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			asesor, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Luis", FirstSurname: "Muñoz"})
			must(err)
			tienda, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Papelería, S.L."})
			must(err)
			for code, rate := range map[string]string{"G21": "21", "R10": "10"} {
				_, err := fm.Service.CreateRate.Handle(actx, fapp.CreateRate{Type: "vat", Territory: "common", Code: code, Description: code, Rate: rate,
					From: vocab.MustDate(2012, 9, 1)})
				must(err)
			}
			tp, err := fm.Service.RegisterTaxpayer.Handle(actx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
			must(err)
			tpid, _ := fdomain.ParseTaxpayerID(tp.ID)
			_, err = fm.Service.AddObligation.Handle(actx, fapp.AddObligation{ID: tpid, Form: "111", Periodicity: "quarterly", FromYear: 2020})
			must(err)
			for code, name := range map[string]string{"4000": "Proveedores", "4720": "IVA soportado", "4751": "Retenciones", "6000": "Compras", "6230": "Profesionales",
				"6280": "Suministros"} {
				_, err := am.Service.CreateAccount.Handle(actx, aapp.CreateAccount{Company: acme.ID, Code: code, Name: name, Postable: true})
				must(err)
			}
			_, err = am.Service.OpenLedger.Handle(actx, aapp.OpenLedger{Company: acme.ID, StartMonth: 1, Accounts: map[string]string{"suppliers": "4000",
				"input-tax": "4720", "withholding-payable": "4751", "purchases": "6000", "professional-services": "6230", "supplies": "6280"}})
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

			prof, err := svc.SetSupplier.Handle(actx, uapp.SetSupplier{Company: acme.ID, Supplier: asesor.ID, Category: "professional-services",
				WithholdingRate: "15", PaymentDays: 30, IBAN: "ES7921000813610123456789"})
			must(err)
			prof, err = svc.SetSupplier.Handle(actx, uapp.SetSupplier{Company: acme.ID, Supplier: asesor.ID, Category: "professional-services",
				WithholdingRate: "7", PaymentDays: 30, IBAN: "ES7921000813610123456789"})
			if err != nil || prof.WithholdingRate != "7.00" || prof.Version != 2 {
				t.Fatalf("supplier profile upsert: %+v %v", prof, err)
			}

			// 333.33 of advice at 21% (70.00) with 7% withheld (23.33), paid to the profile account.
			a, err := svc.RegisterInvoice.Handle(actx, uapp.RegisterInvoice{Company: acme.ID, Supplier: asesor.ID, SupplierNumber: "2026/0042",
				Issued: vocab.MustDate(2026, 9, 30), Received: vocab.MustDate(2026, 10, 2), Total: "403.33",
				Lines: []uapp.LineInput{{Description: "Asesoría fiscal", Base: "333.33", TaxCode: "g21"}}})
			must(err)
			if _, err := svc.RegisterInvoice.Handle(actx, uapp.RegisterInvoice{Company: acme.ID, Supplier: asesor.ID, SupplierNumber: "2026/0042",
				Issued: vocab.MustDate(2026, 9, 30), Received: vocab.MustDate(2026, 10, 2), Total: "403.33",
				Lines: []uapp.LineInput{{Base: "333.33", TaxCode: "G21"}}}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("booked once: %v", err)
			}
			b, err := svc.RegisterInvoice.Handle(actx, uapp.RegisterInvoice{Company: acme.ID, Supplier: tienda.ID, SupplierNumber: "A-77",
				Issued: vocab.MustDate(2026, 10, 1), Received: vocab.MustDate(2026, 10, 3), Due: vocab.MustDate(2026, 10, 31), Total: "176.00",
				Lines: []uapp.LineInput{{Category: "goods", Base: "100", TaxCode: "G21"}, {Category: "supplies", Base: "50", TaxCode: "R10"}},
				PayTo: []uapp.PayToInput{{IBAN: "ES9121000418450200051332", Amount: "100.00"}, {IBAN: "ES6621000418401234567891", Amount: "76.00"}}})
			must(err)
			c, err := svc.RegisterInvoice.Handle(actx, uapp.RegisterInvoice{Company: acme.ID, Supplier: tienda.ID, SupplierNumber: "A-78",
				Issued: vocab.MustDate(2026, 10, 1), Received: vocab.MustDate(2026, 10, 3), Due: vocab.MustDate(2026, 10, 31), Total: "121.00",
				NonDeductible: true, Lines: []uapp.LineInput{{Category: "supplies", Base: "100", TaxCode: "G21"}}})
			must(err)
			_, err = svc.RegisterInvoice.Handle(actx, uapp.RegisterInvoice{Company: acme.ID, Supplier: tienda.ID, SupplierNumber: "AR-1",
				Issued: vocab.MustDate(2026, 10, 4), Received: vocab.MustDate(2026, 10, 5), Due: vocab.MustDate(2026, 10, 5), Total: "-24.20", Corrects: b.ID,
				Lines: []uapp.LineInput{{Category: "goods", Base: "-20", TaxCode: "G21"}}})
			must(err)

			id, _ := udomain.ParseInvoiceID(b.ID)
			got, err := svc.GetInvoice.Handle(actx, uapp.GetInvoice{ID: id})
			if err != nil || got.Register != "FR-2026-000002" || len(got.Lines) != 2 || got.Lines[1].Category != "supplies" || len(got.Taxes) != 2 ||
				got.Taxes[1].Rate != "10.00" || got.Taxes[1].Amount != "5.00" || len(got.PayTo) != 2 || got.PayTo[1].Amount != "76.00" || got.Due != "2026-10-31" {
				t.Fatalf("invoice round trip: %+v %v", got, err)
			}
			aid, _ := udomain.ParseInvoiceID(a.ID)
			got, err = svc.GetInvoice.Handle(actx, uapp.GetInvoice{ID: aid})
			if err != nil || got.Withholding != "23.33" || got.WithholdingRate != "7.00" || got.Payable != "380.00" || got.Due != "2026-10-30" ||
				got.PayTo[0].IBAN != "ES7921000813610123456789" || got.Lines[0].Description != "Asesoría fiscal" || got.Lines[0].TaxCode != "G21" {
				t.Fatalf("adviser round trip: %+v %v", got, err)
			}
			if c.Total != "121.00" {
				t.Fatalf("non deductible: %+v", c)
			}
			deliver()

			pays, err := ym.Service.SearchPayables.Handle(actx, yapp.SearchPayables{Company: acme.ID, Kind: "supplier-invoice"})
			if err != nil || len(pays.Items) != 3 {
				t.Fatalf("payables: %+v %v", pays.Items, err)
			}
			// 6230 333.33; 6000 100-20; 6280 50+121; 4720 70+21+5-4.20; 4751 -23.33; 4000 -(380+176+121-24.20).
			balances(map[string]string{"6230": "333.33", "6000": "80.00", "6280": "171.00", "4720": "91.80", "4751": "-23.33", "4000": "-652.80"})
			q3, err := fm.Service.GenerateFiling.Handle(actx, fapp.GenerateFiling{Organization: acme.ID, Form: "111", Year: 2026, Period: "3T"})
			if err != nil || q3.Withheld != "23.33" || q3.Perceptions != "333.33" {
				t.Fatalf("Modelo 111: %+v %v", q3, err)
			}

			_, err = svc.CancelInvoice.Handle(actx, uapp.CancelInvoice{ID: aid, Reason: "duplicada"})
			must(err)
			deliver()
			balances(map[string]string{"6230": "0.00", "4751": "0.00", "4000": "-272.80"})
			q3, err = fm.Service.GenerateFiling.Handle(actx, fapp.GenerateFiling{Organization: acme.ID, Form: "111", Year: 2026, Period: "3T"})
			if err != nil || q3.Withheld != "0.00" {
				t.Fatalf("Modelo 111 after the cancellation: %+v %v", q3, err)
			}
			reg, err := svc.SearchInvoices.Handle(actx, uapp.SearchInvoices{Company: acme.ID, Supplier: tienda.ID})
			if err != nil || len(reg.Items) != 3 || reg.Items[2].Corrects != b.ID {
				t.Fatalf("register of the stationer: %+v %v", reg.Items, err)
			}
		})
	}
}

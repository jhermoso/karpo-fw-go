package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting"
	aapp "github.com/jhermoso/karpo-fw-go/contexts/accounting/application"
	adomain "github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	ainfra "github.com/jhermoso/karpo-fw-go/contexts/accounting/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/billing"
	bapp "github.com/jhermoso/karpo-fw-go/contexts/billing/application"
	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
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
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables"
	rapp "github.com/jhermoso/karpo-fw-go/contexts/receivables/application"
	rdomain "github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	rinfra "github.com/jhermoso/karpo-fw-go/contexts/receivables/infrastructure"
	tinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// TestAccountingContext runs Accounting with Receivables, Billing, Fiscal and Parties on every
// engine: chart and ledger round trip (roles, tax accounts, closed periods), an invoice and a
// collection posted through the SQL inbox, the cancelled collection reversed, a payslip posted once
// despite redeliveries, manual entries, reversal, closed period, trial balance and account ledger.
func TestAccountingContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			ainfra.DropAll(ctx, db)
			tinfra.DropAll(ctx, db)
			rinfra.DropAll(ctx, db)
			binfra.DropAll(ctx, db)
			finfra.DropAll(ctx, db)
			yinfra.DropAll(ctx, db)
			hinfra.DropAll(ctx, db)
			fcinfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations(), rinfra.Migrations(),
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
			bm := billing.Compose(sw, binfra.FiscalTaxes{Engine: fm.TaxEngine}, binfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
			rm := receivables.Compose(sw, nil)
			am := accounting.Compose(sw)
			broker := inprocess.NewBroker()
			broker.Subscribe("receivables", rm.Consumer)
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
					}{bm.Relay(broker), rm.Relay(broker)} {
						n, err := r.RelayOnce(ctx)
						must(err)
						moved = moved || n > 0
					}
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			svc := am.Service

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme, S.A.", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			ana, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "Muñoz"})
			must(err)
			for party, d := range map[string][2]string{acme.ID: {"c0000000-0004-0000-0000-000000000003", "A58818501"}, ana.ID: {"c0000000-0004-0000-0000-000000000002", "12345678Z"}} {
				id, _ := pdomain.ParsePartyID(party)
				_, err := pm.Service.AddIdentification.Handle(actx, papp.AddIdentification{PartyID: id, DocumentType: d[0], Country: "ES", Number: d[1], Primary: true})
				must(err)
			}

			for code, name := range map[string]string{"4300": "Clientes", "7000": "Ventas", "4770": "IVA repercutido", "5700": "Caja", "5720": "Bancos",
				"6400": "Sueldos", "6420": "SS empresa", "4760": "SS acreedora", "4751": "Retenciones", "4650": "Remuneraciones"} {
				_, err := svc.CreateAccount.Handle(actx, aapp.CreateAccount{Company: acme.ID, Code: code, Name: name, Postable: true})
				must(err)
			}
			if _, err := svc.CreateAccount.Handle(actx, aapp.CreateAccount{Company: acme.ID, Code: "4300", Name: "Otra", Postable: true}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("duplicate account: %v", err)
			}
			led, err := svc.OpenLedger.Handle(actx, aapp.OpenLedger{Company: acme.ID, StartMonth: 1, Accounts: map[string]string{"revenue": "7000",
				"customers": "4300", "cash": "5700", "bank": "5720", "wages": "6400", "employer-social-security": "6420", "social-security-payable": "4760",
				"withholding-payable": "4751", "net-pay-payable": "4650"}, TaxCodes: map[string]string{"G21": "4770"}})
			must(err)
			lid, _ := adomain.ParseLedgerID(led.ID)
			_, err = svc.ClosePeriod.Handle(actx, aapp.ChangePeriod{ID: lid, Year: 2026, Month: 7})
			must(err)
			_, err = svc.ClosePeriod.Handle(actx, aapp.ChangePeriod{ID: lid, Year: 2026, Month: 6})
			must(err)
			led, err = svc.GetLedger.Handle(actx, aapp.GetLedger{Company: acme.ID})
			if err != nil || len(led.Accounts) != 9 || led.TaxCodes["G21"] != "4770" || len(led.Closed) != 2 || led.Closed[0] != "2026/06" {
				t.Fatalf("ledger round trip: %+v %v", led, err)
			}

			// An invoice of 82.64 + 21% and a partial cash collection, posted from the Published Language.
			_, err = fm.Service.CreateRate.Handle(actx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "G21", Description: "General", Rate: "21",
				From: vocab.MustDate(2012, 9, 1)})
			must(err)
			_, err = fm.Service.RegisterTaxpayer.Handle(actx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
			must(err)
			fa, err := bm.Service.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
			must(err)
			inv, err := bm.Service.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: acme.ID, Customer: ana.ID, DetailsInput: bapp.DetailsInput{DueDate: vocab.MustDate(2026, 10, 15)}})
			must(err)
			iid, _ := bdomain.ParseInvoiceID(inv.ID)
			_, err = bm.Service.AddLine.Handle(actx, bapp.AddLine{ID: iid, Description: "Cuota", Quantity: "1", UnitPrice: "82.64", TaxCode: "G21"})
			must(err)
			_, err = bm.Service.Issue.Handle(actx, bapp.IssueInvoice{ID: iid, Series: fa.ID, Date: vocab.MustDate(2026, 9, 28)})
			must(err)
			deliver()
			col, err := rm.Service.RegisterCollection.Handle(actx, rapp.RegisterCollection{Seller: acme.ID, Payer: ana.ID, Date: vocab.MustDate(2026, 10, 2),
				Amount: "50", Method: "cash", Allocations: []rapp.AllocationInput{{Invoice: inv.ID, Installment: 1, Amount: "50"}}})
			must(err)
			deliver()
			balances := func() map[string]string {
				t.Helper()
				rows, err := svc.TrialBalance.Handle(actx, aapp.TrialBalance{Company: acme.ID})
				must(err)
				out := map[string]string{}
				for _, r := range rows {
					out[r.Account] = r.Balance
				}
				return out
			}
			expect := func(want map[string]string) {
				t.Helper()
				got := balances()
				for a, b := range want {
					if got[a] != b {
						t.Fatalf("balance of %s = %q, want %s (all: %v)", a, got[a], b, got)
					}
				}
			}
			expect(map[string]string{"4300": "49.99", "7000": "-82.64", "4770": "-17.35", "5700": "50.00"})

			// The collection is cancelled: Receivables publishes the reversed allocation.
			// Accounting dates the reversal on the moment of the cancellation: it runs on a fixed clock,
			// so that the journal is that of 2026 whatever the day the test runs.
			cid, _ := rdomain.ParseCollectionID(col.ID)
			func() {
				defer fw.SetClock(fake.New(time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)))()
				_, err = rm.Service.CancelCollection.Handle(actx, rapp.CancelCollection{ID: cid})
				must(err)
				deliver()
			}()
			expect(map[string]string{"4300": "99.99", "5700": "0.00"})

			// A payslip posted once despite a redelivery and a second message about it.
			slip := fw.NewUUID().String()
			data, _ := json.Marshal(map[string]any{"payslipId": slip, "person": ana.ID, "employer": acme.ID, "periodEnd": "2026-09-30", "gross": "1800.00",
				"socialSecurity": "114.30", "incomeTax": "216.00", "net": "1469.70", "employerCost": "576.00"})
			msg := fw.NewUUID().String()
			for _, id := range []string{msg, msg, fw.NewUUID().String()} {
				must(broker.Send(ctx, application.Envelope{ID: id, Type: "payroll.payslip-approved.v1", Source: "payroll", OccurredAt: fw.Now(), Data: data}))
			}
			expect(map[string]string{"6400": "1800.00", "6420": "576.00", "4760": "-690.30", "4751": "-216.00", "4650": "-1469.70"})

			// Manual entries: the closed period refuses; a reversal is linked both ways.
			if _, err := svc.PostEntry.Handle(actx, aapp.PostEntry{Company: acme.ID, Date: vocab.MustDate(2026, 7, 15), Description: "Julio",
				Lines: []aapp.LineInput{{Account: "5700", Debit: "10"}, {Account: "5720", Credit: "10"}}}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("closed period: %v", err)
			}
			e1, err := svc.PostEntry.Handle(actx, aapp.PostEntry{Company: acme.ID, Date: vocab.MustDate(2026, 10, 5), Description: "Traspaso a caja",
				Lines: []aapp.LineInput{{Account: "5700", Debit: "300.25", Description: "Caja central"}, {Account: "5720", Credit: "300.25"}}})
			must(err)
			id1, _ := adomain.ParseEntryID(e1.ID)
			rev, err := svc.ReverseEntry.Handle(actx, aapp.ReverseEntry{ID: id1, Date: vocab.MustDate(2026, 10, 6)})
			must(err)
			if _, err := svc.ReverseEntry.Handle(actx, aapp.ReverseEntry{ID: id1}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("reversed once: %v", err)
			}
			e1, err = svc.GetEntry.Handle(actx, aapp.GetEntry{ID: id1})
			if err != nil || e1.ReversedBy != rev.ID || rev.Reverses != e1.ID || e1.Lines[0].Debit != "300.25" || e1.Lines[0].Description != "Caja central" ||
				e1.Lines[1].Party != "" || e1.Date != "2026-10-05" || e1.Period != 10 {
				t.Fatalf("entry round trip: %+v / %+v %v", e1, rev, err)
			}
			expect(map[string]string{"5700": "0.00", "5720": "0.00"})

			// The account ledger of customers with its running balance; the journal without gaps.
			moves, err := svc.AccountLedger.Handle(actx, aapp.AccountLedger{Company: acme.ID, Account: "4300"})
			if err != nil || len(moves) != 3 || moves[0].Balance != "99.99" || moves[1].Balance != "49.99" || moves[2].Balance != "99.99" || moves[0].Party != ana.ID {
				t.Fatalf("account ledger: %+v %v", moves, err)
			}
			page, err := svc.SearchEntries.Handle(actx, aapp.SearchEntries{Company: acme.ID, Size: 50})
			must(err)
			seen := map[int64]bool{}
			for _, e := range page.Items {
				seen[e.Number] = true
			}
			if len(page.Items) != 6 || len(seen) != 6 || !seen[1] || !seen[6] {
				t.Fatalf("journal: %+v", page.Items)
			}
			later, err := svc.SearchEntries.Handle(actx, aapp.SearchEntries{Company: acme.ID, From: "2026-10-01", Size: 50})
			if err != nil || len(later.Items) != 4 {
				t.Fatalf("from October: %+v %v", later.Items, err)
			}
		})
	}
}

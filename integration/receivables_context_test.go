package integration

import (
	"context"
	"errors"
	"testing"

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
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestReceivablesContext runs Receivables fed by Billing on every engine: terms with fixed days,
// receivables opened through the SQL inbox, installments and allocations (children), both-side
// caps, offset of a credit, reversal and exposure (settled flag and dates in SQL).
func TestReceivablesContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			rinfra.DropAll(ctx, db)
			binfra.DropAll(ctx, db)
			finfra.DropAll(ctx, db)
			yinfra.DropAll(ctx, db)
			hinfra.DropAll(ctx, db)
			fcinfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations(), rinfra.Migrations()})
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
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			ana, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "García"})
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
			fa, err := bm.Service.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
			must(err)
			rs, err := bm.Service.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "R", Year: 2026, Corrective: true})
			must(err)

			rsvc := rm.Service
			terms, err := rsvc.CreateTerms.Handle(actx, rapp.CreateTerms{Seller: acme.ID, Code: "30-60", Description: "30/60 día 5 y 20", Installments: 3,
				DaysToFirst: 30, DaysBetween: 30, FixedDays: []int{20, 5}, NoPaymentFrom: 12, NoPaymentTo: 12})
			must(err)
			tid, _ := rdomain.ParseTermsID(terms.ID)
			if got, err := rsvc.Preview.Handle(actx, rapp.PreviewSchedule{ID: tid, Issued: "2026-09-28", Amount: "100"}); err != nil || len(got) != 3 ||
				got[0].Date != "2026-11-05" || got[1].Date != "2027-01-05" || got[2].Amount != "33.34" {
				t.Fatalf("terms round trip: %+v %v", got, err)
			}
			limit := "1000"
			_, err = rsvc.SetCredit.Handle(actx, rapp.SetCredit{Seller: acme.ID, Customer: ana.ID, Terms: terms.ID, Limit: &limit})
			must(err)

			issue := func(d bapp.DraftInvoice, series, qty, price string) bapp.InvoiceDTO {
				inv, err := bm.Service.DraftInvoice.Handle(actx, d)
				must(err)
				id, _ := bdomain.ParseInvoiceID(inv.ID)
				_, err = bm.Service.AddLine.Handle(actx, bapp.AddLine{ID: id, Description: "Servicio", Quantity: qty, UnitPrice: price, TaxCode: "G21"})
				must(err)
				inv, err = bm.Service.Issue.Handle(actx, bapp.IssueInvoice{ID: id, Series: series, Date: vocab.MustDate(2026, 9, 28)})
				must(err)
				return inv
			}
			inv := issue(bapp.DraftInvoice{Seller: acme.ID, Customer: ana.ID}, fa.ID, "1", "100")
			credit := issue(bapp.DraftInvoice{Seller: acme.ID, Corrects: inv.ID, Reason: "R4"}, rs.ID, "-1", "10")
			broker := inprocess.NewBroker()
			broker.Subscribe("receivables", rm.Consumer)
			for {
				n, err := bm.Relay(broker).RelayOnce(ctx)
				must(err)
				if n == 0 {
					break
				}
			}
			rid, _ := rdomain.ParseReceivableID(inv.ID)
			r, err := rsvc.GetReceivable.Handle(actx, rapp.GetReceivable{ID: rid})
			// 121.00 in three installments: 40.33, 40.33, 40.34 (the December ones move to 5 January).
			if err != nil || len(r.Installments) != 3 || r.Installments[1].Due != "2027-01-05" || r.Installments[2].Amount != "40.34" || r.Settled {
				t.Fatalf("receivable round trip: %+v %v", r, err)
			}
			col, err := rsvc.RegisterCollection.Handle(actx, rapp.RegisterCollection{Seller: acme.ID, Payer: ana.ID, Date: vocab.MustDate(2026, 11, 5),
				Amount: "50", Method: "transfer", Allocations: []rapp.AllocationInput{{Invoice: inv.ID, Installment: 1, Amount: "40.33"}}})
			must(err)
			cid, _ := rdomain.ParseCollectionID(col.ID)
			if _, err := rsvc.Allocate.Handle(actx, rapp.Allocate{ID: cid, AllocationInput: rapp.AllocationInput{Invoice: inv.ID, Installment: 1, Amount: "0.01"}}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("installment 1 is settled: %v", err)
			}
			col, err = rsvc.Allocate.Handle(actx, rapp.Allocate{ID: cid, AllocationInput: rapp.AllocationInput{Invoice: inv.ID, Installment: 2, Amount: "9.67"}})
			if err != nil || col.Unallocated != "0.00" || len(col.Allocations) != 2 {
				t.Fatalf("allocate: %+v %v", col, err)
			}
			_, err = rsvc.Offset.Handle(actx, rapp.OffsetCredit{Seller: acme.ID, Credit: credit.ID, Invoice: inv.ID, Installment: 3, Amount: "12.10",
				Date: vocab.MustDate(2026, 11, 5)})
			must(err)
			exp, err := rm.Credit.Exposure(ctx, acme.ID, ana.ID, "2027-01-06")
			// 121.00 − 50.00 − 12.10 (credit netted, credit settled) = 58.90, all of it due on 5 January.
			if err != nil || exp.Open != "58.90" || exp.Overdue != "58.90" || exp.Available != "941.10" {
				t.Fatalf("exposure: %+v %v", exp, err)
			}
			col, err = rsvc.CancelCollection.Handle(actx, rapp.CancelCollection{ID: cid})
			if err != nil || !col.Cancelled {
				t.Fatalf("cancel: %+v %v", col, err)
			}
			if exp, _ := rm.Credit.Exposure(ctx, acme.ID, ana.ID, "2027-01-06"); exp.Open != "108.90" {
				t.Fatalf("exposure after the cancellation: %+v", exp)
			}
			page, err := rsvc.SearchReceivables.Handle(actx, rapp.SearchReceivables{Seller: acme.ID, OpenOnly: true})
			if err != nil || page.Total != 1 {
				t.Fatalf("open receivables (settled flag in SQL): %+v %v", page, err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

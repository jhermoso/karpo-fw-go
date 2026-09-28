package integration

import (
	"context"
	"errors"
	"strings"
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
	"github.com/jhermoso/karpo-fw-go/contexts/treasury"
	tapp "github.com/jhermoso/karpo-fw-go/contexts/treasury/application"
	tdomain "github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
	tinfra "github.com/jhermoso/karpo-fw-go/contexts/treasury/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestTreasuryContext runs Treasury with Receivables, Billing, Fiscal and Parties on every engine:
// account and mandates, a remittance proposed from the due items, generated (mandate use,
// sequence types, creditor identifier), its pain.008 file, settled (collection in Receivables
// through the SQL inbox) and returned (collection cancelled).
func TestTreasuryContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
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
				tinfra.Migrations()})
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
			tm := treasury.Compose(sw, tinfra.ReceivablesDueItems{Collectable: rm.Collectable}, tinfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
			broker := inprocess.NewBroker()
			broker.Subscribe("receivables", rm.Consumer)
			deliver := func() {
				for _, r := range []interface {
					RelayOnce(context.Context) (int, error)
				}{bm.Relay(broker), tm.Relay(broker)} {
					for {
						n, err := r.RelayOnce(ctx)
						if err != nil {
							t.Fatal(err)
						}
						if n == 0 {
							break
						}
					}
				}
			}
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}

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
			fa, err := bm.Service.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
			must(err)
			inv, err := bm.Service.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: acme.ID, Customer: ana.ID, DetailsInput: bapp.DetailsInput{DueDate: vocab.MustDate(2026, 10, 15)}})
			must(err)
			iid, _ := bdomain.ParseInvoiceID(inv.ID)
			_, err = bm.Service.AddLine.Handle(actx, bapp.AddLine{ID: iid, Description: "Cuota", Quantity: "1", UnitPrice: "82.64", TaxCode: "G21"})
			must(err)
			inv, err = bm.Service.Issue.Handle(actx, bapp.IssueInvoice{ID: iid, Series: fa.ID, Date: vocab.MustDate(2026, 9, 28)})
			must(err)
			deliver()

			svc := tm.Service
			acc, err := svc.OpenAccount.Handle(actx, tapp.OpenAccount{Owner: acme.ID, IBAN: "ES9121000418450200051332", BIC: "CAIXESBBXXX", Alias: "Cobros",
				Collections: true, CreditorSuffix: "001", Opened: vocab.MustDate(2020, 1, 1)})
			must(err)
			_, err = svc.RegisterMandate.Handle(actx, tapp.RegisterMandate{Creditor: acme.ID, Debtor: ana.ID, IBAN: "ES7921000813610123456789",
				Reference: "MAND-0001", Scheme: "CORE", Signed: vocab.MustDate(2025, 1, 10)})
			must(err)
			if _, err := svc.RegisterMandate.Handle(actx, tapp.RegisterMandate{Creditor: acme.ID, Debtor: ana.ID, IBAN: "ES7921000813610123456789",
				Reference: "MAND-0001", Scheme: "B2B", Signed: vocab.MustDate(2025, 1, 10)}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("duplicate mandate: %v", err)
			}
			r, err := svc.Propose.Handle(actx, tapp.ProposeRemittance{Creditor: acme.ID, Account: acc.ID, Scheme: "CORE",
				CollectionDate: vocab.MustDate(2026, 10, 20), DueTo: vocab.MustDate(2026, 10, 31)})
			if err != nil || len(r.Items) != 1 || r.Items[0].Amount != "99.99" || r.Items[0].DebtorName != "Ana Muñoz" {
				t.Fatalf("proposal: %+v %v", r, err)
			}
			rid, _ := tdomain.ParseRemittanceID(r.ID)
			r, err = svc.Generate.Handle(actx, tapp.GenerateRemittance{ID: rid})
			// The creditor suffix 001 does not change the check digits of A58818501.
			if err != nil || r.CreditorID != "ES30001A58818501" || r.Items[0].Sequence != "FRST" || r.Items[0].EndToEnd != "FA-2026-000001-1" {
				t.Fatalf("generate round trip: %+v %v", r, err)
			}
			file, err := svc.File.Handle(actx, tapp.GetFile{ID: rid})
			if err != nil || !strings.Contains(string(file), "<CtrlSum>99.99</CtrlSum>") || !strings.Contains(string(file), "<Nm>Ana Munoz</Nm>") ||
				!strings.Contains(string(file), "<BIC>CAIXESBBXXX</BIC>") {
				t.Fatalf("pain.008 from the stored remittance:\n%s %v", file, err)
			}
			_, err = svc.Settle.Handle(actx, tapp.SettleRemittance{ID: rid, On: vocab.MustDate(2026, 10, 20)})
			must(err)
			deliver()
			recID, _ := rdomain.ParseReceivableID(inv.ID)
			got, err := rm.Service.GetReceivable.Handle(actx, rapp.GetReceivable{ID: recID})
			if err != nil || !got.Settled {
				t.Fatalf("collected in Receivables: %+v %v", got, err)
			}
			_, err = svc.Return.Handle(actx, tapp.ReturnDebit{ID: rid, EndToEnd: "FA-2026-000001-1", On: vocab.MustDate(2026, 10, 27), Reason: "MD06"})
			must(err)
			deliver()
			got, _ = rm.Service.GetReceivable.Handle(actx, rapp.GetReceivable{ID: recID})
			if got.Settled || got.Open != "99.99" {
				t.Fatalf("returned in Receivables: %+v", got)
			}
			r, _ = svc.Get.Handle(actx, tapp.GetRemittance{ID: rid})
			if r.Status != "settled" || r.Items[0].Returned != "2026-10-27" || r.Items[0].Reason != "MD06" {
				t.Fatalf("return round trip: %+v", r)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

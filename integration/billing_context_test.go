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
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestBillingContext runs Billing with Fiscal and Parties on every engine: series, invoice lines
// with exact decimals, taxes calculated by the Spanish jurisdiction and frozen at issue, gap-free
// numbering in the same transaction, corrective invoice referencing the original (self FK).
func TestBillingContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			binfra.DropAll(ctx, db)
			finfra.DropAll(ctx, db)
			yinfra.DropAll(ctx, db)
			hinfra.DropAll(ctx, db)
			fcinfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), binfra.Migrations()})
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
				Surcharge: "5.2", From: vocab.MustDate(2012, 9, 1)})
			must(err)
			_, err = fm.Service.CreateTreatment.Handle(actx, fapp.CreateTreatment{Territory: "common", Code: "E1", Description: "Exenta", Kind: "exempt"})
			must(err)
			_, err = fm.Service.RegisterTaxpayer.Handle(actx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common", FiscalYearStartMonth: 1}})
			must(err)

			svc := bm.Service
			fa, err := svc.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "FA", Year: 2026})
			must(err)
			rs, err := svc.OpenSeries.Handle(actx, bapp.OpenSeries{Seller: acme.ID, Code: "R", Year: 2026, Corrective: true})
			must(err)
			inv, err := svc.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: acme.ID, Customer: ana.ID, DetailsInput: bapp.DetailsInput{
				OperationDate: vocab.MustDate(2026, 9, 15), DueDate: vocab.MustDate(2026, 10, 31), EquivalenceSurcharge: true}})
			must(err)
			id, _ := bdomain.ParseInvoiceID(inv.ID)
			for _, l := range []bapp.AddLine{
				{ID: id, Description: "Consultoría", Quantity: "3", UnitPrice: "10.03", TaxCode: "G21"},
				{ID: id, Description: "Unidades", Quantity: "2.5", UnitPrice: "19.999", Discount: "12.5", TaxCode: "G21"},
				{ID: id, Description: "Formación", Quantity: "1", UnitPrice: "50", Treatment: "E1"},
			} {
				_, err = svc.AddLine.Handle(actx, l)
				must(err)
			}
			inv, err = svc.Issue.Handle(actx, bapp.IssueInvoice{ID: id, Series: fa.ID, Date: vocab.MustDate(2026, 9, 28)})
			must(err)
			// 2.5 × 19.999 = 49.9975 − 12.5 % = 43.747… → 43.75; G21 base 30.09 + 43.75 = 73.84 → 15.51; surcharge 3.84.
			got, err := svc.GetInvoice.Handle(actx, bapp.GetInvoice{ID: id})
			if err != nil || got.Number != "FA-2026-000001" || got.Lines[1].Net != "43.75" || got.Lines[1].Quantity != "2.5" || got.Lines[1].UnitPrice != "19.999" ||
				got.Taxes.Net != "123.84" || got.Taxes.Tax != "15.51" || got.Taxes.Surcharge != "3.84" || got.Taxes.Total != "143.19" ||
				len(got.Taxes.Lines) != 2 || got.Taxes.Lines[0].SurchargeRate != "5.20" || got.OperationDate != "2026-09-15" || got.CustomerNIF != "12345678Z" {
				t.Fatalf("round trip: %+v %+v %v", got, got.Taxes, err)
			}
			if _, err := svc.AddLine.Handle(actx, bapp.AddLine{ID: id, Description: "X", Quantity: "1", UnitPrice: "1", TaxCode: "G21"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("immutable: %v", err)
			}
			fix, err := svc.DraftInvoice.Handle(actx, bapp.DraftInvoice{Seller: acme.ID, Corrects: inv.ID, Reason: "R4"})
			must(err)
			fixID, _ := bdomain.ParseInvoiceID(fix.ID)
			_, err = svc.AddLine.Handle(actx, bapp.AddLine{ID: fixID, Description: "Abono", Quantity: "-1", UnitPrice: "10.03", TaxCode: "G21"})
			must(err)
			fix, err = svc.Issue.Handle(actx, bapp.IssueInvoice{ID: fixID, Series: rs.ID, Date: vocab.MustDate(2026, 9, 29)})
			if err != nil || fix.Number != "R-2026-000001" || fix.Corrects != inv.ID || fix.Taxes.Tax != "-2.11" || fix.Taxes.Surcharge != "-0.52" {
				t.Fatalf("corrective: %+v %v", fix, err)
			}
			page, err := svc.SearchInvoices.Handle(actx, bapp.SearchInvoices{Seller: acme.ID, Status: "issued", From: "2026-09-28", To: "2026-09-28"})
			if err != nil || page.Total != 1 {
				t.Fatalf("search by issue date in SQL: %+v %v", page, err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

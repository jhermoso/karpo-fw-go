package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities"
	fcapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	fcdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	fcinfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/fiscal/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/fiscal/infrastructure"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/hr"
	happ "github.com/jhermoso/karpo-fw-go/contexts/hr/application"
	hdomain "github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
	hinfra "github.com/jhermoso/karpo-fw-go/contexts/hr/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/payroll"
	yapp "github.com/jhermoso/karpo-fw-go/contexts/payroll/application"
	ydomain "github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestFiscalContext runs Fiscal fed by Payroll on every engine: rates with validity, taxpayer with
// children, withholdings through the SQL inbox, Modelo 111 and 190 generation (dates and decimals
// in SQL), numbering counter, revert and regeneration.
func TestFiscalContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			finfra.DropAll(ctx, db)
			yinfra.DropAll(ctx, db)
			hinfra.DropAll(ctx, db)
			fcinfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), fcinfra.Migrations(), hinfra.Migrations(),
				yinfra.Migrations(), finfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			sw := hotswap.New(db)
			fac := facilities.Compose(sw)
			pm := parties.Compose(sw, nil)
			hm := hr.Compose(sw, hr.WithOrganizations(hinfra.PartiesOrganizations{Hierarchy: pm.Organizations, Membership: pm.Organizations}),
				hr.WithFacilities(hinfra.FacilitiesDirectory{Directory: fac.Directory}))
			ym := payroll.Compose(sw, yinfra.HRStaff{Staff: hm.Staff})
			fm := fiscal.Compose(sw, finfra.PartiesIdentities{TaxIdentities: pm.TaxIdentities})
			svc := fm.Service
			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}

			// Rates.
			g, err := svc.CreateRate.Handle(actx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "R10", Description: "Reducido", Rate: "10",
				Surcharge: "1.4", From: vocab.MustDate(2012, 9, 1)})
			must(err)
			gid, _ := fdomain.ParseTaxRateID(g.ID)
			_, err = svc.EndRate.Handle(actx, fapp.EndRate{ID: gid, On: vocab.MustDate(2026, 6, 30)})
			must(err)
			_, err = svc.CreateRate.Handle(actx, fapp.CreateRate{Type: "vat", Territory: "common", Code: "R10", Description: "Reducido", Rate: "10.5",
				From: vocab.MustDate(2026, 7, 1)})
			must(err)
			if r, ok, err := fm.Rates.RateOn(ctx, "vat", "common", "r10", "2026-06-30"); err != nil || !ok || r.Surcharge != "1.40" {
				t.Fatalf("rate: %+v %v %v", r, ok, err)
			}
			if r, _, _ := fm.Rates.RateOn(ctx, "vat", "common", "R10", "2026-07-01"); r.Rate != "10.50" {
				t.Fatalf("next rate: %+v", r)
			}

			// Parties, HR, Payroll.
			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			must(err)
			acmeID, _ := pdomain.ParsePartyID(acme.ID)
			_, err = pm.Service.AddIdentification.Handle(actx, papp.AddIdentification{PartyID: acmeID, DocumentType: "c0000000-0004-0000-0000-000000000003",
				Country: "ES", Number: "A58818501", Primary: true})
			must(err)
			ana, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "García",
				Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: pdomain.RelEmployment.String()}})
			must(err)
			anaID, _ := pdomain.ParsePartyID(ana.ID)
			_, err = pm.Service.AddIdentification.Handle(actx, papp.AddIdentification{PartyID: anaID, DocumentType: "c0000000-0004-0000-0000-000000000002",
				Country: "ES", Number: "12345678Z"})
			must(err)
			_, err = pm.Service.AddContact.Handle(actx, papp.AddContact{PartyID: anaID, Kind: "postal", Purposes: []string{"default"},
				Address: &papp.AddressDTO{Line1: "Sol 1", PostalCode: "28013", Locality: "Madrid", Country: "ES"}})
			must(err)
			tower, err := fac.Service.Register.Handle(actx, fcapp.RegisterFacility{Organization: acme.ID, Type: fcdomain.TypeBuilding.String(), Name: "Torre"})
			must(err)
			wc, err := hm.Service.OpenWorkCenter.Handle(actx, happ.OpenWorkCenter{Employer: acme.ID, Facility: tower.ID, Code: "28/1/01", Opened: vocab.MustDate(2020, 1, 1)})
			must(err)
			job, err := hm.Service.Hire.Handle(actx, happ.Hire{Person: ana.ID, Employer: acme.ID, Hired: vocab.MustDate(2025, 1, 15)})
			must(err)
			jobID, _ := hdomain.ParseEmploymentID(job.ID)
			_, err = hm.Service.AddContract.Handle(actx, happ.AddContract{ID: jobID, TypeCode: "100", Start: vocab.MustDate(2025, 1, 15),
				Agreement: "b3800000-0003-0000-0000-000000000005", WorkCenter: wc.ID, Primary: true})
			must(err)
			var slips []ydomain.PayslipID
			for _, month := range []int{1, 2, 3} {
				start := vocab.MustDate(2026, timeMonth(month), 1)
				p, err := ym.Service.DraftPayslip.Handle(actx, yapp.DraftPayslip{Person: ana.ID, Employer: acme.ID, Start: start, End: start.AddMonths(1).AddDays(-1)})
				must(err)
				id, _ := ydomain.ParsePayslipID(p.ID)
				slips = append(slips, id)
				_, err = ym.Service.AddLine.Handle(actx, yapp.AddLine{ID: id, Concept: "SALARIO_BASE", Amount: "2105.25"})
				must(err)
				_, err = ym.Service.AddLine.Handle(actx, yapp.AddLine{ID: id, Concept: "IRPF_GEN", Base: "2105.25", Percent: "15.5"})
				must(err)
				_, err = ym.Service.Approve.Handle(actx, yapp.ApprovePayslip{ID: id})
				must(err)
			}
			_, err = ym.Service.Cancel.Handle(actx, yapp.CancelPayslip{ID: slips[2], Reason: "error"})
			must(err)
			broker := inprocess.NewBroker()
			broker.Subscribe("fiscal", fm.Consumer)
			for {
				n, err := ym.Relay(broker).RelayOnce(ctx)
				must(err)
				if n == 0 {
					break
				}
			}

			// Taxpayer and forms.
			tp, err := svc.RegisterTaxpayer.Handle(actx, fapp.RegisterTaxpayer{Organization: acme.ID, TermsInput: fapp.TermsInput{Territory: "common",
				FiscalYearStartMonth: 1, GeneralProrata: "87.5"}})
			must(err)
			tpID, _ := fdomain.ParseTaxpayerID(tp.ID)
			for _, o := range []fapp.AddObligation{{ID: tpID, Form: "111", Periodicity: "quarterly", FromYear: 2020}, {ID: tpID, Form: "190", Periodicity: "annual", FromYear: 2020}} {
				_, err = svc.AddObligation.Handle(actx, o)
				must(err)
			}
			tp, err = svc.AddActivity.Handle(actx, fapp.AddActivity{ID: tpID, Code: "A1", Category: "services", Description: "Consultoría",
				From: vocab.MustDate(2020, 1, 1), Primary: true})
			must(err)
			got, err := svc.GetTaxpayer.Handle(actx, fapp.GetTaxpayer{Organization: acme.ID})
			if err != nil || got.GeneralProrata != "87.50" || len(got.Obligations) != 2 || len(got.Activities) != 1 || !got.Activities[0].Primary {
				t.Fatalf("taxpayer round trip: %+v %v", got, err)
			}
			// 2105.25 × 15.5 % = 326.31 per payslip; the March payslip was cancelled.
			q1, err := svc.GenerateFiling.Handle(actx, fapp.GenerateFiling{Organization: acme.ID, Form: "111", Year: 2026, Period: "1T"})
			if err != nil || q1.Recipients != 1 || q1.Perceptions != "4210.50" || q1.Withheld != "652.62" || len(q1.Problems) != 0 {
				t.Fatalf("111: %+v %v", q1, err)
			}
			q1ID, _ := fdomain.ParseFilingID(q1.ID)
			q1, err = svc.SubmitFiling.Handle(actx, fapp.SubmitFiling{ID: q1ID, Reference: "CSV"})
			if err != nil || q1.Number != 1 || q1.Status != "submitted" {
				t.Fatalf("submit: %+v %v", q1, err)
			}
			if _, err := svc.GenerateFiling.Handle(actx, fapp.GenerateFiling{Organization: acme.ID, Form: "111", Year: 2026, Period: "1T"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("slot taken: %v", err)
			}
			_, err = svc.RevertFiling.Handle(actx, fapp.RevertFiling{ID: q1ID, Reason: "complementaria"})
			must(err)
			again, err := svc.GenerateFiling.Handle(actx, fapp.GenerateFiling{Organization: acme.ID, Form: "111", Year: 2026, Period: "1T"})
			must(err)
			againID, _ := fdomain.ParseFilingID(again.ID)
			again, err = svc.SubmitFiling.Handle(actx, fapp.SubmitFiling{ID: againID})
			if err != nil || again.Number != 2 {
				t.Fatalf("second submission: %+v %v", again, err)
			}
			year, err := svc.GenerateFiling.Handle(actx, fapp.GenerateFiling{Organization: acme.ID, Form: "190", Year: 2026, Period: "0A"})
			if err != nil || len(year.Lines) != 1 || year.Lines[0].NIF != "12345678Z" || year.Lines[0].Province != "28" || year.Lines[0].Payments != 2 ||
				year.Withheld != "652.62" || len(year.Problems) != 0 {
				t.Fatalf("190 round trip: %+v %v", year, err)
			}
			yearID, _ := fdomain.ParseFilingID(year.ID)
			if _, err := svc.DiscardFiling.Handle(actx, fapp.DiscardFiling{ID: yearID}); err != nil {
				t.Fatal(err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func timeMonth(m int) time.Month { return time.Month(m) }

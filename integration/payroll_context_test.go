package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
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
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestPayrollContext runs Payroll with Parties, Facilities and HR on every engine: concept seed,
// CCC, profile with splits, payslip lines with exact decimals, derived totals, approval, the
// duplicate check (dates in SQL), remittance and cancellation.
func TestPayrollContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			yinfra.DropAll(ctx, db)
			hinfra.DropAll(ctx, db)
			finfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), finfra.Migrations(), hinfra.Migrations(), yinfra.Migrations()})
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
			svc := ym.Service

			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)

			concepts, err := svc.Concepts.Handle(actx, yapp.ListConcepts{})
			if err != nil || len(concepts) != len(ydomain.WellKnownConcepts()) {
				t.Fatalf("concept seed: %d %v", len(concepts), err)
			}
			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			if err != nil {
				t.Fatal(err)
			}
			ana, err := pm.Service.RegisterPerson.Handle(actx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "García",
				Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: pdomain.RelEmployment.String()}})
			if err != nil {
				t.Fatal(err)
			}
			tower, err := fac.Service.Register.Handle(actx, fapp.RegisterFacility{Organization: acme.ID, Type: fdomain.TypeBuilding.String(), Name: "Torre"})
			if err != nil {
				t.Fatal(err)
			}
			wc, err := hm.Service.OpenWorkCenter.Handle(actx, happ.OpenWorkCenter{Employer: acme.ID, Facility: tower.ID, Code: "28/1/01", Opened: vocab.MustDate(2020, 1, 1)})
			if err != nil {
				t.Fatal(err)
			}
			hired := vocab.MustDate(2025, 1, 15)
			job, err := hm.Service.Hire.Handle(actx, happ.Hire{Person: ana.ID, Employer: acme.ID, Number: "E-7", Hired: hired})
			if err != nil {
				t.Fatal(err)
			}
			jobID, _ := hdomain.ParseEmploymentID(job.ID)
			if _, err := hm.Service.AddContract.Handle(actx, happ.AddContract{ID: jobID, TypeCode: "100", Start: hired,
				Agreement: "b3800000-0003-0000-0000-000000000005", WorkCenter: wc.ID, Primary: true}); err != nil {
				t.Fatal(err)
			}

			ccc, err := svc.RegisterAccount.Handle(actx, yapp.RegisterAccount{Employer: acme.ID, Code: "28/1234567/42", Regime: "0111",
				Method: "direct-debit", IBAN: "ES9121000418450200051332"})
			if err != nil {
				t.Fatal(err)
			}
			prof, err := svc.OpenProfile.Handle(actx, yapp.OpenProfile{Person: ana.ID, Employer: acme.ID, TermsInput: yapp.TermsInput{
				ContributionGroup: 5, IncomeTaxRate: "15.5", EmployerAccount: ccc.ID,
				Salary: &yapp.SalaryDTO{Amount: "30000.50", Periodicity: "annual", PaymentsPerYear: 14}}})
			if err != nil {
				t.Fatal(err)
			}
			profID, _ := ydomain.ParseProfileID(prof.ID)
			for _, s := range []yapp.AddSplit{
				{ID: profID, IBAN: "ES7921000813610123456789", Percent: "30", Priority: 1, From: hired},
				{ID: profID, IBAN: "ES9121000418450200051332", Residual: true, From: hired},
			} {
				if prof, err = svc.AddSplit.Handle(actx, s); err != nil {
					t.Fatal(err)
				}
			}
			got, err := svc.GetProfile.Handle(actx, yapp.GetProfile{ID: profID})
			if err != nil || got.IncomeTaxRate != "15.50" || got.Salary.Amount != "30000.50" || len(got.Splits) != 2 || !got.Splits[1].Residual {
				t.Fatalf("profile round trip: %+v %v", got, err)
			}

			start, end := vocab.MustDate(2026, 2, 1), vocab.MustDate(2026, 2, 28)
			slip, err := svc.DraftPayslip.Handle(actx, yapp.DraftPayslip{Person: ana.ID, Employer: acme.ID, Start: start, End: end})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.DraftPayslip.Handle(actx, yapp.DraftPayslip{Person: ana.ID, Employer: acme.ID, Start: start, End: end}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("duplicate (date equality in SQL): %v", err)
			}
			slipID, _ := ydomain.ParsePayslipID(slip.ID)
			for _, l := range []yapp.AddLine{
				{Concept: "SALARIO_BASE", Amount: "2142.89"},
				{Concept: "HORAS_EXTRA", Quantity: "3.5", UnitAmount: "18.333"},
				{Concept: "SS_EMP_CG", Base: "2207.06", Percent: "4.70"},
				{Concept: "IRPF_GEN", Base: "2207.06", Percent: "15.5"},
				{Concept: "SS_ER_CC", Base: "2207.06", Percent: "23.60"},
				{Concept: "INFO_BASE_CC", Amount: "2207.06"},
			} {
				l.ID = slipID
				if slip, err = svc.AddLine.Handle(actx, l); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.Approve.Handle(actx, yapp.ApprovePayslip{ID: slipID}); err != nil {
				t.Fatal(err)
			}
			// 2142.89 + 64.17 = 2207.06; − 103.73 − 342.09 = 1761.24; employer 520.87.
			slip, err = svc.GetPayslip.Handle(actx, yapp.GetPayslip{ID: slipID})
			if err != nil || slip.Status != "approved" || slip.Totals.Gross != "2207.06" || slip.Totals.Net != "1761.24" ||
				slip.Totals.EmployerCost != "520.87" || len(slip.Lines) != 6 || slip.Lines[1].Quantity != "3.50" || slip.Lines[1].Amount != "64.17" ||
				slip.Lines[5].Code != "INFO_BASE_CC" || slip.IncomeTaxRate != "15.50" || slip.WorkCenter != wc.ID {
				t.Fatalf("payslip round trip: %+v %v", slip, err)
			}
			pays, err := ym.Remittance.NetPayments(ctx, []string{slip.ID})
			if err != nil || len(pays[slip.ID]) != 2 || pays[slip.ID][0].Amount != "528.37" || pays[slip.ID][1].Amount != "1232.87" {
				t.Fatalf("net payments: %+v %v", pays, err)
			}
			page, err := svc.SearchPayslips.Handle(actx, yapp.SearchPayslips{Employer: acme.ID, Status: "approved", From: "2026-01-01", To: "2026-02-01"})
			if err != nil || page.Total != 1 {
				t.Fatalf("search by status and period (dates in SQL): %+v %v", page, err)
			}
			if _, err := svc.Cancel.Handle(actx, yapp.CancelPayslip{ID: slipID, Reason: "error"}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.DraftPayslip.Handle(actx, yapp.DraftPayslip{Person: ana.ID, Employer: acme.ID, Start: start, End: end}); err != nil {
				t.Fatalf("a cancelled payslip frees the period: %v", err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

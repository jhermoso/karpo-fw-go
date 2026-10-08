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
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/messaging/inprocess"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestHRContext runs HR with Parties and Facilities on every engine (one database, four migration
// histories): seeded catalogs, work centers, employments with contracts, positions with holders
// and reporting lines (EXISTS / NOT EXISTS), termination cascade and the Staff port.
func TestHRContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			hinfra.DropAll(ctx, db)
			finfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), ginfra.Migrations(), finfra.Migrations(), hinfra.Migrations()})
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
			svc := hm.Service

			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)

			// Seeded catalogs, with the C# GUIDs.
			types, err := svc.PositionTypes.Handle(actx, happ.ListPositionTypes{})
			links := 0
			for _, pt := range types {
				links += len(pt.Classes)
			}
			statuses, _ := svc.PositionStatuses.Handle(actx, happ.ListPositionStatuses{})
			agreements, _ := svc.Agreements.Handle(actx, happ.ListAgreements{})
			if err != nil || len(types) != 57 || links != 23 || len(statuses) != 6 || len(agreements) != 8 {
				t.Fatalf("catalogs: %d types, %d links, %d statuses, %d agreements, %v", len(types), links, len(statuses), len(agreements), err)
			}
			var active hdomain.PositionType
			for _, pt := range types {
				if pt.Active {
					active = pt
					break
				}
			}

			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			if err != nil {
				t.Fatal(err)
			}
			it, _ := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme IT", Roles: []string{pdomain.RoleDepartment.String()}})
			if _, err := pm.Service.EstablishRelationship.Handle(actx, papp.EstablishRelationship{Type: pdomain.RelOrganizationRollup.String(), From: it.ID, To: acme.ID}); err != nil {
				t.Fatal(err)
			}
			clerk, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service,
				Permissions: []authz.Permission{authz.Wildcard}, Grants: []authz.Grant{{OrganizationID: fw.MustParseUUID(acme.ID), Level: authz.Full}}})
			cctx := authz.WithContext(ctx, clerk)
			hire := func(name, number string, hired vocab.Date) (papp.PartyDTO, happ.EmploymentDTO) {
				p, err := pm.Service.RegisterPerson.Handle(cctx, papp.RegisterPerson{GivenName: name, FirstSurname: "García",
					Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: pdomain.RelEmployment.String()}})
				if err != nil {
					t.Fatal(err)
				}
				e, err := svc.Hire.Handle(cctx, happ.Hire{Person: p.ID, Employer: acme.ID, Number: number, Hired: hired, JobCategory: "GP1"})
				if err != nil {
					t.Fatal(err)
				}
				return p, e
			}
			tower, err := fac.Service.Register.Handle(cctx, fapp.RegisterFacility{Organization: acme.ID, Type: fdomain.TypeBuilding.String(), Name: "Torre"})
			if err != nil {
				t.Fatal(err)
			}
			wc, err := svc.OpenWorkCenter.Handle(cctx, happ.OpenWorkCenter{Employer: acme.ID, Facility: tower.ID, Code: "28/1/01", Headquarters: true,
				Opened: vocab.MustDate(2020, 1, 1)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.OpenWorkCenter.Handle(cctx, happ.OpenWorkCenter{Employer: acme.ID, Facility: tower.ID, Code: "28/1/02",
				Opened: vocab.MustDate(2020, 1, 1)}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("the facility is already a work center: %v", err)
			}

			today := vocab.DateOf(fw.Now())
			hired := today.AddDays(-100)
			ana, anaJob := hire("Ana", "E-1", hired)
			bea, _ := hire("Bea", "E-2", hired)
			anaID, _ := hdomain.ParseEmploymentID(anaJob.ID)
			anaJob, err = svc.AddContract.Handle(cctx, happ.AddContract{ID: anaID, TypeCode: "100", Start: hired,
				Agreement: "b3800000-0003-0000-0000-000000000005", WorkCenter: wc.ID, WeeklyHours: "37.5", Primary: true})
			if err != nil || len(anaJob.Contracts) != 1 {
				t.Fatalf("contract: %+v %v", anaJob, err)
			}
			got, err := svc.GetEmployment.Handle(cctx, happ.GetEmployment{ID: anaID})
			if err != nil || got.Contracts[0].WeeklyHours != "37.5" || got.Contracts[0].Start != hired.String() || got.Hired != hired.String() {
				t.Fatalf("round trip: %+v %v", got, err)
			}

			from := hired.BaseTime()
			open := func(unit string) happ.PositionDTO {
				p, err := svc.OpenPosition.Handle(cctx, happ.OpenPosition{Unit: unit, Type: active.ID.String(), PlannedFrom: &from, FullTime: true})
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
			ceo, cto, dev := open(acme.ID), open(it.ID), open(it.ID)
			if cto.Organization != acme.ID {
				t.Fatalf("the organization of a unit: %+v", cto)
			}
			pid := func(p happ.PositionDTO) hdomain.PositionID { id, _ := hdomain.ParsePositionID(p.ID); return id }
			if _, err := svc.FillPosition.Handle(cctx, happ.FillPosition{ID: pid(ceo), Person: ana.ID, From: &from}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.FillPosition.Handle(cctx, happ.FillPosition{ID: pid(cto), Person: bea.ID, From: &from}); err != nil {
				t.Fatal(err)
			}
			for _, pair := range [][2]happ.PositionDTO{{cto, ceo}, {dev, cto}} {
				if _, err := svc.ReportTo.Handle(cctx, happ.ReportTo{ID: pid(pair[0]), Supervisor: pair[1].ID, Primary: true, From: &from}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.ReportTo.Handle(cctx, happ.ReportTo{ID: pid(ceo), Supervisor: dev.ID, Primary: true, From: &from}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("cycle: %v", err)
			}
			chart, err := svc.OrgChart.Handle(cctx, happ.GetOrgChart{Root: pid(ceo), Depth: 5})
			if err != nil || len(chart.Reports) != 1 || len(chart.Reports[0].Reports) != 1 || chart.Reports[0].Reports[0].Position.ID != dev.ID {
				t.Fatalf("chart: %+v %v", chart, err)
			}
			vacant, err := svc.SearchPositions.Handle(cctx, happ.SearchPositions{Organization: acme.ID, VacantOnly: true})
			if err != nil || vacant.Total != 1 || vacant.Items[0].ID != dev.ID {
				t.Fatalf("vacant (NOT EXISTS): %+v %v", vacant, err)
			}
			held, err := svc.SearchPositions.Handle(cctx, happ.SearchPositions{Holder: bea.ID})
			if err != nil || held.Total != 1 || held.Items[0].ID != cto.ID {
				t.Fatalf("held by (EXISTS): %+v %v", held, err)
			}

			if _, err := svc.Terminate.Handle(cctx, happ.Terminate{ID: anaID, On: today.AddDays(-1), Reason: "baja"}); err != nil {
				t.Fatal(err)
			}
			after, err := svc.GetPosition.Handle(cctx, happ.GetPosition{ID: pid(ceo)})
			if err != nil || !after.Vacant || after.Holder != "" || after.Holders[0].Thru == nil {
				t.Fatalf("termination vacates: %+v %v", after, err)
			}
			staff, err := hm.Staff.EmploymentsOn(ctx, []string{ana.ID, bea.ID}, hired.String())
			if err != nil || len(staff[ana.ID]) != 1 || staff[ana.ID][0].Contract == nil || staff[ana.ID][0].Contract.WeeklyHours != "37.5" ||
				len(staff[bea.ID]) != 1 || staff[bea.ID][0].Contract != nil {
				t.Fatalf("staff: %+v %v", staff, err)
			}
			// Parties reacts to hr.employee-terminated.v1 through its inbox (decision 4 of docs/RRHH.md).
			broker := inprocess.NewBroker()
			broker.Subscribe("parties", pm.Consumer)
			for {
				n, err := hm.Relay(broker).RelayOnce(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if n == 0 {
					break
				}
			}
			orgs, err := pm.Organizations.InternalOrganizations(ctx, []string{ana.ID, bea.ID})
			if err != nil || len(orgs[ana.ID]) != 0 || len(orgs[bea.ID]) != 1 {
				t.Fatalf("affiliations after the termination: %+v %v", orgs, err)
			}
			wcID, _ := hdomain.ParseWorkCenterID(wc.ID)
			if closed, err := svc.CloseWorkCenter.Handle(cctx, happ.CloseWorkCenter{ID: wcID, On: today}); err != nil || closed.Closed != today.String() || closed.Headquarters {
				t.Fatalf("close: %+v %v", closed, err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

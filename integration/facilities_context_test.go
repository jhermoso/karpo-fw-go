package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/facilities"
	fapp "github.com/jhermoso/karpo-fw-go/contexts/facilities/application"
	fdomain "github.com/jhermoso/karpo-fw-go/contexts/facilities/domain"
	finfra "github.com/jhermoso/karpo-fw-go/contexts/facilities/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/geography"
	gapp "github.com/jhermoso/karpo-fw-go/contexts/geography/application"
	gdomain "github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	ginfra "github.com/jhermoso/karpo-fw-go/contexts/geography/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	pdomain "github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	pinfra "github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TestFacilitiesContext runs Facilities with Parties and Geography on every engine (one database,
// three migration histories): located office, hierarchy, scope, facility roles in Parties.
func TestFacilitiesContext(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			finfra.DropAll(ctx, db)
			ginfra.DropAll(ctx, db)
			dropPartiesTables(ctx, db)
			m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{pinfra.Migrations(), ginfra.Migrations(), finfra.Migrations()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}

			sw := hotswap.New(db)
			geo := geography.Compose(sw)
			fac := facilities.Compose(sw, facilities.WithAddressChecker(finfra.GeographyAddresses{Checker: geo.Ports}))
			pm := parties.Compose(sw, nil, parties.WithFacilityDirectory(pinfra.FacilitiesDirectory{Directory: fac.Directory}))

			admin, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "admin", Kind: authz.Service, Permissions: []authz.Permission{authz.Wildcard}})
			admin.GlobalAdmin = true
			actx := authz.WithContext(ctx, admin)
			acme, err := pm.Service.RegisterOrganization.Handle(actx, papp.RegisterOrganization{LegalName: "Acme", Roles: []string{pdomain.RoleInternalOrganization.String()}})
			if err != nil {
				t.Fatal(err)
			}
			clerk, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service,
				Permissions: []authz.Permission{authz.Wildcard, fapp.PermRead, gapp.PermBoundaryRead},
				Grants:      []authz.Grant{{OrganizationID: fw.MustParseUUID(acme.ID), Level: authz.Full}}})
			cctx := authz.WithContext(ctx, clerk)
			ana, err := pm.Service.RegisterPerson.Handle(cctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "García",
				Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: pdomain.RelEmployment.String()}})
			if err != nil {
				t.Fatal(err)
			}
			towns, _ := geo.Service.SearchBoundaries.Handle(cctx, gapp.SearchBoundaries{Text: "madrid", Type: gdomain.TypeMunicipality.String()})
			var madrid string
			for _, b := range towns.Items {
				if b.Name == "Madrid" {
					madrid = b.ID
				}
			}
			office, err := fac.Service.Register.Handle(cctx, fapp.RegisterFacility{Organization: acme.ID, Type: fdomain.TypeOffice.String(),
				Name: "Oficina Sol", AreaM2: "250.75", Location: &fapp.LocationDTO{Line1: "Puerta del Sol 1", PostalCode: "28013",
					Locality: "Madrid", Country: "ES", GeoBoundary: madrid, Phone: "+34 910 000 000", Email: "sol@acme.test"}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := fac.Service.Get.Handle(cctx, fapp.GetFacility{ID: fdomain.FacilityID{UUID: fw.MustParseUUID(office.ID)}})
			if err != nil || got.AreaM2 != "250.75" || got.Location.GeoPostalCode == "" || got.Location.Email != "sol@acme.test" {
				t.Fatalf("round trip: %+v %v", got, err)
			}
			room, err := fac.Service.Register.Handle(cctx, fapp.RegisterFacility{Organization: acme.ID, Type: fdomain.TypeRoom.String(),
				Name: "Sala 1", PartOf: office.ID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fac.Service.Move.Handle(cctx, fapp.MoveFacility{ID: fdomain.FacilityID{UUID: fw.MustParseUUID(office.ID)}, PartOf: room.ID}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("cycle: %v", err)
			}
			parts, err := fac.Service.Search.Handle(cctx, fapp.SearchFacilities{PartOf: office.ID})
			if err != nil || parts.Total != 1 {
				t.Fatalf("parts (nullable part_of in SQL): %+v %v", parts, err)
			}
			anaID, _ := pdomain.ParsePartyID(ana.ID)
			if _, err := pm.Service.AssignFacilityRole.Handle(cctx, papp.AssignFacilityRole{PartyID: anaID, Facility: office.ID,
				RoleType: pdomain.FacilityWorkCenter.String()}); err != nil {
				t.Fatal(err)
			}
			staff, err := pm.Service.Search.Handle(cctx, papp.SearchParties{Facility: office.ID})
			if err != nil || staff.Total != 1 || staff.Items[0].ID != ana.ID {
				t.Fatalf("staff: %+v %v", staff, err)
			}
			refs, err := fac.Directory.Resolve(ctx, []string{office.ID, room.ID})
			if err != nil || len(refs) != 2 || refs[office.ID].Organization != acme.ID {
				t.Fatalf("directory: %+v %v", refs, err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

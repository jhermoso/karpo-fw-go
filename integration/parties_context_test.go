package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties"
	papp "github.com/jhermoso/karpo-fw-go/contexts/parties/application"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/parties/infrastructure"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
)

// TestPartiesContext runs the Parties bounded context on every engine: migrations with the
// seeded catalogs (same GUIDs as C#, also in .NET byte order on Oracle), roles with validity
// translated to SQL (EXISTS + nullable end), relationships, directory and audit.
func TestPartiesContext(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			db := open(t, e)
			ctx := context.Background()
			dropPartiesTables(ctx, db)
			m, err := infrastructure.Migrator(db)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Verify(ctx); !errors.Is(err, application.ErrSchemaOutdated) {
				t.Fatalf("empty database: %v", err)
			}
			if _, err := m.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := m.Verify(ctx); err != nil {
				t.Fatal(err)
			}

			ac, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "migrator", Kind: authz.Service,
				Permissions: []authz.Permission{authz.Wildcard}, EffectiveOrganizations: []fw.UUID{}})
			ac.GlobalAdmin = true
			ctx = authz.WithContext(ctx, ac)

			sw := hotswap.New(db)
			mod := parties.Compose(sw, nil)
			svc := mod.Service

			types, err := svc.ListRoleTypes.Handle(ctx, papp.ListRoleTypes{})
			if err != nil || len(types) != len(domain.WellKnownRoleTypes()) {
				t.Fatalf("seeded role types: %d %v", len(types), err)
			}
			acme, err := svc.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme Sociedad Anónima", TradeName: "Acme",
				Roles: []string{domain.RoleInternalOrganization.String()}})
			if err != nil {
				t.Fatal(err)
			}
			ana, err := svc.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Ana", FirstSurname: "García", SecondSurname: "López",
				Gender: "female", BirthDate: "1990-05-17", MaritalStatus: "married", Roles: []string{domain.RoleEmployee.String()}})
			if err != nil {
				t.Fatal(err)
			}
			luis, err := svc.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Luis", FirstSurname: "Pérez"})
			if err != nil {
				t.Fatal(err)
			}
			anaID, _ := domain.ParsePartyID(ana.ID)
			luisID, _ := domain.ParsePartyID(luis.ID)

			past := fw.Now().Add(-48 * time.Hour)
			yesterday := fw.Now().Add(-24 * time.Hour)
			// Luis was a customer until yesterday; Ana is a bill-to customer now.
			if _, err := svc.AssignRole.Handle(ctx, papp.AssignRole{PartyID: luisID, RoleType: domain.RoleCustomer.String(), From: &past}); err != nil {
				t.Fatal(err)
			}
			luisDTO, _ := svc.Get.Handle(ctx, papp.GetParty{ID: luisID})
			roleID, _ := domain.ParsePartyRoleID(luisDTO.Roles[0].ID)
			if _, err := svc.EndRole.Handle(ctx, papp.EndRole{PartyID: luisID, RoleID: roleID, At: &yesterday}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.AssignRole.Handle(ctx, papp.AssignRole{PartyID: anaID, RoleType: domain.RoleBillToCustomer.String()}); err != nil {
				t.Fatal(err)
			}

			customers, err := svc.Search.Handle(ctx, papp.SearchParties{Role: domain.RoleCustomer.String()})
			if err != nil || customers.Total != 1 || customers.Items[0].ID != ana.ID {
				t.Fatalf("current customers (inheritance + validity in SQL): %+v %v", customers, err)
			}
			people, err := svc.Search.Handle(ctx, papp.SearchParties{Kind: "person", Text: "garcía"})
			if err != nil || people.Total != 1 {
				t.Fatalf("people named garcía: %+v %v", people, err)
			}
			got, err := svc.Get.Handle(ctx, papp.GetParty{ID: anaID})
			if err != nil || got.Person.BirthDate != "1990-05-17" || got.Person.SecondSurname != "López" || len(got.Roles) != 2 {
				t.Fatalf("round trip: %+v %v", got, err)
			}

			rel, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelEmployment.String(), From: ana.ID, To: acme.ID})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelEmployment.String(), From: ana.ID, To: acme.ID}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("duplicate: %v", err)
			}
			relID, _ := domain.ParseRelationshipID(rel.ID)
			if _, err := svc.TerminateRelationship.Handle(ctx, papp.TerminateRelationship{ID: relID}); err != nil {
				t.Fatal(err)
			}
			all, err := svc.Relationships.Handle(ctx, papp.PartyRelationships{PartyID: anaID})
			if err != nil || len(all) != 1 || all[0].Until == nil {
				t.Fatalf("relationships: %+v %v", all, err)
			}
			active, err := svc.Relationships.Handle(ctx, papp.PartyRelationships{PartyID: anaID, ActiveOnly: true})
			if err != nil || len(active) != 0 {
				t.Fatalf("active relationships: %+v %v", active, err)
			}

			refs, err := mod.Directory.Resolve(ctx, []string{ana.ID, acme.ID, fw.NewUUID().String()})
			if err != nil || len(refs) != 2 || refs[acme.ID].Name != "Acme" {
				t.Fatalf("directory: %+v %v", refs, err)
			}
			trail, err := mod.Audit.Trail(ctx, domain.PartyKind, ana.ID)
			if err != nil || len(trail) != 2 {
				t.Fatalf("audit: %d %v", len(trail), err)
			}
			pending, err := mod.IntegrationOutbox.Pending(ctx, 100, 10)
			if err != nil || len(pending) < 8 {
				t.Fatalf("published language: %d %v", len(pending), err)
			}
		})
	}
}

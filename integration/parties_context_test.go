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
			// Phase 2: identifications, contacts and classifications.
			opts, err := svc.DocumentOptions.Handle(ctx, papp.DocumentOptions{Country: "ES"})
			if err != nil || len(opts) != 5 || opts[0].Code != "NIDN" {
				t.Fatalf("document options from SQL: %+v %v", opts, err)
			}
			if _, err := svc.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: anaID, DocumentType: domain.DocNationalID.String(),
				Country: "ES", Number: "12345678-Z"}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: anaID, DocumentType: domain.DocPassport.String(),
				Country: "ES", Number: "PAA123456", IssuedOn: "2021-02-03", ExpiresOn: "2031-02-02", IssuingAuthority: "Policía Nacional", Primary: true}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.AddIdentification.Handle(ctx, papp.AddIdentification{PartyID: luisID, DocumentType: domain.DocNationalID.String(),
				Country: "ES", Number: "12345678Z"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("document taken: %v", err)
			}
			boundary := fw.NewUUID()
			if _, err := svc.AddContact.Handle(ctx, papp.AddContact{PartyID: anaID, Kind: "postal", Purposes: []string{"billing"},
				Address: &papp.AddressDTO{StreetType: "CL", Line1: "Mayor 1", PostalCode: "28013", Locality: "Madrid", Country: "ES",
					GeoBoundary: boundary.String()}}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.AddContact.Handle(ctx, papp.AddContact{PartyID: anaID, Kind: "phone", Value: "+34 600 000 001", Purposes: []string{"default"}}); err != nil {
				t.Fatal(err)
			}
			acmeID, _ := domain.ParsePartyID(acme.ID)
			if _, err := svc.Classify.Handle(ctx, papp.Classify{PartyID: acmeID, Classification: domain.ClassCorporate.String()}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Classify.Handle(ctx, papp.Classify{PartyID: acmeID, Classification: domain.ClassRetail.String()}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("exclusive family: %v", err)
			}
			got, err = svc.Get.Handle(ctx, papp.GetParty{ID: anaID})
			if err != nil || len(got.Identifications) != 2 || got.Identifications[1].ExpiresOn != "2031-02-02" || !got.Identifications[1].Primary ||
				got.Identifications[0].Primary || len(got.Contacts) != 2 || got.Contacts[0].Address == nil ||
				got.Contacts[0].Address.GeoBoundary != boundary.String() || got.Contacts[1].Value != "+34600000001" {
				t.Fatalf("phase 2 round trip: %+v %v", got, err)
			}
			byDoc, err := svc.Search.Handle(ctx, papp.SearchParties{Document: "paa123456"})
			if err != nil || byDoc.Total != 1 || byDoc.Items[0].ID != ana.ID {
				t.Fatalf("search by document: %+v %v", byDoc, err)
			}
			byClass, err := svc.Search.Handle(ctx, papp.SearchParties{Classification: domain.ClassCorporate.String()})
			if err != nil || byClass.Total != 1 || byClass.Items[0].ID != acme.ID {
				t.Fatalf("search by classification: %+v %v", byClass, err)
			}

			pending, err := mod.IntegrationOutbox.Pending(ctx, 100, 10)
			if err != nil || len(pending) < 8 {
				t.Fatalf("published language: %d %v", len(pending), err)
			}
		})
	}
}

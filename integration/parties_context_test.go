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
			if err != nil || len(trail) != 4 { // registered, role, affiliated by the employment, affiliation ended
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

			// Phase 3: organization scope (P1), registration in scope, hierarchy and ports.
			clerk, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service,
				Permissions: []authz.Permission{authz.Wildcard}, Grants: []authz.Grant{{OrganizationID: acmeID.UUID, Level: authz.Full}}})
			clerkCtx := authz.WithContext(context.Background(), clerk)
			pedro, err := svc.RegisterPerson.Handle(clerkCtx, papp.RegisterPerson{GivenName: "Pedro", FirstSurname: "Ruiz",
				Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: domain.RelCustomer.String()}})
			if err != nil || len(pedro.Organizations) != 1 || pedro.Organizations[0] != acme.ID {
				t.Fatalf("register in scope: %+v %v", pedro, err)
			}
			seen, err := svc.Search.Handle(clerkCtx, papp.SearchParties{Kind: "person"})
			if err != nil || seen.Total != 1 || seen.Items[0].ID != pedro.ID { // ana's employment was terminated; luis never related
				names := []string{}
				for _, p := range seen.Items {
					names = append(names, p.Name)
				}
				t.Fatalf("scoped search (affiliations in SQL): %v %v", names, err)
			}
			if _, err := svc.Get.Handle(clerkCtx, papp.GetParty{ID: luisID}); !errors.Is(err, fw.ErrNotFound) {
				t.Fatalf("out of scope must be not found: %v", err)
			}
			sales, err := svc.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme Sales", LegalForm: "corporation",
				Roles: []string{domain.RoleDivision.String()}})
			if err != nil || sales.Organization.LegalForm != "corporation" {
				t.Fatalf("legal form round trip: %+v %v", sales, err)
			}
			madrid, _ := svc.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Acme Madrid", Roles: []string{domain.RoleDepartment.String()}})
			for _, pair := range [][2]string{{sales.ID, acme.ID}, {madrid.ID, sales.ID}} {
				if _, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelOrganizationRollup.String(), From: pair[0], To: pair[1]}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelOrganizationRollup.String(), From: sales.ID, To: madrid.ID}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("cycle: %v", err)
			}
			desc, err := mod.Organizations.Descendants(ctx, []string{acme.ID})
			if err != nil || len(desc) != 3 {
				t.Fatalf("descendants: %v %v", desc, err)
			}
			of, err := mod.Organizations.InternalOrganizationOf(ctx, []string{madrid.ID})
			if err != nil || of[madrid.ID].ID != acme.ID {
				t.Fatalf("internal organization of: %+v %v", of, err)
			}
			member, err := mod.Organizations.InternalOrganizations(ctx, []string{pedro.ID, luis.ID})
			if err != nil || len(member[pedro.ID]) != 1 || len(member[luis.ID]) != 0 {
				t.Fatalf("membership: %+v %v", member, err)
			}

			// Details by relationship type (docs/PARTIES-UDM.md): the codes read from SQL, the
			// prospect relationship added by migration 11 and its trial in a nullable column.
			relTypes, err := svc.ListRelationshipTypes.Handle(ctx, papp.ListRelationshipTypes{})
			if err != nil || len(relTypes) != len(domain.WellKnownRelationshipTypes()) {
				t.Fatalf("seeded relationship types: %d %v", len(relTypes), err)
			}
			for _, rt := range relTypes {
				if rt.Code == "" || (rt.ID == domain.RelProspect.String()) != (rt.Code == domain.CodeProspect) {
					t.Fatalf("relationship type code: %+v", rt)
				}
			}
			until := fw.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
			flotas, err := svc.RegisterOrganization.Handle(clerkCtx, papp.RegisterOrganization{LegalName: "Flotas Ana",
				Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: domain.RelProspect.String(),
					RelationshipDetailsInput: papp.RelationshipDetailsInput{Prospect: &papp.ProspectInput{TrialUntil: &until}}}})
			if err != nil || len(flotas.Roles) != 1 || flotas.Roles[0].Name != "Prospect" {
				t.Fatalf("register a prospect with a trial: %+v %v", flotas, err)
			}
			flotasID, _ := domain.ParsePartyID(flotas.ID)
			prospects, err := svc.Relationships.Handle(clerkCtx, papp.PartyRelationships{PartyID: flotasID})
			if err != nil || len(prospects) != 1 || prospects[0].Prospect == nil || !prospects[0].Prospect.InTrial ||
				!prospects[0].Prospect.TrialUntil.Equal(until) {
				t.Fatalf("trial round trip: %+v %v", prospects, err)
			}
			prospectID, _ := domain.ParseRelationshipID(prospects[0].ID)
			longer := until.Add(15 * 24 * time.Hour)
			if _, err := svc.SetProspectTrial.Handle(clerkCtx, papp.SetProspectTrial{ID: prospectID, TrialUntil: &longer}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SetProspectTrial.Handle(clerkCtx, papp.SetProspectTrial{ID: relID, TrialUntil: &longer}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("an employment has no trial: %v", err)
			}
			trials, err := mod.Trials.Trials(ctx, acme.ID, []string{flotas.ID, pedro.ID})
			if err != nil || len(trials) != 1 || !trials[flotas.ID].InForce || !trials[flotas.ID].Until.Equal(longer) {
				t.Fatalf("trials port: %+v %v", trials, err)
			}
			relationships := hotswap.Repository(sw, infrastructure.RelationshipRepositoryFactory)
			inTrial, err := relationships.Find(ctx, domain.InTrialAt(until.Add(time.Hour)))
			if err != nil || len(inTrial) != 1 || inTrial[0].ID() != prospectID {
				t.Fatalf("in trial (nullable end in SQL): %d %v", len(inTrial), err)
			}
			if late, err := relationships.Find(ctx, domain.InTrialAt(longer.Add(time.Hour))); err != nil || len(late) != 0 {
				t.Fatalf("after the trial: %d %v", len(late), err)
			}
			withdrawn, err := svc.SetProspectTrial.Handle(clerkCtx, papp.SetProspectTrial{ID: prospectID})
			if err != nil || withdrawn.Prospect.TrialUntil != nil || withdrawn.Prospect.InTrial || withdrawn.Version != 3 {
				t.Fatalf("withdrawn trial: %+v %v", withdrawn, err)
			}

			// The share of an ownership relationship, an exact decimal in a nullable text column.
			if _, err := svc.AssignRole.Handle(ctx, papp.AssignRole{PartyID: luisID, RoleType: domain.RoleShareholder.String()}); err != nil {
				t.Fatal(err)
			}
			third := "33.33"
			owns, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelOwnership.String(), From: luis.ID, To: acme.ID,
				RelationshipDetailsInput: papp.RelationshipDetailsInput{Ownership: &papp.OwnershipInput{Share: &third}}})
			if err != nil || owns.Ownership == nil || owns.Ownership.Share != "33.33" {
				t.Fatalf("ownership with share: %+v %v", owns, err)
			}
			ownsID, _ := domain.ParseRelationshipID(owns.ID)
			half := "50"
			if _, err := svc.SetOwnershipShare.Handle(clerkCtx, papp.SetOwnershipShare{ID: ownsID, Share: &half}); err != nil {
				t.Fatal(err)
			}
			luisRels, err := svc.Relationships.Handle(ctx, papp.PartyRelationships{PartyID: luisID})
			if err != nil || len(luisRels) != 1 || luisRels[0].Ownership == nil || luisRels[0].Ownership.Share != "50.00" || luisRels[0].Prospect != nil {
				t.Fatalf("share round trip: %+v %v", luisRels, err)
			}
			if _, err := svc.SetOwnershipShare.Handle(clerkCtx, papp.SetOwnershipShare{ID: prospectID, Share: &half}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("a prospect relationship has no share: %v", err)
			}

			// The personal details of a person are edited after registration.
			edited, err := svc.UpdatePerson.Handle(clerkCtx, papp.UpdatePerson{ID: luisID, Gender: "male", BirthDate: "1988-02-29", MaritalStatus: "single"})
			if err != nil || edited.Person.BirthDate != "1988-02-29" || edited.Person.Gender != "male" || edited.Person.MaritalStatus != "single" {
				t.Fatalf("person details round trip: %+v %v", edited.Person, err)
			}

			pending, err := mod.IntegrationOutbox.Pending(ctx, 100, 10)
			if err != nil || len(pending) < 8 {
				t.Fatalf("published language: %d %v", len(pending), err)
			}
		})
	}
}

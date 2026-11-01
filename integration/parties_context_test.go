package integration

import (
	"context"
	"errors"
	"slices"
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
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// TestPartiesContext ejecuta el contexto delimitado Parties en cada motor: migraciones con los
// catálogos sembrados (los mismos GUID que en C#, también en el orden de bytes de .NET en Oracle),
// roles con vigencia traducidos a SQL (EXISTS + fin que admite NULL), relaciones, directorio y auditoría.
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

			// El escenario corre con el reloj falso del framework: el reloj del sistema avanza a saltos
			// (de hasta 15,6 ms en Windows), así que dos casos de uso podrían compartir un instante o
			// no según la ejecución, y el empleo de más abajo terminaría con o sin duración. Empieza
			// en un segundo exacto, que todos los motores guardan sin pérdida: un instante releído es
			// igual al del reloj.
			clock := fake.New(time.Now().Truncate(time.Second))
			t.Cleanup(fw.SetClock(clock))

			sw := hotswap.New(db)
			mod := parties.Compose(sw, nil)
			svc := mod.Service

			types, err := svc.ListRoleTypes.Handle(ctx, papp.ListRoleTypes{})
			if err != nil || len(types) != len(domain.WellKnownRoleTypes()) {
				t.Fatalf("seeded role types: %d %v", len(types), err)
			}
			// Migración 16: cada tipo de rol del programa tiene su código, y se puede añadir uno.
			for _, rt := range types {
				if rt.Code != domain.CodeOf(rt.Name) || !rt.WellKnown {
					t.Fatalf("role type %s: code %q", rt.Name, rt.Code)
				}
			}
			guarantor, err := svc.DefineRoleType.Handle(ctx, papp.DefineRoleType{Name: "Guarantor", Code: "guarantor", Parent: domain.RolePersonCategory.String()})
			if err != nil || guarantor.Applicability != "person" || guarantor.WellKnown {
				t.Fatalf("adding a role type: %+v %v", guarantor, err)
			}
			if _, err := svc.DefineRoleType.Handle(ctx, papp.DefineRoleType{Name: "Avalista", Code: "guarantor", Parent: domain.RolePersonCategory.String()}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("a code already taken: %v", err)
			}
			if types, err = svc.ListRoleTypes.Handle(ctx, papp.ListRoleTypes{}); err != nil || len(types) != len(domain.WellKnownRoleTypes())+1 {
				t.Fatalf("role types after adding one: %d %v", len(types), err)
			}
			guarantee, err := svc.DefineRelationshipType.Handle(ctx, papp.DefineRelationshipType{Name: "Guarantee", Code: "guarantee",
				FromRole: guarantor.ID, ToRole: domain.RoleCustomer.String()})
			if err != nil || guarantee.WellKnown || guarantee.Hierarchical || guarantee.FromRole != guarantor.ID {
				t.Fatalf("adding a relationship type: %+v %v", guarantee, err)
			}
			if _, err := svc.DefineRelationshipType.Handle(ctx, papp.DefineRelationshipType{Name: "Aval", Code: "guarantee",
				FromRole: guarantor.ID, ToRole: domain.RoleCustomer.String()}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("a relationship type code already taken: %v", err)
			}
			if all, err := svc.ListRelationshipTypes.Handle(ctx, papp.ListRelationshipTypes{}); err != nil || len(all) != len(domain.WellKnownRelationshipTypes())+1 {
				t.Fatalf("relationship types after adding one: %d %v", len(all), err)
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
			// Luis fue cliente hasta ayer; Ana es ahora cliente de facturación.
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
			clock.Advance(time.Millisecond) // el empleo dura un momento
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
			if err != nil || len(trail) != 4 { // alta, rol, afiliada por el empleo, afiliación terminada
				t.Fatalf("audit: %d %v", len(trail), err)
			}
			// Fase 2: identificaciones, contactos y clasificaciones.
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
			// Migración 25: el perfil del autónomo va en su rol Self Employed y se relee igual (D-85).
			luisSelf, err := svc.AssignRole.Handle(ctx, papp.AssignRole{PartyID: luisID, RoleType: domain.RoleSelfEmployed.String()})
			if err != nil {
				t.Fatal(err)
			}
			selfRole := luisSelf.Roles[slices.IndexFunc(luisSelf.Roles, func(r papp.RoleDTO) bool { return r.RoleType == domain.RoleSelfEmployed.String() })].ID
			selfRoleID, _ := domain.ParsePartyRoleID(selfRole)
			if _, err := svc.SetSelfEmployed.Handle(ctx, papp.SetSelfEmployedDetails{PartyID: luisID, RoleID: selfRoleID,
				TradeName: "Fontanería Pérez", ActivityCode: "504.1", RegisteredOn: "2019-07-01"}); err != nil {
				t.Fatal(err)
			}
			readSelf := func() *papp.SelfEmployedDTO {
				t.Helper()
				p, err := svc.Get.Handle(ctx, papp.GetParty{ID: luisID})
				if err != nil {
					t.Fatal(err)
				}
				return p.Roles[slices.IndexFunc(p.Roles, func(r papp.RoleDTO) bool { return r.ID == selfRole })].SelfEmployed
			}
			if got := readSelf(); got == nil || *got != (papp.SelfEmployedDTO{TradeName: "Fontanería Pérez", ActivityCode: "504.1", RegisteredOn: "2019-07-01"}) {
				t.Fatalf("self-employed profile round trip: %+v", got)
			}
			if _, err := svc.SetSelfEmployed.Handle(ctx, papp.SetSelfEmployedDetails{PartyID: luisID, RoleID: selfRoleID}); err != nil {
				t.Fatal(err)
			}
			if got := readSelf(); got != nil {
				t.Fatalf("an empty profile is stored as nulls: %+v", got)
			}
			boundary := fw.NewUUID()
			if _, err := svc.AddContact.Handle(ctx, papp.AddContact{PartyID: anaID, Kind: "postal", Purposes: []string{"billing"},
				Address: &papp.AddressDTO{StreetType: "CL", Line1: "Mayor", Number: "1", NumberQualifier: "bis", Block: "2", Portal: "A", Staircase: "B",
					Floor: "3", Door: "izquierda", PostalCode: "28013", Locality: "Madrid", Country: "ES",
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
				got.Contacts[0].Address.GeoBoundary != boundary.String() || got.Contacts[1].Value != "+34600000001" ||
				got.Contacts[0].Address.Number != "1" || got.Contacts[0].Address.NumberQualifier != "bis" || got.Contacts[0].Address.Block != "2" ||
				got.Contacts[0].Address.Portal != "A" || got.Contacts[0].Address.Staircase != "B" || got.Contacts[0].Address.Floor != "3" ||
				got.Contacts[0].Address.Door != "izquierda" {
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
			// Migración 28: un e-mail con formato erróneo se guarda con su incidencia, que se relee, se
			// encuentra con el filtro y se va al terminar el contacto (D-92).
			sloppy, err := svc.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Datos Sucios SL"})
			if err != nil {
				t.Fatal(err)
			}
			sloppyID, _ := domain.ParsePartyID(sloppy.ID)
			bad, err := svc.AddContact.Handle(ctx, papp.AddContact{PartyID: sloppyID, Kind: "email", Value: " info@sucios "})
			if err != nil || len(bad.Warnings) != 1 || bad.Warnings[0].Code != domain.IssueInvalidEmail {
				t.Fatalf("a badly formed e-mail: %+v %v", bad.Warnings, err)
			}
			got, err = svc.Get.Handle(ctx, papp.GetParty{ID: sloppyID})
			if err != nil || len(got.DataQualityIssues) != 1 || got.DataQualityIssues[0].Value != "info@sucios" ||
				got.DataQualityIssues[0].Subject != got.Contacts[0].ID || got.DataQualityIssues[0].Since.IsZero() {
				t.Fatalf("the issue read back: %+v %v", got.DataQualityIssues, err)
			}
			withIssues, err := svc.Search.Handle(ctx, papp.SearchParties{WithDataQualityIssues: true})
			if err != nil || withIssues.Total != 1 || withIssues.Items[0].ID != sloppy.ID {
				t.Fatalf("search with data quality issues: %+v %v", withIssues, err)
			}
			badMail, _ := domain.ParseContactID(got.Contacts[0].ID)
			if got, err = svc.EndContact.Handle(ctx, papp.EndContact{PartyID: sloppyID, ContactID: badMail}); err != nil || len(got.DataQualityIssues) != 0 {
				t.Fatalf("ending the contact clears its issue: %+v %v", got.DataQualityIssues, err)
			}
			if withIssues, err = svc.Search.Handle(ctx, papp.SearchParties{WithDataQualityIssues: true}); err != nil || withIssues.Total != 0 {
				t.Fatalf("nobody with issues: %+v %v", withIssues, err)
			}
			// Migración 30: identidad y perfil de cada empresa (D-122).
			partiesD122(ctx, t, svc, acme.ID)

			// Fase 3: ámbito de organización (P1), alta dentro del ámbito, jerarquía y puertos.
			clerk, _ := authz.NewContext(authz.Context{Subject: fw.NewUUID(), SubjectName: "clerk", Kind: authz.Service,
				Permissions: []authz.Permission{authz.Wildcard}, Grants: []authz.Grant{{OrganizationID: acmeID.UUID, Level: authz.Full}}})
			clerkCtx := authz.WithContext(context.Background(), clerk)
			pedro, err := svc.RegisterPerson.Handle(clerkCtx, papp.RegisterPerson{GivenName: "Pedro", FirstSurname: "Ruiz",
				Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: domain.RelCustomer.String()}})
			if err != nil || len(pedro.Organizations) != 1 || pedro.Organizations[0] != acme.ID {
				t.Fatalf("register in scope: %+v %v", pedro, err)
			}
			// Migración 18: la marca de un documento de identidad rechazado, y la búsqueda por ella.
			flagged, err := svc.FlagDocumentIssue.Handle(clerkCtx, papp.FlagDocumentIssue{ID: mustParty(t, pedro.ID), Number: "12345678A",
				Reason: "the DNI does not pass its check digits"})
			if err != nil || flagged.DocumentIssue == nil || flagged.DocumentIssue.Number != "12345678A" {
				t.Fatalf("marking a refused document: %+v %v", flagged.DocumentIssue, err)
			}
			marked, err := svc.Search.Handle(clerkCtx, papp.SearchParties{DocumentIssue: true})
			if err != nil || marked.Total != 1 || marked.Items[0].ID != pedro.ID || marked.Items[0].DocumentIssue == nil {
				t.Fatalf("parties with a refused document: %+v %v", marked.Items, err)
			}
			if cleared, err := svc.ClearDocumentIssue.Handle(clerkCtx, papp.ClearDocumentIssue{ID: mustParty(t, pedro.ID)}); err != nil || cleared.DocumentIssue != nil {
				t.Fatalf("taking the mark away: %+v %v", cleared.DocumentIssue, err)
			}
			if marked, err = svc.Search.Handle(clerkCtx, papp.SearchParties{DocumentIssue: true}); err != nil || marked.Total != 0 {
				t.Fatalf("nobody is marked now: %d %v", marked.Total, err)
			}

			// Migración 17: el número de cliente es un detalle de la relación de cliente.
			pedroID, _ := domain.ParsePartyID(pedro.ID)
			buys, err := svc.Relationships.Handle(clerkCtx, papp.PartyRelationships{PartyID: pedroID, ActiveOnly: true})
			if err != nil || len(buys) != 1 || buys[0].Customer == nil || buys[0].Customer.Number != "" {
				t.Fatalf("pedro's customer relationship: %+v %v", buys, err)
			}
			buysID, _ := domain.ParseRelationshipID(buys[0].ID)
			numbered, err := svc.SetCustomerNumber.Handle(clerkCtx, papp.SetCustomerNumber{ID: buysID, CustomerNumber: " c-4300001 "})
			if err != nil || numbered.Customer.Number != "C-4300001" {
				t.Fatalf("numbering a customer: %+v %v", numbered, err)
			}
			if again, err := svc.Relationships.Handle(clerkCtx, papp.PartyRelationships{PartyID: pedroID}); err != nil || again[0].Customer.Number != "C-4300001" {
				t.Fatalf("the customer number read back: %+v %v", again, err)
			}
			// Migración 27: la relación de negocio que pasa a la superviviente al fusionar recuerda
			// de cuál viene, y deshacer la fusión la devuelve (D-93).
			heir, err := svc.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: "Pedro Ruiz Heredero"})
			if err != nil {
				t.Fatal(err)
			}
			mark, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelDuplicateOf.String(), From: pedro.ID, To: heir.ID})
			if err != nil || mark.Merge == nil || len(mark.Merge.Transferred) != 1 {
				t.Fatalf("merging pedro: %+v %v", mark.Merge, err)
			}
			// La superviviente tiene también la propia «Duplicate Of»: se mira sólo la de cliente.
			moved, err := svc.Relationships.Handle(ctx, papp.PartyRelationships{PartyID: mustParty(t, heir.ID), ActiveOnly: true})
			moved = slices.DeleteFunc(moved, func(r papp.RelationshipDTO) bool { return r.Type != domain.RelCustomer.String() })
			if err != nil || len(moved) != 1 || moved[0].TransferredFrom != buys[0].ID || moved[0].Customer.Number != "C-4300001" {
				t.Fatalf("the relationship that passed to the survivor, read back: %+v %v", moved, err)
			}
			markID, _ := domain.ParseRelationshipID(mark.ID)
			if _, err := svc.TerminateRelationship.Handle(ctx, papp.TerminateRelationship{ID: markID}); err != nil {
				t.Fatal(err)
			}
			back, err := svc.Relationships.Handle(ctx, papp.PartyRelationships{PartyID: pedroID, ActiveOnly: true})
			if err != nil || len(back) != 1 || back[0].TransferredFrom != moved[0].ID || back[0].Customer.Number != "C-4300001" {
				t.Fatalf("the relationship that came back, read back: %+v %v", back, err)
			}
			rival, err := svc.RegisterOrganization.Handle(clerkCtx, papp.RegisterOrganization{LegalName: "Rival Sociedad Limitada",
				Affiliation: &papp.NewAffiliation{Organization: acme.ID, RelationshipType: domain.RelCustomer.String()}})
			if err != nil {
				t.Fatal(err)
			}
			rivalID, _ := domain.ParsePartyID(rival.ID)
			hers, err := svc.Relationships.Handle(clerkCtx, papp.PartyRelationships{PartyID: rivalID})
			if err != nil || len(hers) != 1 {
				t.Fatalf("the rival's customer relationship: %+v %v", hers, err)
			}
			hersID, _ := domain.ParseRelationshipID(hers[0].ID)
			if _, err := svc.SetCustomerNumber.Handle(clerkCtx, papp.SetCustomerNumber{ID: hersID, CustomerNumber: "C-4300001"}); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("the number of another customer of the organization: %v", err)
			}
			seen, err := svc.Search.Handle(clerkCtx, papp.SearchParties{Kind: "person"})
			if err != nil || seen.Total != 1 || seen.Items[0].ID != pedro.ID { // el empleo de ana se terminó; luis nunca tuvo relación
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

			// Detalles por tipo de relación (docs/PARTIES-UDM.md): los códigos leídos de SQL, la
			// relación de cliente potencial que añade la migración 11 y su periodo de prueba en una columna que admite NULL.
			relTypes, err := svc.ListRelationshipTypes.Handle(ctx, papp.ListRelationshipTypes{})
			if err != nil || len(relTypes) != len(domain.WellKnownRelationshipTypes())+1 { // la semilla y la añadida más arriba
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

			// La participación de una relación de propiedad, un decimal exacto en una columna de texto que admite NULL.
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

			// Los datos personales de una persona se editan después del alta.
			edited, err := svc.UpdatePerson.Handle(clerkCtx, papp.UpdatePerson{ID: luisID, Gender: "male", BirthDate: "1988-02-29", MaritalStatus: "single"})
			if err != nil || edited.Person.BirthDate != "1988-02-29" || edited.Person.Gender != "male" || edited.Person.MaritalStatus != "single" {
				t.Fatalf("person details round trip: %+v %v", edited.Person, err)
			}

			// Lo que empieza y termina en un mismo instante del reloj (más abajo no se mueve): todos
			// los motores guardan el periodo vacío y lo releen, ninguna consulta lo encuentra en vigor,
			// y no estorba a nada que empiece en ese instante.
			instant := fw.Now()
			eva, err := svc.RegisterPerson.Handle(ctx, papp.RegisterPerson{GivenName: "Eva", FirstSurname: "Sanz", Roles: []string{domain.RoleEmployee.String()}})
			if err != nil {
				t.Fatal(err)
			}
			evaID, _ := domain.ParsePartyID(eva.ID)
			if _, err := svc.AssignRole.Handle(ctx, papp.AssignRole{PartyID: evaID, RoleType: domain.RoleCustomer.String()}); err != nil {
				t.Fatal(err)
			}
			withMail, err := svc.AddContact.Handle(ctx, papp.AddContact{PartyID: evaID, Kind: "email", Value: "eva@example.com", Purposes: []string{"default"}})
			eva = withMail.PartyDTO
			if err != nil || len(eva.Roles) != 2 || eva.Roles[1].RoleType != domain.RoleCustomer.String() || len(eva.Contacts) != 1 {
				t.Fatalf("eva: %+v %v", eva, err)
			}
			evaRole, _ := domain.ParsePartyRoleID(eva.Roles[1].ID)
			evaMail, _ := domain.ParseContactID(eva.Contacts[0].ID)
			if _, err := svc.EndRole.Handle(ctx, papp.EndRole{PartyID: evaID, RoleID: evaRole}); err != nil {
				t.Fatalf("a role ended at the instant it started: %v", err)
			}
			if _, err := svc.EndContact.Handle(ctx, papp.EndContact{PartyID: evaID, ContactID: evaMail}); err != nil {
				t.Fatalf("a contact ended at the instant it started: %v", err)
			}
			eva, err = svc.Get.Handle(ctx, papp.GetParty{ID: evaID})
			if err != nil {
				t.Fatalf("a party with empty periods read back: %v", err)
			}
			if r := eva.Roles[1]; r.Active || r.Until == nil || !r.Until.Equal(r.From) || !r.From.Equal(instant) || !eva.Roles[0].Active {
				t.Fatalf("empty role round trip: %+v", eva.Roles)
			}
			if c := eva.Contacts[0]; c.Active || c.Until == nil || !c.Until.Equal(c.From) || !c.From.Equal(instant) || len(c.Purposes) != 0 {
				t.Fatalf("empty contact round trip: %+v", c)
			}
			if never, err := svc.Search.Handle(ctx, papp.SearchParties{Text: "sanz", Role: domain.RoleCustomer.String()}); err != nil || never.Total != 0 {
				t.Fatalf("a role that never was in force (validity in SQL): %+v %v", never, err)
			}
			if plays, err := svc.Search.Handle(ctx, papp.SearchParties{Text: "sanz", Role: domain.RoleEmployee.String()}); err != nil || plays.Total != 1 {
				t.Fatalf("the role that started at that instant and goes on: %+v %v", plays, err)
			}
			if _, err := svc.AssignRole.Handle(ctx, papp.AssignRole{PartyID: evaID, RoleType: domain.RoleCustomer.String()}); err != nil {
				t.Fatalf("the same role again at that instant: %v", err)
			}
			if _, err := svc.AddContact.Handle(ctx, papp.AddContact{PartyID: evaID, Kind: "email", Value: "eva@example.com"}); err != nil {
				t.Fatalf("the same e-mail again at that instant: %v", err)
			}

			salesID, _ := domain.ParsePartyID(sales.ID)
			sales, err = svc.Classify.Handle(ctx, papp.Classify{PartyID: salesID, Classification: domain.ClassRetail.String()})
			if err != nil || len(sales.Classifications) != 1 {
				t.Fatalf("classify: %+v %v", sales.Classifications, err)
			}
			retail, _ := domain.ParseClassificationID(sales.Classifications[0].ID)
			if _, err := svc.EndClassification.Handle(ctx, papp.EndClassification{PartyID: salesID, ID: retail}); err != nil {
				t.Fatalf("a classification ended at the instant it started: %v", err)
			}
			sales, err = svc.Classify.Handle(ctx, papp.Classify{PartyID: salesID, Classification: domain.ClassCorporate.String()})
			if err != nil || len(sales.Classifications) != 2 {
				t.Fatalf("another segment at that instant (the family is exclusive): %+v %v", sales.Classifications, err)
			}
			if c := sales.Classifications; c[0].Active || c[0].Until == nil || !c[0].Until.Equal(c[0].From) || !c[1].Active {
				t.Fatalf("empty classification round trip: %+v", c)
			}
			if never, err := svc.Search.Handle(ctx, papp.SearchParties{Classification: domain.ClassRetail.String()}); err != nil || never.Total != 0 {
				t.Fatalf("a classification that never was in force: %+v %v", never, err)
			}

			hired, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelEmployment.String(), From: eva.ID, To: acme.ID})
			if err != nil {
				t.Fatal(err)
			}
			hiredID, _ := domain.ParseRelationshipID(hired.ID)
			if _, err := svc.TerminateRelationship.Handle(ctx, papp.TerminateRelationship{ID: hiredID}); err != nil {
				t.Fatalf("a relationship terminated at the instant it started: %v", err)
			}
			evaRels, err := svc.Relationships.Handle(ctx, papp.PartyRelationships{PartyID: evaID})
			if err != nil || len(evaRels) != 1 || evaRels[0].Active || evaRels[0].Until == nil || !evaRels[0].Until.Equal(evaRels[0].Since) ||
				!evaRels[0].Since.Equal(instant) {
				t.Fatalf("empty relationship round trip: %+v %v", evaRels, err)
			}
			if current, err := svc.Relationships.Handle(ctx, papp.PartyRelationships{PartyID: evaID, ActiveOnly: true}); err != nil || len(current) != 0 {
				t.Fatalf("a relationship that never was in force: %+v %v", current, err)
			}
			if _, err := svc.Get.Handle(clerkCtx, papp.GetParty{ID: evaID}); !errors.Is(err, fw.ErrNotFound) {
				t.Fatalf("an affiliation that never was in force gives no visibility: %v", err)
			}
			if member, err := mod.Organizations.InternalOrganizations(ctx, []string{eva.ID}); err != nil || len(member[eva.ID]) != 0 {
				t.Fatalf("membership through an empty affiliation: %+v %v", member, err)
			}
			if _, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelEmployment.String(), From: eva.ID, To: acme.ID}); err != nil {
				t.Fatalf("the same relationship again at that instant: %v", err)
			}
			if seen, err := svc.Get.Handle(clerkCtx, papp.GetParty{ID: evaID}); err != nil || len(seen.Organizations) != 1 {
				t.Fatalf("visible through the new employment: %+v %v", seen, err)
			}
			if !fw.Now().Equal(instant) {
				t.Fatal("the clock moved")
			}

			// Migración 29: los índices únicos detienen el código repetido que no ve la comprobación
			// del caso de uso (otro proceso que da de alta lo mismo a la vez), con la misma violación
			// de regla (422); varios tipos sin código conviven.
			catalogs, err := infrastructure.CatalogsFor(db)
			if err != nil {
				t.Fatal(err)
			}
			for _, rt := range []struct{ name, code string }{{"Avalista", "guarantor"}, {"Fiador", ""}, {"Garante", ""}} {
				typ, err := domain.NewRoleType(domain.RoleTypeID{UUID: fw.NewUUID()}, rt.name, "", rt.code, domain.RolePersonCategory)
				if err != nil {
					t.Fatal(err)
				}
				if err := catalogs.AddRoleType(ctx, typ); (rt.code == "" && err != nil) || (rt.code != "" && !errors.Is(err, fw.ErrRuleViolation)) {
					t.Fatalf("role type %s with code %q: %v", rt.name, rt.code, err)
				}
			}
			aval, err := domain.NewRelationshipType(domain.RelationshipTypeID{UUID: fw.NewUUID()}, "Aval", "", "guarantee", domain.RoleEmployee, domain.RoleCustomer)
			if err != nil {
				t.Fatal(err)
			}
			if err := catalogs.AddRelationshipType(ctx, aval); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("a relationship type code already taken: %v", err)
			}
			var promoted [2]domain.RelationshipID
			for i, name := range []string{"Agencia Sol", "Agencia Luna"} {
				agency, err := svc.RegisterOrganization.Handle(ctx, papp.RegisterOrganization{LegalName: name})
				if err != nil {
					t.Fatal(err)
				}
				rel, err := svc.EstablishRelationship.Handle(ctx, papp.EstablishRelationship{Type: domain.RelCollaborator.String(), From: agency.ID, To: acme.ID})
				if err != nil {
					t.Fatalf("collaborators without a code: %v", err)
				}
				promoted[i], _ = domain.ParseRelationshipID(rel.ID)
			}
			if _, err := svc.SetPromotionCode.Handle(ctx, papp.SetPromotionCode{ID: promoted[0], PromotionCode: "SOL-1"}); err != nil {
				t.Fatal(err)
			}
			relRepo, err := infrastructure.RelationshipRepositoryFactory(db)
			if err != nil {
				t.Fatal(err)
			}
			second, err := relRepo.Get(ctx, promoted[1])
			if err != nil {
				t.Fatal(err)
			}
			if err := second.SetPromotionCode(wellKnownRelationshipType(t, domain.RelCollaborator), "SOL-1"); err != nil {
				t.Fatal(err)
			}
			if err := relRepo.Save(ctx, second); !errors.Is(err, fw.ErrRuleViolation) {
				t.Fatalf("the promotion code of another open collaborator: %v", err)
			}

			pending, err := mod.IntegrationOutbox.Pending(ctx, 100, 10)
			if err != nil || len(pending) < 8 {
				t.Fatalf("published language: %d %v", len(pending), err)
			}
		})
	}
}

func wellKnownRelationshipType(t *testing.T, id domain.RelationshipTypeID) domain.RelationshipType {
	t.Helper()
	for _, rt := range domain.WellKnownRelationshipTypes() {
		if rt.ID == id {
			return rt
		}
	}
	t.Fatalf("no well-known relationship type %s", id)
	return domain.RelationshipType{}
}

func mustParty(t *testing.T, id string) domain.PartyID {
	t.Helper()
	p, err := domain.ParsePartyID(id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

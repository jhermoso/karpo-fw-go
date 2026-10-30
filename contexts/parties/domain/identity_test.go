package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Un contacto con dueña (D-122) sólo choca con los de su dueña y los de todos, y sus propósitos son
// los de su dueña; su incidencia de calidad de datos lleva la dueña.
func TestParty_OwnedContacts(t *testing.T) {
	ana := person(t, "Ana", "Dueña", "")
	acme, beta := domain.NewPartyID(), domain.NewPartyID()
	general, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@example.com", Purposes: []domain.Purpose{domain.PurposeDefault}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@example.com", Owner: acme}, t0); !isViolation(err, "parties.duplicate_contact") {
		t.Fatalf("the same value as a contact of everybody: %v", err)
	}
	mine, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@acme.example", Owner: acme,
		Purposes: []domain.Purpose{domain.PurposeDefault}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@acme.example", Owner: beta}, t0); err != nil {
		t.Fatalf("another company does not see acme's contact, so it is no duplicate for it: %v", err)
	}
	if c, _ := ana.Contact(general); !c.Has(domain.PurposeDefault) || c.IsOwned() {
		t.Fatalf("acme's default does not take the general default: %+v", c)
	}
	if c, _ := ana.Contact(mine); !c.Has(domain.PurposeDefault) || c.Owner != acme {
		t.Fatalf("acme's own default: %+v", c)
	}
	if c, ok := ana.ContactFor(domain.ContactEmail, domain.PurposeDefault); !ok || c.ID != general {
		t.Fatalf("the default of everybody is the general one: %+v", c)
	}
	bad, err := ana.AddContact(domain.ContactData{Kind: domain.ContactPhone, Value: "600", Owner: acme}, t0)
	if err != nil {
		t.Fatal(err)
	}
	issues := ana.DataQualityIssuesOf(bad.UUID)
	if len(issues) != 1 || issues[0].Owner != acme || issues[0].OwnerOf() == nil {
		t.Fatalf("the issue follows the owner: %+v", issues)
	}
	if !domain.WithDataQualityIssuesSeenBy([]domain.PartyID{acme}).IsSatisfiedBy(ana) || domain.WithDataQualityIssuesSeenBy([]domain.PartyID{beta}).IsSatisfiedBy(ana) {
		t.Fatal("only acme sees an issue of acme's contact")
	}
	ev := ana.PendingEvents()[len(ana.PendingEvents())-1].(domain.ContactAdded)
	if ev.Owner != acme.String() {
		t.Fatalf("the event says the owner: %+v", ev)
	}
}

// El idioma preferido es ISO 639-1; un cambio de identidad se nota con los campos que cambiaron.
func TestParty_LanguageAndIdentityCorrections(t *testing.T) {
	if _, err := vocab.NewLanguageCode("xx"); err == nil {
		t.Fatal("xx is no language")
	}
	es, err := vocab.NewLanguageCode(" ES ")
	if err != nil || es.String() != "es" {
		t.Fatalf("%v %v", es, err)
	}
	ana := person(t, "Ana", "Idioma", "")
	before := ana.IdentityFields()
	if err := ana.SetPreferredLanguage(es); err != nil || ana.Language() != es {
		t.Fatal(err)
	}
	n, _ := domain.NewPersonalName("Ana", "Idiomas", "")
	_ = ana.RenamePerson(n)
	_, _ = ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@x.example", Owner: domain.NewPartyID()}, t0)
	if got := ana.NoteIdentityCorrections(before); !slices.Equal(got, []string{"language", "name"}) {
		t.Fatalf("the contact of a company is not identity: %v", got)
	}
	ev := ana.PendingEvents()[len(ana.PendingEvents())-1].(domain.IdentityCorrected)
	if !slices.Equal(ev.Fields, []string{"language", "name"}) {
		t.Fatalf("event: %+v", ev)
	}
	if got := ana.NoteIdentityCorrections(ana.IdentityFields()); got != nil {
		t.Fatalf("nothing changed: %v", got)
	}
	first, _ := ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocNationalID, Country: vocab.MustCountryCode("ES"), Number: "12345678Z"}, false)
	second, _ := ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocPassport, Country: vocab.MustCountryCode("ES"), Number: "PAA123456",
		ExpiresOn: vocab.MustDate(2031, 2, 3), IssuingAuthority: "Policía"}, false)
	before = ana.IdentityFields()
	if err := ana.MarkPrimaryIdentification(second); err != nil {
		t.Fatal(err)
	}
	if p, _ := ana.PrimaryIdentification(); p.ID != second {
		t.Fatal("the passport is the primary now")
	}
	if got := ana.NoteIdentityCorrections(before); !slices.Equal(got, []string{domain.IdentityFieldIdentifications}) {
		t.Fatalf("marking the primary corrects the documents: %v", got)
	}
	if err := ana.MarkPrimaryIdentification(domain.IdentificationID{UUID: first.UUID}); err != nil {
		t.Fatal(err)
	}
	if err := ana.MarkPrimaryIdentification(domain.NewIdentificationID()); !errors.Is(err, fw.ErrNotFound) {
		t.Fatal("an unknown document")
	}
}

// El perfil de la pareja en la relación: número de proveedor, códigos externos y consentimientos.
func TestRelationship_PairProfile(t *testing.T) {
	types, _ := domain.IndexRelationshipTypes(domain.WellKnownRelationshipTypes())
	customer, supplier := types[domain.RelCustomer], types[domain.RelSupplier]
	acme, talleres := org(t, "Acme", ""), org(t, "Talleres", "")
	_, _ = acme.AssignRole(cat, domain.RoleInternalOrganization, t0)
	_, _ = talleres.AssignRole(cat, domain.RoleSupplier, t0)
	_, _ = talleres.AssignRole(cat, domain.RoleCustomer, t0)
	buys, err := domain.Establish(domain.NewRelationshipID(), supplier, acme, talleres, cat, t0, "")
	if err != nil {
		t.Fatal(err)
	}
	sells, _ := domain.Establish(domain.NewRelationshipID(), customer, talleres, acme, cat, t0, "")
	if err := buys.SetSupplierNumber(supplier, " p-17 "); err != nil || buys.SupplierNumber() != "P-17" {
		t.Fatalf("%v %q", err, buys.SupplierNumber())
	}
	if err := sells.SetSupplierNumber(customer, "P-1"); !isViolation(err, "parties.not_a_supplier_relationship") {
		t.Fatal("a customer has no supplier number")
	}
	if err := buys.SetSupplierNumber(supplier, "P 1"); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("format")
	}
	if err := sells.SetExternalCodes(customer, domain.ExternalCodes{Sage: " 430000017 ", Apiscore: "C-77"}); err != nil ||
		sells.ExternalCodes() != (domain.ExternalCodes{Sage: "430000017", Apiscore: "C-77"}) {
		t.Fatalf("%v %+v", err, sells.ExternalCodes())
	}
	if err := buys.SetExternalCodes(supplier, domain.ExternalCodes{Sage: strings.Repeat("9", 16)}); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("a Sage code has at most 15")
	}

	now := fw.Now()
	d := domain.ConsentData{LegalBasis: domain.BasisConsent, Purpose: "marketing", GrantedAt: now.Add(-time.Hour), RetentionMonths: 24}
	id, err := sells.RecordConsent(acme.ID(), d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sells.RecordConsent(acme.ID(), d); !isViolation(err, "parties.consent_duplicate_purpose") {
		t.Fatalf("one consent in force per purpose: %v", err)
	}
	if _, err := sells.RecordConsent(domain.NewPartyID(), d); !isViolation(err, "parties.consent_needs_company") {
		t.Fatal("the company is one side of the pair")
	}
	if _, err := domain.ParseLegalBasis("whim"); err == nil {
		t.Fatal("an unknown legal basis")
	}
	future := d
	future.Purpose, future.GrantedAt = "billing", now.Add(time.Hour)
	if _, err := sells.RecordConsent(acme.ID(), future); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("not granted in the future")
	}
	if err := sells.WithdrawConsent(acme.ID(), id, now.Add(-2*time.Hour)); !isViolation(err, "parties.consent_withdrawn_before_granted") {
		t.Fatal("withdrawn before granted")
	}
	if err := sells.WithdrawConsent(acme.ID(), id, now); err != nil {
		t.Fatal(err)
	}
	if err := sells.WithdrawConsent(acme.ID(), id, now); !isViolation(err, "parties.consent_withdrawn") {
		t.Fatal("twice")
	}
	if cs := sells.Consents(); len(cs) != 1 || cs[0].IsActive() || cs[0].RetentionMonths != 24 {
		t.Fatalf("%+v", cs)
	}
	if _, err := sells.RecordConsent(acme.ID(), d); err != nil {
		t.Fatalf("after withdrawing, the purpose is free again: %v", err)
	}
	events := sells.PendingEvents()
	if rec, ok := events[len(events)-1].(domain.DataProtectionConsentRecorded); !ok || rec.Organization != acme.ID().String() || rec.Party != talleres.ID().String() {
		t.Fatalf("event: %+v", events[len(events)-1])
	}
	// Una relación terminada ya no cambia su perfil.
	_ = sells.Terminate(now)
	if err := sells.SetExternalCodes(customer, domain.ExternalCodes{}); !isViolation(err, "parties.relationship_ended") {
		t.Fatal("an ended relationship keeps its codes")
	}
}

// El primer documento completa la identidad y no avisa; la marca de documento rechazado sí cuenta.
func TestParty_FirstDocumentAndDocumentIssue(t *testing.T) {
	ana := person(t, "Ana", "Primera", "")
	before := ana.IdentityFields()
	_, _ = ana.AddIdentification(policy, domain.IdentificationData{Type: domain.DocNationalID, Country: vocab.MustCountryCode("ES"), Number: "12345678Z"}, false)
	if got := ana.NoteIdentityCorrections(before); got != nil {
		t.Fatalf("the first document is no correction: %v", got)
	}
	before = ana.IdentityFields()
	_ = ana.FlagDocumentIssue("1234", "control digit")
	if got := ana.NoteIdentityCorrections(before); !slices.Equal(got, []string{"documentIssue"}) {
		t.Fatalf("the mark of a refused document: %v", got)
	}
}

// Retirar un consentimiento no se fecha en el futuro; al fusionar, los vigentes pasan a la relación
// que continúa y los retirados se quedan.
func TestRelationship_ConsentsWithdrawnAndTransferred(t *testing.T) {
	types, _ := domain.IndexRelationshipTypes(domain.WellKnownRelationshipTypes())
	customer := types[domain.RelCustomer]
	acme, dup, survivor := org(t, "Acme", ""), org(t, "Talleres", ""), org(t, "Talleres bis", "")
	_, _ = acme.AssignRole(cat, domain.RoleInternalOrganization, t0)
	for _, p := range []*domain.Party{dup, survivor} {
		_, _ = p.AssignRole(cat, domain.RoleCustomer, t0)
	}
	prev, _ := domain.Establish(domain.NewRelationshipID(), customer, dup, acme, cat, t0, "")
	now := fw.Now()
	kept, _ := prev.RecordConsent(acme.ID(), domain.ConsentData{LegalBasis: domain.BasisConsent, Purpose: "marketing", GrantedAt: now.Add(-time.Hour)})
	gone, _ := prev.RecordConsent(acme.ID(), domain.ConsentData{LegalBasis: domain.BasisContract, Purpose: "billing", GrantedAt: now.Add(-time.Hour)})
	if err := prev.WithdrawConsent(acme.ID(), gone, now.Add(time.Hour)); !errors.Is(err, fw.ErrValidation) {
		t.Fatalf("not withdrawn in the future: %v", err)
	}
	if err := prev.WithdrawConsent(acme.ID(), gone, now); err != nil {
		t.Fatal(err)
	}
	next, _ := domain.Establish(domain.NewRelationshipID(), customer, survivor, acme, cat, now, "")
	if err := next.Continue(customer, prev, false); err != nil {
		t.Fatal(err)
	}
	if cs := next.Consents(); len(cs) != 1 || cs[0].ID != kept || !cs[0].IsActive() {
		t.Fatalf("the consent in force goes with the pair: %+v", cs)
	}
}

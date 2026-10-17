package domain_test

import (
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/time/fake"
)

// Lo que empieza «ahora» puede terminar «ahora»: puede que el reloj del anfitrión no se haya movido
// entre los dos casos de uso (el reloj del sistema avanza a saltos), y un usuario puede deshacer en
// el acto lo que acaba de introducir. El escenario corre con un reloj parado, así que cada inicio y
// cada fin son el mismo instante.
func TestEndingAtTheInstantOfTheStart(t *testing.T) {
	clock := fake.New(t0)
	t.Cleanup(fw.SetClock(clock))

	var employment domain.RelationshipType
	for _, rt := range domain.WellKnownRelationshipTypes() {
		if rt.ID == domain.RelEmployment {
			employment = rt
		}
	}
	ana := person(t, "Ana", "García", "")
	acme := org(t, "Acme", "")

	role, err := ana.AssignRole(cat, domain.RoleEmployee, fw.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acme.AssignRole(cat, domain.RoleInternalOrganization, fw.Now()); err != nil {
		t.Fatal(err)
	}
	contact, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@example.com",
		Purposes: []domain.Purpose{domain.PurposeDefault}}, fw.Now())
	if err != nil {
		t.Fatal(err)
	}
	class, err := acme.Classify(domain.WellKnownClassificationCatalog(), domain.ClassRetail, fw.Now())
	if err != nil {
		t.Fatal(err)
	}
	facility := fw.NewUUID()
	facilityRole, err := acme.AssignFacilityRole(domain.WellKnownFacilityRoleTypes(), facility, domain.FacilityHeadquarters, fw.Now())
	if err != nil {
		t.Fatal(err)
	}
	rel, err := domain.Establish(domain.NewRelationshipID(), employment, ana, acme, cat, fw.Now(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ana.Affiliate(acme.ID(), rel.ID(), fw.Now()); err != nil {
		t.Fatal(err)
	}

	for name, end := range map[string]func() error{
		"contact":        func() error { return ana.EndContact(contact, fw.Now()) },
		"role":           func() error { return ana.EndRole(role, fw.Now()) },
		"classification": func() error { return acme.EndClassification(class, fw.Now()) },
		"facility role":  func() error { return acme.EndFacilityRole(facilityRole, fw.Now()) },
		"relationship":   func() error { return rel.Terminate(fw.Now()) },
		"affiliation":    func() error { return ana.EndAffiliation(rel.ID(), fw.Now()) },
	} {
		if err := end(); err != nil {
			t.Errorf("%s ended at the instant it started: %v", name, err)
		}
	}
	if t.Failed() {
		return
	}

	// Lo que terminó en el acto nunca estuvo vigente, ni en ese instante ni después.
	now, later := fw.Now(), fw.Now().Add(time.Hour)
	if _, ok := ana.ContactFor(domain.ContactEmail, domain.PurposeDefault); ok || ana.Contacts()[0].IsActiveAt(now) {
		t.Error("the contact was never in use")
	}
	if ana.PlaysAt(cat, domain.RoleEmployee, now) || ana.PlaysAt(cat, domain.RoleEmployee, later) {
		t.Error("the role was never played")
	}
	if domain.ClassifiedAt(now, domain.ClassRetail).IsSatisfiedBy(acme) || len(ana.OrganizationsAt(now)) != 0 {
		t.Error("the classification and the affiliation were never in force")
	}
	if domain.ActiveAt(now).IsSatisfiedBy(rel) || domain.ActiveAt(later).IsSatisfiedBy(rel) || rel.Until() == nil {
		t.Error("the relationship was never in force")
	}
	// Se queda en la historia y no estorba a nada: el mismo hecho puede volver a empezar en ese
	// instante, o antes con el fin abierto.
	if len(ana.Contacts()) != 1 || len(ana.Roles()) != 1 {
		t.Fatalf("history: %d contacts, %d roles", len(ana.Contacts()), len(ana.Roles()))
	}
	if _, err := ana.AddContact(domain.ContactData{Kind: domain.ContactEmail, Value: "ana@example.com"}, now); err != nil {
		t.Errorf("the same e-mail again at that instant: %v", err)
	}
	if _, err := ana.AssignRole(cat, domain.RoleEmployee, now.Add(-time.Hour)); err != nil {
		t.Errorf("the same role from an hour before: %v", err)
	}
	if _, err := acme.Classify(domain.WellKnownClassificationCatalog(), domain.ClassCorporate, now.Add(-time.Hour)); err != nil {
		t.Errorf("another segment from an hour before (the family is exclusive): %v", err)
	}
	if _, err := acme.AssignFacilityRole(domain.WellKnownFacilityRoleTypes(), facility, domain.FacilityHeadquarters, now); err != nil {
		t.Errorf("the same facility role again at that instant: %v", err)
	}
	// Terminar otra vez no hace nada, y el inicio sigue siendo el fin más temprano que hay.
	if err := ana.EndContact(contact, later); err != nil {
		t.Errorf("ending an ended contact later: %v", err)
	}
	if err := ana.EndContact(contact, now.Add(-time.Nanosecond)); !isViolation(err, "parties.contact_end_before_start") {
		t.Errorf("end before start: %v", err)
	}
}

package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

var (
	cat = domain.WellKnownRoleCatalog()
	t0  = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
)

func person(t *testing.T, given, first, second string) *domain.Party {
	t.Helper()
	n, err := domain.NewPersonalName(given, first, second)
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.RegisterPerson(domain.NewPartyID(), domain.PersonDetails{Name: n})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func org(t *testing.T, legal, trade string) *domain.Party {
	t.Helper()
	n, err := domain.NewOrganizationName(legal, trade)
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.RegisterOrganization(domain.NewPartyID(), domain.OrganizationDetails{Name: n})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

func TestNames(t *testing.T) {
	n, err := domain.NewPersonalName("  María  José ", "García", " de la Fuente ")
	if err != nil || n.String() != "María José García de la Fuente" || n.Surnames() != "García de la Fuente" {
		t.Fatalf("%q %v", n, err)
	}
	if _, err := domain.NewPersonalName("Ana", "", ""); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("a first surname is required")
	}
	o, _ := domain.NewOrganizationName("Acme Sociedad Anónima", "Acme")
	if o.String() != "Acme" || o.Legal() != "Acme Sociedad Anónima" {
		t.Fatalf("%v", o)
	}
	if same, _ := domain.NewOrganizationName("Acme", "ACME"); same.Trade() != "" {
		t.Fatal("a trade name equal to the legal name is dropped")
	}
	if _, err := domain.ParseGender("x"); err == nil {
		t.Fatal("gender enum")
	}
	if m, err := domain.ParseMaritalStatus(" Married "); err != nil || m != domain.MaritalMarried {
		t.Fatal("marital status")
	}
}

func TestRoleCatalog(t *testing.T) {
	if cat.Applicability(domain.RoleEmployee) != domain.AppliesToPerson ||
		cat.Applicability(domain.RoleFinancialInstitution) != domain.AppliesToOrganization ||
		cat.Applicability(domain.RoleCustomer) != domain.AppliesToAny ||
		cat.Applicability(domain.RoleAgent) != domain.AppliesToOrganization {
		t.Fatal("applicability is derived from the hierarchy")
	}
	if !cat.IsA(domain.RoleFinancialInstitution, domain.RoleCorporation) || !cat.IsA(domain.RoleBillToCustomer, domain.RoleCustomer) ||
		cat.IsA(domain.RoleCustomer, domain.RoleBillToCustomer) {
		t.Fatal("IsA follows the ancestors")
	}
	if d := cat.Descendants(domain.RoleCustomer); len(d) != 4 {
		t.Fatalf("customer and its three sub-roles: %v", d)
	}
	cyc := []domain.RoleType{
		{ID: domain.RolePartyRoot, Name: vocab.MustName("a"), Parent: ptr(domain.RoleCustomer)},
		{ID: domain.RoleCustomer, Name: vocab.MustName("b"), Parent: ptr(domain.RolePartyRoot)},
	}
	if _, err := domain.NewRoleCatalog(cyc, domain.RolePersonCategory, domain.RoleOrganizationCategory); err == nil {
		t.Fatal("cycles are rejected")
	}
	if _, err := domain.NewRoleCatalog([]domain.RoleType{{ID: domain.RoleCustomer, Name: vocab.MustName("b"), Parent: ptr(domain.RolePartyRoot)}},
		domain.RolePersonCategory, domain.RoleOrganizationCategory); err == nil {
		t.Fatal("unknown parents are rejected")
	}
	for _, rt := range domain.WellKnownRelationshipTypes() {
		if _, ok := cat.RoleType(rt.FromRole); !ok {
			t.Errorf("%s: unknown from role", rt.Name)
		}
		if _, ok := cat.RoleType(rt.ToRole); !ok {
			t.Errorf("%s: unknown to role", rt.Name)
		}
	}
}

func ptr(id domain.RoleTypeID) *domain.RoleTypeID { return &id }

func TestParty_RegisterAndRename(t *testing.T) {
	ana := person(t, "Ana", "García", "López")
	if ana.Kind() != domain.KindPerson || ana.Name() != "Ana García López" || !ana.IsActive() {
		t.Fatal("register person")
	}
	if len(ana.PendingEvents()) != 1 || ana.PendingEvents()[0].EventType() != "parties.party_registered" {
		t.Fatal("registered event")
	}
	n, _ := domain.NewPersonalName("Ana", "García", "")
	if err := ana.RenamePerson(n); err != nil || ana.Name() != "Ana García" {
		t.Fatal(err)
	}
	o, _ := domain.NewOrganizationName("Acme", "")
	if err := ana.RenameOrganization(o); !isViolation(err, "parties.kind_mismatch") {
		t.Fatalf("kind mismatch: %v", err)
	}
	future := vocab.DateOf(fw.Now().AddDate(1, 0, 0))
	if err := ana.UpdatePersonDetails(domain.GenderFemale, future, ""); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("birth date cannot be in the future")
	}
	ana.Deactivate()
	if err := ana.RenamePerson(n); !isViolation(err, "parties.inactive") {
		t.Fatal("inactive parties cannot be renamed")
	}
}

func TestParty_Roles(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	acme := org(t, "Acme", "")

	if _, err := ana.AssignRole(cat, domain.RoleSupplier, t0); !isViolation(err, "parties.role_incompatible") {
		t.Fatalf("a person cannot be a supplier: %v", err)
	}
	if _, err := acme.AssignRole(cat, domain.RoleEmployee, t0); !isViolation(err, "parties.role_incompatible") {
		t.Fatalf("an organization cannot be an employee: %v", err)
	}
	if _, err := acme.AssignRole(cat, domain.RoleLegalCategory, t0); !isViolation(err, "parties.role_is_category") {
		t.Fatalf("categories cannot be assigned: %v", err)
	}
	if _, err := acme.AssignRole(cat, domain.RoleTypeID{UUID: fw.NewUUID()}, t0); !errors.Is(err, fw.ErrValidation) {
		t.Fatal("unknown role types are rejected")
	}

	id, err := ana.AssignRole(cat, domain.RoleCustomer, t0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ana.AssignRole(cat, domain.RoleCustomer, t0.AddDate(0, 6, 0)); !isViolation(err, "parties.role_overlap") {
		t.Fatalf("overlapping roles of the same type: %v", err)
	}
	if err := ana.EndRole(id, t0.AddDate(-1, 0, 0)); !isViolation(err, "parties.role_end_before_start") {
		t.Fatalf("end before start: %v", err)
	}
	if err := ana.EndRole(id, t0.AddDate(1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := ana.AssignRole(cat, domain.RoleCustomer, t0.AddDate(1, 0, 0)); err != nil {
		t.Fatalf("a new period after the end is allowed: %v", err)
	}
	if _, err := ana.AssignRole(cat, domain.RoleCustomer, t0.AddDate(0, 3, 0)); !isViolation(err, "parties.role_overlap") {
		t.Fatal("a period inside the first one overlaps")
	}
	if !ana.PlaysAt(cat, domain.RoleCustomer, t0.AddDate(0, 1, 0)) || !ana.PlaysAt(cat, domain.RoleCustomer, t0.AddDate(2, 0, 0)) {
		t.Fatal("plays customer in both periods")
	}

	bank := org(t, "Banco", "")
	if _, err := bank.AssignRole(cat, domain.RoleFinancialInstitution, t0); err != nil {
		t.Fatal(err)
	}
	if !bank.PlaysAt(cat, domain.RoleCorporation, t0) || bank.PlaysAt(cat, domain.RoleCorporation, t0.Add(-time.Second)) {
		t.Fatal("a financial institution is a corporation, from the role start on")
	}
	if err := bank.EndRole(domain.PartyRoleID{UUID: fw.NewUUID()}, t0); !errors.Is(err, fw.ErrNotFound) {
		t.Fatal("unknown role")
	}
	bank.Deactivate()
	if _, err := bank.AssignRole(cat, domain.RoleSupplier, t0); !isViolation(err, "parties.inactive") {
		t.Fatal("inactive parties get no roles")
	}
}

func TestRelationship(t *testing.T) {
	var employment domain.RelationshipType
	for _, rt := range domain.WellKnownRelationshipTypes() {
		if rt.ID == domain.RelEmployment {
			employment = rt
		}
	}
	ana := person(t, "Ana", "García", "")
	acme := org(t, "Acme", "")

	if _, err := domain.Establish(domain.NewRelationshipID(), employment, ana, acme, cat, t0, ""); !isViolation(err, "parties.relationship_role_missing") {
		t.Fatalf("roles are required: %v", err)
	}
	_, _ = ana.AssignRole(cat, domain.RoleEmployee, t0)
	_, _ = acme.AssignRole(cat, domain.RoleInternalOrganization, t0)
	if _, err := domain.Establish(domain.NewRelationshipID(), employment, ana, acme, cat, t0.Add(-time.Hour), ""); !isViolation(err, "parties.relationship_role_missing") {
		t.Fatal("roles must be played when the relationship starts")
	}
	if _, err := domain.Establish(domain.NewRelationshipID(), employment, ana, ana, cat, t0, ""); !isViolation(err, "parties.self_relationship") {
		t.Fatal("self relationship")
	}
	r, err := domain.Establish(domain.NewRelationshipID(), employment, ana, acme, cat, t0, "hired")
	if err != nil {
		t.Fatal(err)
	}
	if r.From() != ana.ID() || r.FromRole() != domain.RoleEmployee || r.Until() != nil || len(r.PendingEvents()) != 1 {
		t.Fatal("established")
	}
	if !domain.ActiveAt(t0).IsSatisfiedBy(r) || !domain.Involving(acme.ID()).IsSatisfiedBy(r) {
		t.Fatal("specs")
	}
	if err := r.Terminate(t0.Add(-time.Hour)); !isViolation(err, "parties.relationship_end_before_start") {
		t.Fatal("end before start")
	}
	if err := r.Terminate(t0.AddDate(1, 0, 0)); err != nil || r.Until() == nil {
		t.Fatal(err)
	}
	if domain.ActiveAt(t0.AddDate(2, 0, 0)).IsSatisfiedBy(r) || !domain.ActiveAt(t0.AddDate(0, 6, 0)).IsSatisfiedBy(r) {
		t.Fatal("validity")
	}
	if dup := domain.SameRelationship(employment, ana.ID(), acme.ID(), t0.AddDate(0, 6, 0)); !dup.IsSatisfiedBy(r) {
		t.Fatal("an overlapping relationship is a duplicate")
	}
	if dup := domain.SameRelationship(employment, ana.ID(), acme.ID(), t0.AddDate(2, 0, 0)); dup.IsSatisfiedBy(r) {
		t.Fatal("after the end it is not")
	}
}

func TestPartySpecs(t *testing.T) {
	ana := person(t, "Ana", "García", "")
	_, _ = ana.AssignRole(cat, domain.RoleBillToCustomer, t0)
	customers := domain.PlaysAt(t0.AddDate(0, 1, 0), cat.Descendants(domain.RoleCustomer)...)
	if !customers.IsSatisfiedBy(ana) || domain.PlaysAt(t0.Add(-time.Hour), domain.RoleBillToCustomer).IsSatisfiedBy(ana) {
		t.Fatal("PlaysAt with descendants and validity")
	}
	if !domain.NameContains("garcía").IsSatisfiedBy(ana) || domain.OfKind(domain.KindOrganization).IsSatisfiedBy(ana) {
		t.Fatal("name and kind")
	}
}

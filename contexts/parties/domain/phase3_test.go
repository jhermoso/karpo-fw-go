package domain_test

import (
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
)

func TestParty_AffiliationsAndVisibility(t *testing.T) {
	acme := org(t, "Acme", "")
	globex := org(t, "Globex", "")
	ana := person(t, "Ana", "García", "")
	rel := domain.NewRelationshipID()

	if err := ana.Affiliate(acme.ID(), rel, t0); err != nil {
		t.Fatal(err)
	}
	if err := ana.Affiliate(acme.ID(), rel, t0); err != nil || len(ana.Affiliations()) != 1 {
		t.Fatal("affiliating is idempotent per relationship")
	}
	_ = acme.Affiliate(acme.ID(), domain.NewRelationshipID(), t0)
	if len(acme.Affiliations()) != 0 {
		t.Fatal("an organization is not affiliated with itself")
	}
	at := t0.AddDate(0, 1, 0)
	if got := ana.OrganizationsAt(at); len(got) != 1 || got[0] != acme.ID() {
		t.Fatalf("organizations: %v", got)
	}
	visibleToAcme := domain.VisibleTo([]domain.PartyID{acme.ID()}, at)
	visibleToGlobex := domain.VisibleTo([]domain.PartyID{globex.ID()}, at)
	if !visibleToAcme.IsSatisfiedBy(ana) || visibleToGlobex.IsSatisfiedBy(ana) || !visibleToAcme.IsSatisfiedBy(acme) {
		t.Fatal("visible through the affiliation and to the organization itself")
	}
	if err := ana.EndAffiliation(rel, at); err != nil {
		t.Fatal(err)
	}
	if domain.VisibleTo([]domain.PartyID{acme.ID()}, at.Add(time.Hour)).IsSatisfiedBy(ana) {
		t.Fatal("an ended affiliation hides the party")
	}
	if !domain.VisibleTo([]domain.PartyID{acme.ID()}, at.Add(-time.Hour)).IsSatisfiedBy(ana) {
		t.Fatal("the past stays visible at past instants")
	}
	if err := ana.EndAffiliation(domain.NewRelationshipID(), at); err != nil {
		t.Fatal("ending an unknown affiliation is a no-op")
	}

	bank := org(t, "Banco", "")
	bank.Share(true)
	if !visibleToGlobex.IsSatisfiedBy(bank) || !bank.IsShared() {
		t.Fatal("shared parties are visible to every organization")
	}
}

func TestParty_LegalForm(t *testing.T) {
	acme := org(t, "Acme", "")
	if err := acme.SetLegalForm(domain.LegalFormCorporation); err != nil || acme.Organization().LegalForm != domain.LegalFormCorporation {
		t.Fatal(err)
	}
	if _, err := domain.ParseLegalForm("llc"); err == nil {
		t.Fatal("unknown legal form")
	}
	ana := person(t, "Ana", "García", "")
	if err := ana.SetLegalForm(domain.LegalFormCorporation); !isViolation(err, "parties.kind_mismatch") {
		t.Fatal("people have no legal form")
	}
}

func TestRollupIsHierarchical(t *testing.T) {
	for _, rt := range domain.WellKnownRelationshipTypes() {
		if (rt.ID == domain.RelOrganizationRollup) != rt.Hierarchical {
			t.Errorf("%s hierarchical=%v", rt.Name, rt.Hierarchical)
		}
	}
	division := org(t, "Sales", "")
	_, _ = division.AssignRole(cat, domain.RoleDivision, t0)
	acme := org(t, "Acme", "")
	_, _ = acme.AssignRole(cat, domain.RoleInternalOrganization, t0)
	var rollup domain.RelationshipType
	for _, rt := range domain.WellKnownRelationshipTypes() {
		if rt.ID == domain.RelOrganizationRollup {
			rollup = rt
		}
	}
	if _, err := domain.Establish(domain.NewRelationshipID(), rollup, division, acme, cat, t0, ""); err != nil {
		t.Fatalf("a division rolls up to an internal organization: %v", err)
	}
}

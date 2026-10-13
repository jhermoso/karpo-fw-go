package domain_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
)

// The role type 0037 is "Team", code "team", in every project (docs/DECISIONES-COMUNES.md, D-54).
func TestTeamRoleTypeHasTheCommonNameAndCode(t *testing.T) {
	team, ok := domain.WellKnownRoleCatalog().ByCode("team")
	if !ok {
		t.Fatal("no role type has the code team")
	}
	if team.ID != domain.RoleTeam || team.Name.String() != "Team" {
		t.Fatalf("team is %s %q", team.ID, team.Name)
	}
	if team.ID.String() != "10000000-0000-0000-0001-000000000037" {
		t.Fatalf("team has the id %s", team.ID)
	}
}

// Every project seeds the same types, with the same ids and codes (docs/DECISIONES-COMUNES.md, D-62).
func TestTheCatalogHasTheCommonTypes(t *testing.T) {
	cat := domain.WellKnownRoleCatalog()
	for code, id := range map[string]string{
		"team":  "10000000-0000-0000-0001-000000000037",
		"club":  "10000000-0000-0000-0001-000000000038",
		"group": "10000000-0000-0000-0001-000000000039",
	} {
		rt, ok := cat.ByCode(code)
		if !ok || rt.ID.String() != id || !cat.IsA(rt.ID, domain.RoleInformalCategory) || cat.Applicability(rt.ID) != domain.AppliesToOrganization {
			t.Fatalf("role type %s: %+v", code, rt)
		}
	}
	rels := map[string]string{}
	for _, rt := range domain.WellKnownRelationshipTypes() {
		rels[rt.Code] = rt.ID.String()
	}
	for code, id := range map[string]string{
		"subsidiary":            "10000000-0000-0000-0002-000000000010",
		"legal-representation":  "10000000-0000-0000-0002-000000000011",
		"department-assignment": "10000000-0000-0000-0002-000000000012",
		"collaborator-contact":  "10000000-0000-0000-0002-000000000013",
	} {
		if rels[code] != id {
			t.Fatalf("relationship type %s has the id %q", code, rels[code])
		}
	}
}

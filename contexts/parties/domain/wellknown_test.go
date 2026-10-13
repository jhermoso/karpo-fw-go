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

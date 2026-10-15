package infrastructure

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	_ "modernc.org/sqlite"
)

// A database seeded before the common catalog gets the types it lacks and Team among the informal
// organizations; running the step again changes nothing.
func TestSeedCommonCatalog_CompletesAnEarlierSeed(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "catalog.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	m, err := Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// As an earlier seed left it.
	for _, q := range []string{
		`DELETE FROM relationship_types WHERE code IN ('subsidiary', 'legal-representation', 'department-assignment', 'collaborator-contact')`,
		`DELETE FROM role_types WHERE code IN ('club', 'group')`,
		`UPDATE role_types SET parent_id = (SELECT id FROM role_types WHERE code = 'organization-unit') WHERE code = 'team'`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string) (n int) {
		if err := raw.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for range 2 {
		if err := seedCommonCatalog(ctx, db); err != nil {
			t.Fatal(err)
		}
		if n := count(`SELECT COUNT(*) FROM role_types`); n != len(domain.WellKnownRoleTypes()) {
			t.Fatalf("role types: %d", n)
		}
		if n := count(`SELECT COUNT(*) FROM relationship_types`); n != len(domain.WellKnownRelationshipTypes()) {
			t.Fatalf("relationship types: %d", n)
		}
		if n := count(`SELECT COUNT(*) FROM role_types WHERE code IN ('team', 'club', 'group')
			AND parent_id = (SELECT id FROM role_types WHERE code = 'informal')`); n != 3 {
			t.Fatalf("informal organizations: %d", n)
		}
		if n := count(`SELECT COUNT(*) FROM relationship_types WHERE code IS NULL OR code = ''`); n != 0 {
			t.Fatalf("relationship types without a code: %d", n)
		}
	}
}

package infrastructure

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	_ "modernc.org/sqlite"
)

// Una base sembrada antes del catálogo común recibe los tipos que le faltan y Team entre las
// organizaciones informales; volver a ejecutar el paso no cambia nada.
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

	// Como la dejó una siembra anterior.
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
	// Una base de antes del traslado: Prospect Relationship en …0016, con una relación.
	for _, q := range []string{
		`DELETE FROM relationship_types WHERE code = 'duplicate-of'`,
		`UPDATE relationship_types SET id = '` + oldProspect + `' WHERE code = 'prospect'`,
		`INSERT INTO parties (id, version, kind, name, active, test) VALUES ('0f0f0f0f-0000-0000-0000-0000000000a1', 1, 'person', 'Bea', 1, 0)`,
		`INSERT INTO parties (id, version, kind, name, active, test) VALUES ('0f0f0f0f-0000-0000-0000-0000000000a2', 1, 'organization', 'Acme', 1, 0)`,
		`INSERT INTO party_relationships (id, version, rel_type, from_party, to_party, from_role, to_role, valid_from)
			VALUES ('0f0f0f0f-0000-0000-0000-0000000000b1', 1, '` + oldProspect + `', '0f0f0f0f-0000-0000-0000-0000000000a1',
			'0f0f0f0f-0000-0000-0000-0000000000a2', '` + domain.RoleProspect.String() + `', '` + domain.RoleInternalOrganization.String() + `', '2026-10-01 00:00:00')`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	// Un tipo que alguien añadió con un código que necesita el programa detiene el paso, con un
	// mensaje.
	if _, err := raw.Exec(`INSERT INTO role_types (id, name, category, code) VALUES ('0f0f0f0f-0000-0000-0000-000000000001', 'Club', 0, 'club')`); err != nil {
		t.Fatal(err)
	}
	if err := seedCommonCatalog(ctx, db); err == nil || !strings.Contains(err.Error(), `"club"`) {
		t.Fatalf("a code already taken: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM role_types WHERE name = 'Club'`); err != nil {
		t.Fatal(err)
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
		if n := count(`SELECT COUNT(*) FROM relationship_types WHERE code = 'prospect' AND id = '` + domain.RelProspect.String() + `'`); n != 1 {
			t.Fatalf("prospect at its new id: %d", n)
		}
		if n := count(`SELECT COUNT(*) FROM party_relationships WHERE rel_type = '` + domain.RelProspect.String() + `'`); n != 1 {
			t.Fatalf("the prospect relationship did not follow its type: %d", n)
		}
		if n := count(`SELECT COUNT(*) FROM relationship_types WHERE code = 'duplicate-of' AND id = '` + oldProspect + `'`); n != 1 {
			t.Fatalf("duplicate of at the old id: %d", n)
		}
		if n := count(`SELECT COUNT(*) FROM relationship_types WHERE code IS NULL OR code = ''`); n != 0 {
			t.Fatalf("relationship types without a code: %d", n)
		}
	}
}

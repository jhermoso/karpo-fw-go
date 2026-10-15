package infrastructure

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/geography/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// A database seeded with the broken name gets it corrected; one seeded afterwards keeps the
// right one.
func TestFixBoundaryNames(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "geo.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
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
	name := func() string {
		var n string
		if err := raw.QueryRowContext(ctx, "SELECT name FROM geo_boundaries WHERE geo_code = '15' AND ine_code = '15' AND nuts_code = 'ES111'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := name(); got != "Coruña, A" {
		t.Fatalf("freshly seeded: %q", got)
	}
	id := domain.BoundaryID{UUID: fw.MustParseUUID(boundaryNameFixes[0].id)}
	if n, err := db.Update(ctx, "geo_boundaries", sqlrepo.Values{"name": "CoruÃ±a, A"}, sqlrepo.Values{"id": id}); err != nil || n != 1 {
		t.Fatalf("breaking the name: %d %v", n, err)
	}
	if err := fixBoundaryNames(ctx, db); err != nil {
		t.Fatal(err)
	}
	if got := name(); got != "Coruña, A" {
		t.Fatalf("after the fix: %q", got)
	}
}

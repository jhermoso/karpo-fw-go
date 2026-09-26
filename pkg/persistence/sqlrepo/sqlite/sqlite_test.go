package sqlite_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlconformance"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// OpenTemp opens a file-backed SQLite database in a temporary directory.
func openTemp(t *testing.T) *sqlrepo.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "karpo.db")
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1) // SQLite has a single writer; one connection avoids lock contention
	db := sqlite.Open(raw)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRepositoryConformance(t *testing.T) {
	sqlconformance.Run(t, openTemp(t))
}

package infrastructure_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure/securityconformance"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// La misma batería se ejecuta sobre PostgreSQL, SQL Server, Oracle y MySQL en el módulo de integración.
func TestSecurityConformance_SQLite(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "security.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw)
	m, err := infrastructure.Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	securityconformance.Run(t, db)
}

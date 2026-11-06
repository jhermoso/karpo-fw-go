package infrastructure_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/security"
	"github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure"
	"github.com/jhermoso/karpo-fw-go/contexts/security/infrastructure/securityconformance"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
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

// La migración 4 sobre una base que ya tenía las tres primeras (también en el módulo de integración).
func TestSecurityGrantedBackfill_SQLite(t *testing.T) {
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "backfill.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	securityconformance.GrantedBackfill(t, sqlite.Open(raw))
}

// La migración 5 (D-135) sobre una base que ya tenía las cuatro primeras (también en el módulo de integración).
func TestSecurityTreasuryRename_SQLite(t *testing.T) {
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "rename.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw)
	sec := security.Compose(hotswap.New(db), security.WithPasswordHasher(infrastructure.NewPBKDF2(1000)))
	securityconformance.TreasuryRename(t, db, func(ctx context.Context, declared ...authz.Permission) error {
		_, err := sec.SyncCatalog(ctx, declared...)
		return err
	})
}

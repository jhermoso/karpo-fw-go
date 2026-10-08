package sqlconformance

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// RunMigrations checks sqlrepo.Migrator on db: pending/applied/modified/unknown/dirty states,
// idempotence, incremental application, independent contexts, out-of-order refusal, rollback
// of a failed migration (or dirty state on engines without transactional DDL), Force, and the
// lease lock.
func RunMigrations(t *testing.T, db *sqlrepo.DB) {
	ctx := context.Background()
	for _, s := range []string{"t_mig_a", "t_mig_b", "t_mig_c", "t_mig_other", "t_mig_seed", sqlrepo.DefaultMigrationsTable, sqlrepo.DefaultMigrationsLock} {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+s)
	}
	transactional := db.Dialect().Name() != "oracle" && db.Dialect().Name() != "mysql"

	v1 := sqlrepo.Migration{Version: 1, Name: "create a", Up: sqlrepo.Portable(`CREATE TABLE t_mig_a (id INT NOT NULL PRIMARY KEY, name VARCHAR(50))`)}
	v2 := sqlrepo.Migration{Version: 2, Name: "seed a", Up: sqlrepo.Portable(`INSERT INTO t_mig_a (id, name) VALUES (1, 'one')`)}
	v3 := sqlrepo.Migration{Version: 3, Name: "create b", Up: sqlrepo.Portable(`CREATE TABLE t_mig_b (id INT NOT NULL PRIMARY KEY)`)}
	other := sqlrepo.MigrationSet{Context: "other", Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "create other", Up: sqlrepo.Portable(`CREATE TABLE t_mig_other (id INT NOT NULL PRIMARY KEY)`)}}}
	set := func(ms ...sqlrepo.Migration) []sqlrepo.MigrationSet {
		return []sqlrepo.MigrationSet{{Context: "alpha", Migrations: ms}, other}
	}
	migrator := func(sets []sqlrepo.MigrationSet, opts ...sqlrepo.MigratorOption) *sqlrepo.Migrator {
		t.Helper()
		m, err := sqlrepo.NewMigrator(db, sets, opts...)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	states := func(m *sqlrepo.Migrator) map[string]string {
		t.Helper()
		st, err := m.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, s := range st {
			out[s.Context+"/"+s.Name] = s.State
		}
		return out
	}
	count := func(table string) int {
		t.Helper()
		var n int
		if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	// A fresh database: everything pending, Verify fails, Status does not write.
	m := migrator(set(v1, v2))
	if err := m.Verify(ctx); !errors.Is(err, application.ErrSchemaOutdated) {
		t.Fatalf("verify on an empty database: %v", err)
	}
	if st := states(m); st["alpha/create a"] != application.MigrationPending || st["other/create other"] != application.MigrationPending {
		t.Fatalf("fresh status: %v", st)
	}
	applied, err := m.Migrate(ctx)
	if err != nil || len(applied) != 3 {
		t.Fatalf("migrate: %d %v", len(applied), err)
	}
	if err := m.Verify(ctx); err != nil || count("t_mig_a") != 1 {
		t.Fatalf("after migrate: %v", err)
	}
	if again, err := m.Migrate(ctx); err != nil || len(again) != 0 {
		t.Fatalf("migrate is idempotent: %d %v", len(again), err)
	}

	// Incremental: only the new migration runs.
	m = migrator(set(v1, v2, v3))
	if applied, err := m.Migrate(ctx); err != nil || len(applied) != 1 || applied[0].Name != "create b" || count("t_mig_a") != 1 {
		t.Fatalf("incremental: %v %v", applied, err)
	}

	// Out of order: a pending migration older than an applied one is refused.
	if _, err := migrator(set(v1, v2, v3, sqlrepo.Migration{Version: 10, Name: "ten", Up: sqlrepo.Portable(`INSERT INTO t_mig_a (id, name) VALUES (10, 'ten')`)})).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	late := sqlrepo.Migration{Version: 5, Name: "late", Up: sqlrepo.Portable(`INSERT INTO t_mig_a (id, name) VALUES (5, 'five')`)}
	ten := sqlrepo.Migration{Version: 10, Name: "ten", Up: sqlrepo.Portable(`INSERT INTO t_mig_a (id, name) VALUES (10, 'ten')`)}
	if _, err := migrator(set(v1, v2, v3, late, ten)).Migrate(ctx); !errors.Is(err, application.ErrSchemaOutdated) || count("t_mig_a") != 2 {
		t.Fatalf("out of order must be refused: %v", err)
	}

	// Modified: an applied migration whose statements changed.
	v2b := v2
	v2b.Up = sqlrepo.Portable(`INSERT INTO t_mig_a (id, name) VALUES (1, 'uno')`)
	m = migrator(set(v1, v2b, v3, ten))
	if st := states(m); st["alpha/seed a"] != application.MigrationModified {
		t.Fatalf("modified: %v", st)
	}
	if _, err := m.Migrate(ctx); !errors.Is(err, application.ErrSchemaOutdated) {
		t.Fatalf("migrate must refuse a modified history: %v", err)
	}

	// Unknown: the database is ahead of this code.
	m = migrator(set(v1))
	if st := states(m); st["alpha/create b"] != application.MigrationUnknown {
		t.Fatalf("unknown: %v", st)
	}
	if err := m.Verify(ctx); !errors.Is(err, application.ErrSchemaOutdated) {
		t.Fatalf("verify with unknown migrations: %v", err)
	}

	// A failing migration.
	broken := sqlrepo.Migration{Version: 20, Name: "broken", Up: sqlrepo.Portable(
		`CREATE TABLE t_mig_c (id INT NOT NULL PRIMARY KEY)`,
		`INSERT INTO t_mig_missing (id) VALUES (1)`)}
	m = migrator(set(v1, v2, v3, ten, broken))
	_, err = m.Migrate(ctx)
	if transactional {
		if err == nil || errors.Is(err, application.ErrSchemaDirty) {
			t.Fatalf("transactional failure: %v", err)
		}
		if st := states(m); st["alpha/broken"] != application.MigrationPending {
			t.Fatalf("a failed transactional migration leaves no trace: %v", st)
		}
		if _, err := db.ExecContext(ctx, "SELECT COUNT(*) FROM t_mig_c"); err == nil {
			t.Fatal("the DDL of the failed migration must be rolled back")
		}
	} else {
		if !errors.Is(err, application.ErrSchemaDirty) {
			t.Fatalf("non transactional failure must be dirty: %v", err)
		}
		if st := states(m); st["alpha/broken"] != application.MigrationDirty {
			t.Fatalf("dirty: %v", st)
		}
		if err := m.Verify(ctx); !errors.Is(err, application.ErrSchemaDirty) {
			t.Fatalf("verify dirty: %v", err)
		}
		if _, err := m.Migrate(ctx); !errors.Is(err, application.ErrSchemaDirty) {
			t.Fatalf("migrate must refuse a dirty history: %v", err)
		}
		// Repair by hand, then force.
		if err := m.Force(ctx, "alpha", 20); err != nil {
			t.Fatal(err)
		}
		if err := m.Verify(ctx); err != nil {
			t.Fatalf("after force: %v", err)
		}
	}

	// From here on the code declares whatever the database now holds.
	current := []sqlrepo.Migration{v1, v2, v3, ten}
	if !transactional {
		current = append(current, broken) // forced as applied
	}

	// Lease lock: another live instance blocks, an expired one is taken over.
	if _, err := db.ExecContext(ctx, "INSERT INTO "+db.Dialect().Quote(sqlrepo.DefaultMigrationsLock)+
		" (id, owner, acquired_at) VALUES (1, 'other-instance', "+nowSQL(db)+")"); err != nil {
		t.Fatal(err)
	}
	m = migrator(set(current...), sqlrepo.WithLockTimeouts(time.Hour, 300*time.Millisecond))
	if _, err := m.Migrate(ctx); !errors.Is(err, sqlrepo.ErrMigrationLocked) {
		t.Fatalf("a held lock must block: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	m = migrator(set(current...), sqlrepo.WithLockTimeouts(time.Millisecond, 5*time.Second))
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatalf("an expired lock must be taken over: %v", err)
	}
	if count(sqlrepo.DefaultMigrationsLock) != 0 {
		t.Fatal("the lock must be released")
	}

	// Go migrations: seeds with the dialect's value binding, in the migration's unit of work.
	seed := []sqlrepo.MigrationSet{{Context: "seed", Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "seed table", Up: sqlrepo.Portable(`CREATE TABLE t_mig_seed (id INT NOT NULL PRIMARY KEY, name VARCHAR(50) NOT NULL)`)},
		{Version: 2, Name: "seed rows", Run: func(ctx context.Context, db *sqlrepo.DB) error {
			for i, n := range []string{"uno", "dos"} {
				if err := db.Insert(ctx, "t_mig_seed", sqlrepo.Values{"id": int64(i + 1), "name": n}); err != nil {
					return err
				}
			}
			return nil
		}},
	}}}
	sm := migrator(seed)
	if applied, err := sm.Migrate(ctx); err != nil || len(applied) != 2 || count("t_mig_seed") != 2 {
		t.Fatalf("go migration: %v %v", applied, err)
	}
	if err := sm.Verify(ctx); err != nil {
		t.Fatalf("a go migration keeps a stable checksum: %v", err)
	}
	if n, err := db.Update(ctx, "t_mig_seed", sqlrepo.Values{"name": "two"}, sqlrepo.Values{"id": int64(2)}); err != nil || n != 1 {
		t.Fatalf("update: %d %v", n, err)
	}
	if _, err := db.Update(ctx, "t_mig_seed", sqlrepo.Values{"name": "x"}, nil); err == nil {
		t.Fatal("an update without condition is refused")
	}
	// InsertMany: more rows than one batch and more parameters than SQL Server's limit.
	var many [][]any
	for i := range 1234 {
		many = append(many, []any{int64(1000 + i), fmt.Sprintf("n%04d", i)})
	}
	if err := db.Do(ctx, func(ctx context.Context) error { return db.InsertMany(ctx, "t_mig_seed", []string{"id", "name"}, many) }); err != nil {
		t.Fatalf("insert many: %v", err)
	}
	if n := count("t_mig_seed"); n != 2+1234 {
		t.Fatalf("insert many count: %d", n)
	}
	if err := db.InsertMany(ctx, "t_mig_seed", []string{"id", "name"}, [][]any{{int64(1)}}); err == nil {
		t.Fatal("a row with missing values is refused")
	}
	rows, err := db.Select(ctx, "t_mig_seed", []string{"id", "name"}, "id")
	if err != nil || len(rows) != 2+1234 || rows[1].Int64("id") != 2 || rows[1].String("name") != "two" ||
		rows[len(rows)-1].String("name") != "n1233" {
		t.Fatalf("select: %d rows, %v", len(rows), err)
	}

	if _, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{{Context: "x", Migrations: []sqlrepo.Migration{v2, v1}}}); err == nil {
		t.Fatal("versions must increase")
	}
}

// nowSQL returns an expression for "now minus a few seconds" valid on the engine.
func nowSQL(db *sqlrepo.DB) string {
	switch db.Dialect().Name() {
	case "sqlite":
		return "strftime('%Y-%m-%dT%H:%M:%fZ', 'now')"
	case "oracle":
		return "SYSTIMESTAMP"
	case "sqlserver":
		return "SYSUTCDATETIME()"
	case "mysql":
		return "UTC_TIMESTAMP(6)"
	}
	return "CURRENT_TIMESTAMP"
}

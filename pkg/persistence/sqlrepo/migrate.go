package sqlrepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Default names of the migration history and lock tables.
const (
	DefaultMigrationsTable = "schema_migrations"
	DefaultMigrationsLock  = "schema_migrations_lock"
)

// AnyDialect keys the statements of a migration that are valid on every engine.
const AnyDialect = "*"

// Migration is one versioned change of a bounded context's schema (or of its reference data).
// Up holds its statements per dialect name ("sqlite", "postgres", "sqlserver", "oracle",
// "mysql") or AnyDialect. Statements are executed one by one; Oracle statements must not end
// with ";". A migration never changes once applied: write a new one instead.
type Migration struct {
	Version int64
	Name    string
	Up      map[string][]string
	// Run is optional Go code executed after the statements, in a unit of work of db (data
	// migrations and seeds that need the dialect's value binding, such as UUIDs on Oracle).
	// Its code is not part of the checksum: never change an applied Run, add a migration.
	Run func(ctx context.Context, db *DB) error
}

// Portable builds the statements of a migration valid on every engine.
func Portable(stmts ...string) map[string][]string { return map[string][]string{AnyDialect: stmts} }

// MigrationSet is the ordered list of migrations owned by a bounded context. Each context keeps
// its own version sequence, so contexts evolve independently on a shared database.
type MigrationSet struct {
	Context    string
	Migrations []Migration
}

func (s MigrationSet) validate() error {
	if strings.TrimSpace(s.Context) == "" {
		return fmt.Errorf("%w: migration set without context", domain.ErrValidation)
	}
	var last int64
	for _, m := range s.Migrations {
		if m.Version <= last {
			return fmt.Errorf("%w: %s: migration versions must be positive and strictly increasing (%d after %d)",
				domain.ErrValidation, s.Context, m.Version, last)
		}
		if strings.TrimSpace(m.Name) == "" || (len(m.Up) == 0 && m.Run == nil) {
			return fmt.Errorf("%w: %s v%d needs a name and statements", domain.ErrValidation, s.Context, m.Version)
		}
		last = m.Version
	}
	return nil
}

// Migrator is the SQL application.SchemaMigrator: it records applied migrations in a history
// table with the checksum of their statements, serializes concurrent instances with a lease
// lock, and runs each migration in a transaction on engines with transactional DDL (SQLite,
// PostgreSQL, SQL Server). On Oracle and MySQL, where DDL commits implicitly, a migration is
// recorded as dirty before it runs and clean after it succeeds; a failure leaves it dirty until
// someone repairs the database and calls Force.
type Migrator struct {
	db        *DB
	sets      []MigrationSet
	table     string
	lockTable string
	lease     time.Duration
	wait      time.Duration
	owner     string
}

var _ application.SchemaMigrator = (*Migrator)(nil)

// MigratorOption configures a Migrator.
type MigratorOption func(*Migrator)

// WithMigrationTables overrides the history and lock table names.
func WithMigrationTables(history, lock string) MigratorOption {
	return func(m *Migrator) { m.table, m.lockTable = history, lock }
}

// WithLockTimeouts sets how long a lock is valid before it can be taken over (lease, default
// 10 minutes) and how long Migrate waits for it (wait, default 1 minute).
func WithLockTimeouts(lease, wait time.Duration) MigratorOption {
	return func(m *Migrator) { m.lease, m.wait = lease, wait }
}

// NewMigrator creates a migrator for the given bounded contexts on db.
func NewMigrator(db *DB, sets []MigrationSet, opts ...MigratorOption) (*Migrator, error) {
	m := &Migrator{db: db, sets: sets, table: DefaultMigrationsTable, lockTable: DefaultMigrationsLock,
		lease: 10 * time.Minute, wait: time.Minute, owner: domain.NewUUID().String()}
	for _, o := range opts {
		o(m)
	}
	if err := errors.Join(checkIdent("table", m.table), checkIdent("table", m.lockTable)); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, s := range sets {
		if err := s.validate(); err != nil {
			return nil, err
		}
		if seen[s.Context] {
			return nil, fmt.Errorf("%w: context %s declared twice", domain.ErrValidation, s.Context)
		}
		seen[s.Context] = true
	}
	return m, nil
}

func (m *Migrator) q(s string) string { return m.db.d.Quote(s) }

// Transactional reports whether DDL on the dialect is transactional.
func transactionalDDL(dialect string) bool { return dialect != "oracle" && dialect != "mysql" }

func (m *Migrator) statements(mg Migration) ([]string, error) {
	if s, ok := mg.Up[m.db.d.Name()]; ok {
		return s, nil
	}
	if s, ok := mg.Up[AnyDialect]; ok {
		return s, nil
	}
	if len(mg.Up) == 0 && mg.Run != nil {
		return nil, nil
	}
	return nil, fmt.Errorf("%w: migration %d %q has no statements for %s", domain.ErrUnsupported, mg.Version, mg.Name, m.db.d.Name())
}

// checksumOf returns the checksum of a migration on the migrator's dialect.
func (m *Migrator) checksumOf(mg Migration) (string, error) {
	stmts, err := m.statements(mg)
	if err != nil {
		return "", err
	}
	if mg.Run != nil {
		stmts = append(slices.Clone(stmts), "run:"+mg.Name)
	}
	return checksum(stmts), nil
}

func checksum(stmts []string) string {
	h := sha256.Sum256([]byte(strings.Join(stmts, "\n;\n")))
	return hex.EncodeToString(h[:])
}

type historyRow struct {
	name, checksum string
	dirty          bool
	at             time.Time
}

type historyKey struct {
	context string
	version int64
}

// ---------------------------------------------------------------------------------------------
// Tables
// ---------------------------------------------------------------------------------------------

func (m *Migrator) ddl() (history, lock string) {
	switch m.db.d.Name() {
	case "sqlite":
		return fmt.Sprintf(`CREATE TABLE %s (context TEXT NOT NULL, version INTEGER NOT NULL, name TEXT NOT NULL,
	checksum TEXT NOT NULL, dirty INTEGER NOT NULL, applied_at TEXT NOT NULL, PRIMARY KEY (context, version))`, m.q(m.table)),
			fmt.Sprintf(`CREATE TABLE %s (id INTEGER NOT NULL PRIMARY KEY, owner TEXT NOT NULL, acquired_at TEXT NOT NULL)`, m.q(m.lockTable))
	case "postgres":
		return fmt.Sprintf(`CREATE TABLE %s (context VARCHAR(100) NOT NULL, version BIGINT NOT NULL, name VARCHAR(200) NOT NULL,
	checksum VARCHAR(64) NOT NULL, dirty BOOLEAN NOT NULL, applied_at TIMESTAMPTZ NOT NULL, PRIMARY KEY (context, version))`, m.q(m.table)),
			fmt.Sprintf(`CREATE TABLE %s (id INTEGER NOT NULL PRIMARY KEY, owner VARCHAR(64) NOT NULL, acquired_at TIMESTAMPTZ NOT NULL)`, m.q(m.lockTable))
	case "sqlserver":
		return fmt.Sprintf(`CREATE TABLE %s (context NVARCHAR(100) NOT NULL, version BIGINT NOT NULL, name NVARCHAR(200) NOT NULL,
	checksum NVARCHAR(64) NOT NULL, dirty BIT NOT NULL, applied_at DATETIME2(7) NOT NULL, PRIMARY KEY (context, version))`, m.q(m.table)),
			fmt.Sprintf(`CREATE TABLE %s (id INT NOT NULL PRIMARY KEY, owner NVARCHAR(64) NOT NULL, acquired_at DATETIME2(7) NOT NULL)`, m.q(m.lockTable))
	case "oracle":
		return fmt.Sprintf(`CREATE TABLE %s (context VARCHAR2(100 CHAR) NOT NULL, version NUMBER(19) NOT NULL, name VARCHAR2(200 CHAR) NOT NULL,
	checksum VARCHAR2(64 CHAR) NOT NULL, dirty NUMBER(1) NOT NULL, applied_at TIMESTAMP(6) WITH TIME ZONE NOT NULL, PRIMARY KEY (context, version))`, m.q(m.table)),
			fmt.Sprintf(`CREATE TABLE %s (id NUMBER(10) NOT NULL PRIMARY KEY, owner VARCHAR2(64 CHAR) NOT NULL, acquired_at TIMESTAMP(6) WITH TIME ZONE NOT NULL)`, m.q(m.lockTable))
	case "mysql":
		return fmt.Sprintf(`CREATE TABLE %s (context VARCHAR(100) NOT NULL, version BIGINT NOT NULL, name VARCHAR(200) NOT NULL,
	checksum VARCHAR(64) NOT NULL, dirty BOOLEAN NOT NULL, applied_at DATETIME(6) NOT NULL, PRIMARY KEY (context, version))`, m.q(m.table)),
			fmt.Sprintf(`CREATE TABLE %s (id INT NOT NULL PRIMARY KEY, owner VARCHAR(64) NOT NULL, acquired_at DATETIME(6) NOT NULL)`, m.q(m.lockTable))
	}
	return "", ""
}

// ensureTables creates the history and lock tables when missing (outside any transaction: a
// failed probe would abort a PostgreSQL transaction).
func (m *Migrator) ensureTables(ctx context.Context) error {
	history, lock := m.ddl()
	if history == "" {
		return fmt.Errorf("%w: no migration tables for dialect %s", domain.ErrUnsupported, m.db.d.Name())
	}
	for _, t := range []struct{ name, ddl string }{{m.table, history}, {m.lockTable, lock}} {
		if m.exists(ctx, t.name) {
			continue
		}
		if _, err := m.db.sqlDB.ExecContext(ctx, t.ddl); err != nil && !m.exists(ctx, t.name) {
			return fmt.Errorf("sqlrepo: creating %s: %w", t.name, err)
		}
	}
	return nil
}

func (m *Migrator) exists(ctx context.Context, table string) bool {
	rows, err := m.db.sqlDB.QueryContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE 1 = 0", m.q(table)))
	if err != nil {
		return false
	}
	_ = rows.Close()
	return true
}

func (m *Migrator) history(ctx context.Context) (map[historyKey]historyRow, error) {
	cols := []string{"context", "version", "name", "checksum", "dirty", "applied_at"}
	sel := make([]string, len(cols))
	for i, c := range cols {
		sel[i] = m.q(c)
	}
	rows, err := m.db.sqlDB.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s", strings.Join(sel, ", "), m.q(m.table)))
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: reading %s: %w", m.table, err)
	}
	scanned, err := scanAll(m.db.d, rows, cols)
	if err != nil {
		return nil, err
	}
	out := make(map[historyKey]historyRow, len(scanned))
	for _, r := range scanned {
		k := historyKey{r.String("context"), r.Int64("version")}
		out[k] = historyRow{name: r.String("name"), checksum: r.String("checksum"), dirty: r.Bool("dirty"), at: r.Time("applied_at")}
		if err := r.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------------------------
// Status / Verify / Migrate
// ---------------------------------------------------------------------------------------------

// Status implements application.SchemaMigrator.
// It only reads: without a history table every migration is pending.
func (m *Migrator) Status(ctx context.Context) ([]application.MigrationStatus, error) {
	h := map[historyKey]historyRow{}
	if m.exists(ctx, m.table) {
		var err error
		if h, err = m.history(ctx); err != nil {
			return nil, err
		}
	}
	var out []application.MigrationStatus
	for _, s := range m.sets {
		declared := map[int64]bool{}
		for _, mg := range s.Migrations {
			declared[mg.Version] = true
			st := application.MigrationStatus{Context: s.Context, Version: mg.Version, Name: mg.Name, State: application.MigrationPending}
			if row, ok := h[historyKey{s.Context, mg.Version}]; ok {
				at := row.at
				st.AppliedAt = &at
				sum, err := m.checksumOf(mg)
				switch {
				case row.dirty:
					st.State = application.MigrationDirty
				case err != nil || row.checksum != sum:
					st.State = application.MigrationModified
				default:
					st.State = application.MigrationApplied
				}
			}
			out = append(out, st)
		}
		for k, row := range h {
			if k.context == s.Context && !declared[k.version] {
				at := row.at
				out = append(out, application.MigrationStatus{Context: s.Context, Version: k.version, Name: row.name,
					State: application.MigrationUnknown, AppliedAt: &at})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b application.MigrationStatus) int {
		if c := strings.Compare(a.Context, b.Context); c != 0 {
			return c
		}
		return int(a.Version - b.Version)
	})
	return out, nil
}

// Verify implements application.SchemaMigrator.
func (m *Migrator) Verify(ctx context.Context) error {
	st, err := m.Status(ctx)
	if err != nil {
		return err
	}
	return verify(st)
}

func verify(st []application.MigrationStatus) error {
	var problems []string
	dirty := false
	for _, s := range st {
		if s.State != application.MigrationApplied {
			problems = append(problems, fmt.Sprintf("%s v%d %s: %s", s.Context, s.Version, s.Name, s.State))
			dirty = dirty || s.State == application.MigrationDirty
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sentinel := application.ErrSchemaOutdated
	if dirty {
		sentinel = application.ErrSchemaDirty
	}
	return fmt.Errorf("%w: %s", sentinel, strings.Join(problems, "; "))
}

// Migrate implements application.SchemaMigrator.
func (m *Migrator) Migrate(ctx context.Context) (applied []application.MigrationStatus, err error) {
	if err := m.ensureTables(ctx); err != nil {
		return nil, err
	}
	if err := m.lock(ctx); err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, m.unlock(context.WithoutCancel(ctx))) }()

	st, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}
	var blocking []application.MigrationStatus
	maxApplied := map[string]int64{}
	for _, s := range st {
		switch s.State {
		case application.MigrationApplied:
			maxApplied[s.Context] = max(maxApplied[s.Context], s.Version)
		case application.MigrationPending:
		default:
			blocking = append(blocking, s)
		}
	}
	if len(blocking) > 0 {
		return nil, verify(blocking)
	}
	for _, set := range m.sets {
		for _, mg := range set.Migrations {
			if !slices.ContainsFunc(st, func(s application.MigrationStatus) bool {
				return s.Context == set.Context && s.Version == mg.Version && s.State == application.MigrationPending
			}) {
				continue
			}
			if mg.Version < maxApplied[set.Context] {
				return applied, fmt.Errorf("%w: %s v%d is older than the applied v%d: give it a newer version",
					application.ErrSchemaOutdated, set.Context, mg.Version, maxApplied[set.Context])
			}
			if err := m.apply(ctx, set.Context, mg); err != nil {
				return applied, err
			}
			now := domain.Now()
			applied = append(applied, application.MigrationStatus{Context: set.Context, Version: mg.Version, Name: mg.Name,
				State: application.MigrationApplied, AppliedAt: &now})
		}
	}
	return applied, nil
}

func (m *Migrator) apply(ctx context.Context, bc string, mg Migration) error {
	stmts, err := m.statements(mg)
	if err != nil {
		return err
	}
	sum, err := m.checksumOf(mg)
	if err != nil {
		return err
	}
	run := func(ctx context.Context) error {
		for i, s := range stmts {
			if _, err := m.db.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("sqlrepo: %s v%d %s, statement %d: %w", bc, mg.Version, mg.Name, i+1, err)
			}
		}
		if mg.Run == nil {
			return nil
		}
		if err := m.db.Do(ctx, func(ctx context.Context) error { return mg.Run(ctx, m.db) }); err != nil {
			return fmt.Errorf("sqlrepo: %s v%d %s: %w", bc, mg.Version, mg.Name, err)
		}
		return nil
	}
	if transactionalDDL(m.db.d.Name()) {
		return m.db.Do(ctx, func(ctx context.Context) error {
			if err := run(ctx); err != nil {
				return err
			}
			return m.record(ctx, bc, mg, sum, false)
		})
	}
	if err := m.record(ctx, bc, mg, sum, true); err != nil {
		return err
	}
	if err := run(ctx); err != nil {
		return fmt.Errorf("%w: %w (repair the database, then call Force)", application.ErrSchemaDirty, err)
	}
	return m.markClean(ctx, bc, mg.Version)
}

func (m *Migrator) record(ctx context.Context, bc string, mg Migration, sum string, dirty bool) error {
	b := &Builder{d: m.db.d}
	vals := []any{bc, mg.Version, mg.Name, sum, dirty, domain.Now()}
	phs := make([]string, len(vals))
	for i, v := range vals {
		ph, err := b.Arg(v)
		if err != nil {
			return err
		}
		phs[i] = ph
	}
	cols := []string{"context", "version", "name", "checksum", "dirty", "applied_at"}
	for i, c := range cols {
		cols[i] = m.q(c)
	}
	_, err := m.db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		m.q(m.table), strings.Join(cols, ", "), strings.Join(phs, ", ")), b.args...)
	return err
}

func (m *Migrator) markClean(ctx context.Context, bc string, version int64) error {
	b := &Builder{d: m.db.d}
	f, _ := b.Arg(false)
	c, _ := b.Arg(bc)
	v, _ := b.Arg(version)
	_, err := m.db.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s = %s AND %s = %s",
		m.q(m.table), m.q("dirty"), f, m.q("context"), c, m.q("version"), v), b.args...)
	return err
}

// Force marks a dirty migration as applied after the database was repaired by hand, updating
// its checksum to the current statements.
func (m *Migrator) Force(ctx context.Context, bc string, version int64) error {
	for _, s := range m.sets {
		if s.Context != bc {
			continue
		}
		for _, mg := range s.Migrations {
			if mg.Version != version {
				continue
			}
			cs, err := m.checksumOf(mg)
			if err != nil {
				return err
			}
			b := &Builder{d: m.db.d}
			f, _ := b.Arg(false)
			sum, _ := b.Arg(cs)
			c, _ := b.Arg(bc)
			v, _ := b.Arg(version)
			_, err = m.db.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET %s = %s, %s = %s WHERE %s = %s AND %s = %s",
				m.q(m.table), m.q("dirty"), f, m.q("checksum"), sum, m.q("context"), c, m.q("version"), v), b.args...)
			return err
		}
	}
	return fmt.Errorf("%w: %s v%d is not declared", domain.ErrNotFound, bc, version)
}

// ---------------------------------------------------------------------------------------------
// Lease lock (one migrator at a time across instances)
// ---------------------------------------------------------------------------------------------

// ErrMigrationLocked is returned when another instance holds the migration lock too long.
var ErrMigrationLocked = fmt.Errorf("%w: schema migration lock held by another instance", domain.ErrConflict)

func (m *Migrator) lock(ctx context.Context) error {
	deadline := time.Now().Add(m.wait)
	for {
		b := &Builder{d: m.db.d}
		id, _ := b.Arg(int64(1))
		owner, _ := b.Arg(m.owner)
		at, err := b.Arg(domain.Now())
		if err != nil {
			return err
		}
		_, err = m.db.sqlDB.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s, %s, %s) VALUES (%s, %s, %s)",
			m.q(m.lockTable), m.q("id"), m.q("owner"), m.q("acquired_at"), id, owner, at), b.args...)
		if err == nil {
			return nil
		}
		if !m.db.d.IsUniqueViolation(err) {
			return fmt.Errorf("sqlrepo: migration lock: %w", err)
		}
		m.takeOverExpired(ctx)
		if time.Now().After(deadline) {
			return ErrMigrationLocked
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// takeOverExpired deletes a lock whose lease expired (its holder crashed).
func (m *Migrator) takeOverExpired(ctx context.Context) {
	b := &Builder{d: m.db.d}
	id, _ := b.Arg(int64(1))
	limit, err := b.Arg(domain.Now().Add(-m.lease))
	if err != nil {
		return
	}
	_, _ = m.db.sqlDB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s = %s AND %s < %s",
		m.q(m.lockTable), m.q("id"), id, m.q("acquired_at"), limit), b.args...)
}

func (m *Migrator) unlock(ctx context.Context) error {
	b := &Builder{d: m.db.d}
	owner, _ := b.Arg(m.owner)
	_, err := m.db.sqlDB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s = %s", m.q(m.lockTable), m.q("owner"), owner), b.args...)
	return err
}

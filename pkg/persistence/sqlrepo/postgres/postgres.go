// Package postgres provides the PostgreSQL dialect for sqlrepo. It does not import a driver:
// register one in the composition root (e.g. github.com/jackc/pgx/v5/stdlib, driver "pgx").
//
// Recommended column types: UUID for identities, BOOLEAN, TIMESTAMPTZ, BIGINT for versions.
package postgres

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Dialect implements sqlrepo.Dialect for PostgreSQL.
type Dialect struct{}

// New returns the PostgreSQL dialect.
func New() Dialect { return Dialect{} }

// Open wraps an opened PostgreSQL *sql.DB.
func Open(db *sql.DB, opts ...sqlrepo.Option) *sqlrepo.DB { return sqlrepo.New(db, New(), opts...) }

func (Dialect) Name() string                         { return "postgres" }
func (Dialect) Placeholder(n int) string             { return fmt.Sprintf("$%d", n) }
func (Dialect) Quote(ident string) string            { return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"` }
func (Dialect) LimitOffset(l, o int) string          { return fmt.Sprintf("LIMIT %d OFFSET %d", l, o) }
func (Dialect) LikeEscape() string                   { return `ESCAPE '\'` }
func (Dialect) MaxInList() int                       { return 10000 }
func (Dialect) BoolValue(b bool) any                 { return b }
func (Dialect) UUIDValue(u domain.UUID) any          { return u.String() }
func (Dialect) TimeValue(t time.Time) any            { return t.UTC() }
func (Dialect) ParseUUID(v any) (domain.UUID, error) { return sqlrepo.ParseUUIDDefault(v) }

// DateValue binds a civil date as midnight UTC into a DATE column.
func (Dialect) DateValue(y int, m time.Month, d int) any {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DateExpr returns the placeholder unchanged.
func (Dialect) DateExpr(ph string) string { return ph }

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "SQLSTATE 23505", "(23505)", "duplicate key value violates unique constraint")
}

// OutboxDDL returns the statements creating the outbox table.
func OutboxDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultOutboxTable
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id VARCHAR(64) PRIMARY KEY,
	event_type VARCHAR(200) NOT NULL,
	aggregate_type VARCHAR(200),
	aggregate_id VARCHAR(64),
	aggregate_version BIGINT,
	payload TEXT NOT NULL,
	occurred_at TIMESTAMPTZ NOT NULL,
	correlation_id VARCHAR(64),
	causation_id VARCHAR(64),
	attempts INTEGER NOT NULL DEFAULT 0,
	last_error VARCHAR(1000),
	processed_at TIMESTAMPTZ)`, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS ix_%s_pending ON %s (processed_at, occurred_at)`, table, table),
	}
}

// AuditDDL returns the statements creating the audit log table.
func AuditDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultAuditTable
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id VARCHAR(64) PRIMARY KEY, aggregate_type VARCHAR(200) NOT NULL, aggregate_id VARCHAR(64) NOT NULL,
	aggregate_version BIGINT NOT NULL, operation VARCHAR(20) NOT NULL, actor_id VARCHAR(64), actor_name VARCHAR(200),
	channel VARCHAR(20), import_source VARCHAR(200), import_run_id VARCHAR(64), import_file VARCHAR(500),
	correlation_id VARCHAR(64), occurred_at TIMESTAMPTZ NOT NULL, changes TEXT, events VARCHAR(2000))`, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS ix_%s_trail ON %s (aggregate_type, aggregate_id, occurred_at)`, table, table),
	}
}

var _ sqlrepo.Dialect = Dialect{}

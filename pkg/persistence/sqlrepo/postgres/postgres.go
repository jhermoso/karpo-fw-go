// Package postgres ofrece el dialecto PostgreSQL para sqlrepo. No importa ningún driver: se
// registra uno en la raíz de composición (p. ej. github.com/jackc/pgx/v5/stdlib, driver "pgx").
//
// Tipos de columna recomendados: UUID para las identidades, BOOLEAN, TIMESTAMPTZ, BIGINT para las
// versiones.
package postgres

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Dialect implementa sqlrepo.Dialect para PostgreSQL.
type Dialect struct{}

// New devuelve el dialecto PostgreSQL.
func New() Dialect { return Dialect{} }

// Open envuelve un *sql.DB de PostgreSQL ya abierto.
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

// DateValue enlaza una fecha civil como medianoche UTC en una columna DATE.
func (Dialect) DateValue(y int, m time.Month, d int) any {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DateExpr devuelve el placeholder sin cambios.
func (Dialect) DateExpr(ph string) string { return ph }

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "SQLSTATE 23505", "(23505)", "duplicate key value violates unique constraint")
}

// OutboxDDL devuelve las sentencias que crean la tabla del outbox.
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

// AuditDDL devuelve las sentencias que crean la tabla del registro de auditoría.
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

// InboxDDL devuelve las sentencias que crean la tabla del inbox (consumidor, id del mensaje).
func InboxDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultInboxTable
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	consumer VARCHAR(100) NOT NULL, message_id VARCHAR(64) NOT NULL, processed_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (consumer, message_id))`, table),
	}
}

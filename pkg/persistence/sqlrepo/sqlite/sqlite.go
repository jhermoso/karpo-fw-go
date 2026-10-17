// Package sqlite ofrece el dialecto SQLite para sqlrepo. No importa ningún driver: se registra
// uno en la raíz de composición (p. ej. modernc.org/sqlite, Go puro, nombre de driver "sqlite").
//
// Notas de semántica: el LIKE de SQLite no distingue mayúsculas de minúsculas en ASCII salvo que
// se active PRAGMA case_sensitive_like; los booleanos se guardan como 0/1; los UUID como TEXT; los
// instantes como TEXT UTC de ancho fijo (orden lexicográfico == orden cronológico).
package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TimeLayout es el formato UTC de ancho fijo con el que se guardan los instantes.
const TimeLayout = "2006-01-02T15:04:05.000000000Z"

// Dialect implementa sqlrepo.Dialect para SQLite.
type Dialect struct{}

// New devuelve el dialecto SQLite.
func New() Dialect { return Dialect{} }

// Open envuelve un *sql.DB de SQLite ya abierto.
func Open(db *sql.DB, opts ...sqlrepo.Option) *sqlrepo.DB { return sqlrepo.New(db, New(), opts...) }

func (Dialect) Name() string                         { return "sqlite" }
func (Dialect) Placeholder(int) string               { return "?" }
func (Dialect) Quote(ident string) string            { return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"` }
func (Dialect) LimitOffset(l, o int) string          { return fmt.Sprintf("LIMIT %d OFFSET %d", l, o) }
func (Dialect) LikeEscape() string                   { return `ESCAPE '\'` }
func (Dialect) MaxInList() int                       { return 900 }
func (Dialect) UUIDValue(u domain.UUID) any          { return u.String() }
func (Dialect) TimeValue(t time.Time) any            { return t.UTC().Format(TimeLayout) }
func (Dialect) ParseUUID(v any) (domain.UUID, error) { return sqlrepo.ParseUUIDDefault(v) }

func (Dialect) BoolValue(b bool) any {
	if b {
		return int64(1)
	}
	return int64(0)
}

// DateValue enlaza una fecha civil como texto ISO "YYYY-MM-DD" (el orden lexicográfico es el
// cronológico).
func (Dialect) DateValue(y int, m time.Month, d int) any {
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
}

// DateExpr devuelve el placeholder sin cambios.
func (Dialect) DateExpr(ph string) string { return ph }

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "UNIQUE constraint failed", "PRIMARY KEY constraint failed", "(1555)", "(2067)")
}

// OutboxDDL devuelve las sentencias que crean la tabla del outbox.
func OutboxDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultOutboxTable
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
	id TEXT PRIMARY KEY,
	event_type TEXT NOT NULL,
	aggregate_type TEXT,
	aggregate_id TEXT,
	aggregate_version INTEGER,
	payload TEXT NOT NULL,
	occurred_at TEXT NOT NULL,
	correlation_id TEXT,
	causation_id TEXT,
	attempts INTEGER NOT NULL DEFAULT 0,
	last_error TEXT,
	processed_at TEXT)`, table),
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
	id TEXT PRIMARY KEY, aggregate_type TEXT NOT NULL, aggregate_id TEXT NOT NULL, aggregate_version INTEGER NOT NULL,
	operation TEXT NOT NULL, actor_id TEXT, actor_name TEXT, channel TEXT, import_source TEXT, import_run_id TEXT,
	import_file TEXT, correlation_id TEXT, occurred_at TEXT NOT NULL, changes TEXT, events TEXT)`, table),
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
	consumer TEXT NOT NULL, message_id TEXT NOT NULL, processed_at TEXT NOT NULL,
	PRIMARY KEY (consumer, message_id))`, table),
	}
}

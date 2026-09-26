// Package sqlite provides the SQLite dialect for sqlrepo. It does not import a driver: register
// one in the composition root (e.g. modernc.org/sqlite, pure Go, driver name "sqlite").
//
// Semantics notes: SQLite LIKE is case-insensitive for ASCII unless PRAGMA case_sensitive_like
// is enabled; booleans are stored as 0/1; UUIDs as TEXT; instants as fixed-width UTC TEXT
// (lexicographic order == chronological order).
package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// TimeLayout is the fixed-width UTC layout used to store instants.
const TimeLayout = "2006-01-02T15:04:05.000000000Z"

// Dialect implements sqlrepo.Dialect for SQLite.
type Dialect struct{}

// New returns the SQLite dialect.
func New() Dialect { return Dialect{} }

// Open wraps an opened SQLite *sql.DB.
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

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "UNIQUE constraint failed", "PRIMARY KEY constraint failed", "(1555)", "(2067)")
}

// OutboxDDL returns the statements creating the outbox table.
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

var _ sqlrepo.Dialect = Dialect{}

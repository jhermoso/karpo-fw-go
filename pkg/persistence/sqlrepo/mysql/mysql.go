// Package mysql provides the MySQL / MariaDB dialect for sqlrepo. It does not import a driver:
// register one in the composition root (github.com/go-sql-driver/mysql, driver "mysql"; use
// parseTime=true&loc=UTC in the DSN).
//
// Identities are stored as CHAR(36), booleans as TINYINT(1)/BOOLEAN, instants as DATETIME(6) UTC.
// Semantics note: default collations are case-insensitive.
package mysql

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Dialect implements sqlrepo.Dialect for MySQL 8+ / MariaDB 10.5+.
type Dialect struct{}

// New returns the MySQL dialect.
func New() Dialect { return Dialect{} }

// Open wraps an opened MySQL *sql.DB.
func Open(db *sql.DB, opts ...sqlrepo.Option) *sqlrepo.DB { return sqlrepo.New(db, New(), opts...) }

func (Dialect) Name() string                         { return "mysql" }
func (Dialect) Placeholder(int) string               { return "?" }
func (Dialect) Quote(ident string) string            { return "`" + strings.ReplaceAll(ident, "`", "``") + "`" }
func (Dialect) LimitOffset(l, o int) string          { return fmt.Sprintf("LIMIT %d OFFSET %d", l, o) }
func (Dialect) LikeEscape() string                   { return `ESCAPE '\\'` } // backslash is an escape in MySQL literals
func (Dialect) MaxInList() int                       { return 10000 }
func (Dialect) BoolValue(b bool) any                 { return b }
func (Dialect) UUIDValue(u domain.UUID) any          { return u.String() }
func (Dialect) TimeValue(t time.Time) any            { return t.UTC() }
func (Dialect) ParseUUID(v any) (domain.UUID, error) { return sqlrepo.ParseUUIDDefault(v) }

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "Error 1062", "Duplicate entry")
}

// OutboxDDL returns the statements creating the outbox table.
func OutboxDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultOutboxTable
	}
	return []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` ("+`
	id VARCHAR(64) NOT NULL PRIMARY KEY,
	event_type VARCHAR(200) NOT NULL,
	aggregate_type VARCHAR(200),
	aggregate_id VARCHAR(64),
	aggregate_version BIGINT,
	payload LONGTEXT NOT NULL,
	occurred_at DATETIME(6) NOT NULL,
	correlation_id VARCHAR(64),
	causation_id VARCHAR(64),
	attempts INT NOT NULL DEFAULT 0,
	last_error VARCHAR(1000),
	processed_at DATETIME(6),
	INDEX ix_pending (processed_at, occurred_at))`, table),
	}
}

var _ sqlrepo.Dialect = Dialect{}

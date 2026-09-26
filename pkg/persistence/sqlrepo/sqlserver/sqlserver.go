// Package sqlserver provides the Microsoft SQL Server dialect for sqlrepo. It does not import a
// driver: register one in the composition root (github.com/microsoft/go-mssqldb, driver
// "sqlserver", which binds positional arguments as @p1..@pN).
//
// Identities are stored as UNIQUEIDENTIFIER (compatible with the existing .NET Karpo databases:
// the mixed-endian byte order returned by the driver is converted transparently), booleans as
// BIT, instants as DATETIME2 in UTC.
//
// Semantics notes: the default collations are case-insensitive and ignore trailing spaces in
// equality, so Contains/StartsWith/Eq on text may match more rows than the in-memory evaluation.
// Use a case-sensitive collation (e.g. Latin1_General_100_CS_AS) where exact semantics matter.
package sqlserver

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Dialect implements sqlrepo.Dialect for SQL Server 2012+.
type Dialect struct{}

// New returns the SQL Server dialect.
func New() Dialect { return Dialect{} }

// Open wraps an opened SQL Server *sql.DB.
func Open(db *sql.DB, opts ...sqlrepo.Option) *sqlrepo.DB { return sqlrepo.New(db, New(), opts...) }

func (Dialect) Name() string                { return "sqlserver" }
func (Dialect) Placeholder(n int) string    { return fmt.Sprintf("@p%d", n) }
func (Dialect) Quote(ident string) string   { return "[" + strings.ReplaceAll(ident, "]", "]]") + "]" }
func (Dialect) LikeEscape() string          { return `ESCAPE '\'` }
func (Dialect) MaxInList() int              { return 2000 } // 2100 parameters per request
func (Dialect) BoolValue(b bool) any        { return b }
func (Dialect) UUIDValue(u domain.UUID) any { return u.String() }
func (Dialect) TimeValue(t time.Time) any   { return t.UTC() }

func (Dialect) LimitOffset(l, o int) string {
	return fmt.Sprintf("OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", o, l)
}

// ParseUUID converts UNIQUEIDENTIFIER values: the driver returns 16 bytes in mixed-endian order.
func (Dialect) ParseUUID(v any) (domain.UUID, error) {
	if b, ok := v.([]byte); ok && len(b) == 16 {
		return domain.UUID(sqlrepo.SwapGUIDBytes([16]byte(b))), nil
	}
	return sqlrepo.ParseUUIDDefault(v)
}

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "Error 2627", "Error 2601", "Violation of PRIMARY KEY", "Violation of UNIQUE KEY", "Cannot insert duplicate key")
}

// OutboxDDL returns the statements creating the outbox table.
func OutboxDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultOutboxTable
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE %s (
	id NVARCHAR(64) NOT NULL PRIMARY KEY,
	event_type NVARCHAR(200) NOT NULL,
	aggregate_type NVARCHAR(200) NULL,
	aggregate_id NVARCHAR(64) NULL,
	aggregate_version BIGINT NULL,
	payload NVARCHAR(MAX) NOT NULL,
	occurred_at DATETIME2(7) NOT NULL,
	correlation_id NVARCHAR(64) NULL,
	causation_id NVARCHAR(64) NULL,
	attempts INT NOT NULL DEFAULT 0,
	last_error NVARCHAR(1000) NULL,
	processed_at DATETIME2(7) NULL)`, table),
		fmt.Sprintf(`CREATE INDEX ix_%s_pending ON %s (processed_at, occurred_at)`, table, table),
	}
}

var _ sqlrepo.Dialect = Dialect{}

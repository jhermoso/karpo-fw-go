// Package oracle provides the Oracle Database (12c+) dialect for sqlrepo. It does not import a
// driver: register one in the composition root (e.g. github.com/sijms/go-ora/v2, pure Go,
// driver "oracle", which binds positional arguments as :1..:N).
//
// Conventions:
//   - identifiers are quoted in upper case, matching tables created with unquoted names;
//   - identities are stored as RAW(16); by default in RFC byte order, or in the .NET
//     Guid.ToByteArray order (WithDotNetGUIDs) to share tables with the C# Karpo services,
//     which store GUIDs with HEXTORAW / EF Core conventions;
//   - booleans are stored as NUMBER(1) (0/1), instants as TIMESTAMP WITH TIME ZONE (UTC);
//   - IN lists are split in chunks of 1000 (ORA-01795).
//
// Semantics note: Oracle treats the empty string as NULL.
package oracle

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Dialect implements sqlrepo.Dialect for Oracle.
type Dialect struct {
	dotNetGUIDs bool
}

// DialectOption configures the Oracle dialect.
type DialectOption func(*Dialect)

// WithDotNetGUIDs stores and reads RAW(16) identities in .NET Guid byte order.
func WithDotNetGUIDs() DialectOption { return func(d *Dialect) { d.dotNetGUIDs = true } }

// New returns the Oracle dialect.
func New(opts ...DialectOption) Dialect {
	var d Dialect
	for _, opt := range opts {
		opt(&d)
	}
	return d
}

// Open wraps an opened Oracle *sql.DB.
func Open(db *sql.DB, d Dialect, opts ...sqlrepo.Option) *sqlrepo.DB {
	return sqlrepo.New(db, d, opts...)
}

func (Dialect) Name() string              { return "oracle" }
func (Dialect) Placeholder(n int) string  { return fmt.Sprintf(":%d", n) }
func (Dialect) LikeEscape() string        { return `ESCAPE '\'` }
func (Dialect) MaxInList() int            { return 1000 }
func (Dialect) TimeValue(t time.Time) any { return t.UTC() }

func (Dialect) Quote(ident string) string {
	return `"` + strings.ToUpper(strings.ReplaceAll(ident, `"`, `""`)) + `"`
}

func (Dialect) LimitOffset(l, o int) string {
	return fmt.Sprintf("OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", o, l)
}

func (Dialect) BoolValue(b bool) any {
	if b {
		return int64(1)
	}
	return int64(0)
}

func (d Dialect) UUIDValue(u domain.UUID) any {
	b := [16]byte(u)
	if d.dotNetGUIDs {
		b = sqlrepo.SwapGUIDBytes(b)
	}
	return b[:]
}

func (d Dialect) ParseUUID(v any) (domain.UUID, error) {
	switch x := v.(type) {
	case []byte:
		if len(x) == 16 {
			b := [16]byte(x)
			if d.dotNetGUIDs {
				b = sqlrepo.SwapGUIDBytes(b)
			}
			return domain.UUID(b), nil
		}
	case string:
		if len(x) == 32 { // RAWTOHEX output
			u, err := domain.ParseUUID(x)
			if err == nil && d.dotNetGUIDs {
				u = domain.UUID(sqlrepo.SwapGUIDBytes([16]byte(u)))
			}
			return u, err
		}
	}
	return sqlrepo.ParseUUIDDefault(v)
}

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "ORA-00001")
}

// OutboxDDL returns the statements creating the outbox table.
func OutboxDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultOutboxTable
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE %s (
	id VARCHAR2(64) NOT NULL PRIMARY KEY,
	event_type VARCHAR2(200) NOT NULL,
	aggregate_type VARCHAR2(200),
	aggregate_id VARCHAR2(64),
	aggregate_version NUMBER(19),
	payload CLOB NOT NULL,
	occurred_at TIMESTAMP(6) WITH TIME ZONE NOT NULL,
	correlation_id VARCHAR2(64),
	causation_id VARCHAR2(64),
	attempts NUMBER(10) DEFAULT 0 NOT NULL,
	last_error VARCHAR2(1000),
	processed_at TIMESTAMP(6) WITH TIME ZONE)`, table),
		fmt.Sprintf(`CREATE INDEX ix_%s_pending ON %s (processed_at, occurred_at)`, table, table),
	}
}

var _ sqlrepo.Dialect = Dialect{}

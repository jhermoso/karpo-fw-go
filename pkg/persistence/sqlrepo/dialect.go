// Package sqlrepo implements the domain repository contract (and the outbox) on top of
// database/sql for any relational database, through a pluggable Dialect.
//
// It is technology agnostic in both directions:
//   - the domain never sees SQL: aggregates are mapped with a Mapping (logical fields to columns,
//     dehydrate/hydrate functions) that lives in the bounded context's infrastructure layer;
//   - the SQL is never tied to one engine: every engine-specific detail (placeholders, quoting,
//     paging, boolean and UUID representation, LIKE escaping, IN-list limits, error codes) is a
//     Dialect method. Dialects live in sub-packages (sqlite, postgres, sqlserver, oracle, mysql)
//     and do not import database drivers: the composition root chooses and registers the driver.
//
// Specifications are translated to SQL WHERE clauses (including EXISTS sub-queries for child
// collections and per-mapping translations of custom specifications). Nothing is ever filtered
// in memory: untranslatable expressions fail with domain.ErrUnsupported.
package sqlrepo

import (
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Dialect captures everything that differs between SQL engines.
type Dialect interface {
	// Name identifies the engine ("sqlite", "postgres", "sqlserver", "oracle", "mysql").
	Name() string
	// Placeholder returns the bind parameter marker for the n-th (1-based) argument.
	Placeholder(n int) string
	// Quote quotes an identifier (table or column name).
	Quote(ident string) string
	// LimitOffset returns the paging clause appended after ORDER BY.
	LimitOffset(limit, offset int) string
	// LikeEscape returns the ESCAPE clause for LIKE patterns escaped with a backslash.
	LikeEscape() string
	// MaxInList is the maximum number of items in one IN (...) list.
	MaxInList() int
	// BoolValue converts a boolean to its driver representation.
	BoolValue(b bool) any
	// UUIDValue converts a UUID to its driver representation.
	UUIDValue(u domain.UUID) any
	// ParseUUID converts a scanned column value to a UUID.
	ParseUUID(v any) (domain.UUID, error)
	// TimeValue converts an instant to its driver representation (UTC).
	TimeValue(t time.Time) any
	// IsUniqueViolation reports whether err is a primary-key/unique constraint violation.
	IsUniqueViolation(err error) bool
}

// ParseUUIDDefault parses the usual scanned representations of a UUID: canonical strings,
// 16 raw bytes (RFC byte order) and textual byte slices.
func ParseUUIDDefault(v any) (domain.UUID, error) {
	switch x := v.(type) {
	case nil:
		return domain.UUID{}, nil
	case domain.UUID:
		return x, nil
	case [16]byte:
		return domain.UUID(x), nil
	case string:
		return domain.ParseUUID(x)
	case []byte:
		if len(x) == 16 {
			return domain.UUIDFromBytes(x)
		}
		return domain.ParseUUID(string(x))
	default:
		return domain.UUID{}, fmt.Errorf("%w: cannot convert %T to uuid", domain.ErrUnsupported, v)
	}
}

// SwapGUIDBytes converts between RFC byte order and the mixed-endian order used by
// Microsoft GUIDs (SQL Server uniqueidentifier, .NET Guid.ToByteArray, EF Core on Oracle RAW(16)).
// The transformation is its own inverse.
func SwapGUIDBytes(b [16]byte) [16]byte {
	b[0], b[1], b[2], b[3] = b[3], b[2], b[1], b[0]
	b[4], b[5] = b[5], b[4]
	b[6], b[7] = b[7], b[6]
	return b
}

// EscapeLike escapes the LIKE wildcards (% and _), the SQL Server bracket and the escape
// character itself with a backslash; use together with Dialect.LikeEscape.
func EscapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`, `[`, `\[`)
	return r.Replace(s)
}

// ErrorContains reports whether err (or its text) contains any of the fragments.
// Dialects use it to classify driver errors without importing driver packages.
func ErrorContains(err error, fragments ...string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, f := range fragments {
		if strings.Contains(msg, f) {
			return true
		}
	}
	return false
}

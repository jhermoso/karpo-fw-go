// Package sqlserver ofrece el dialecto Microsoft SQL Server para sqlrepo. No importa ningún
// driver: se registra uno en la raíz de composición (github.com/microsoft/go-mssqldb, driver
// "sqlserver", que enlaza los argumentos posicionales como @p1..@pN).
//
// Las identidades se guardan como UNIQUEIDENTIFIER (compatible con las bases de datos de Karpo
// en .NET que ya existen: el orden de bytes mixed-endian que devuelve el driver se convierte de
// forma transparente), los booleanos como BIT y los instantes como DATETIME2 en UTC.
//
// Notas de semántica: las collations por defecto no distinguen mayúsculas de minúsculas e ignoran
// los espacios finales en la igualdad, así que Contains/StartsWith/Eq sobre texto pueden casar con
// más filas que la evaluación en memoria. Donde importe la semántica exacta, conviene una
// collation que distinga mayúsculas (p. ej. Latin1_General_100_CS_AS).
package sqlserver

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Dialect implementa sqlrepo.Dialect para SQL Server 2012+.
type Dialect struct{}

// New devuelve el dialecto SQL Server.
func New() Dialect { return Dialect{} }

// Open envuelve un *sql.DB de SQL Server ya abierto.
func Open(db *sql.DB, opts ...sqlrepo.Option) *sqlrepo.DB { return sqlrepo.New(db, New(), opts...) }

func (Dialect) Name() string                { return "sqlserver" }
func (Dialect) Placeholder(n int) string    { return fmt.Sprintf("@p%d", n) }
func (Dialect) Quote(ident string) string   { return "[" + strings.ReplaceAll(ident, "]", "]]") + "]" }
func (Dialect) LikeEscape() string          { return `ESCAPE '\'` }
func (Dialect) MaxInList() int              { return 2000 } // 2100 parámetros por petición
func (Dialect) BoolValue(b bool) any        { return b }
func (Dialect) UUIDValue(u domain.UUID) any { return u.String() }
func (Dialect) TimeValue(t time.Time) any   { return t.UTC() }

func (Dialect) LimitOffset(l, o int) string {
	return fmt.Sprintf("OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", o, l)
}

// ParseUUID convierte valores UNIQUEIDENTIFIER: el driver devuelve 16 bytes en orden mixed-endian.
func (Dialect) ParseUUID(v any) (domain.UUID, error) {
	if b, ok := v.([]byte); ok && len(b) == 16 {
		return domain.UUID(sqlrepo.SwapGUIDBytes([16]byte(b))), nil
	}
	return sqlrepo.ParseUUIDDefault(v)
}

// DateValue enlaza una fecha civil como medianoche UTC en una columna DATE.
func (Dialect) DateValue(y int, m time.Month, d int) any {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DateExpr devuelve el placeholder sin cambios.
func (Dialect) DateExpr(ph string) string { return ph }

// DecimalExpr convierte el texto del decimal (que go-mssqldb enlaza como NVARCHAR) con
// CAST(... AS DECIMAL(p,s)) a la medida del valor. La conversión implícita lo llevaría al tipo de
// la columna y daría un error de desbordamiento, en vez de no coincidir, con un valor más grande
// que la columna. La conversión de texto a DECIMAL no depende del idioma de la sesión. El driver
// lee DECIMAL como []byte con punto.
func (Dialect) DecimalExpr(ph string, precision, scale int) string {
	return fmt.Sprintf("CAST(%s AS DECIMAL(%d,%d))", ph, precision, scale)
}

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "Error 2627", "Error 2601", "Violation of PRIMARY KEY", "Violation of UNIQUE KEY", "Cannot insert duplicate key")
}

// OutboxDDL devuelve las sentencias que crean la tabla del outbox.
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

// AuditDDL devuelve las sentencias que crean la tabla del registro de auditoría.
func AuditDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultAuditTable
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE %s (
	id NVARCHAR(64) NOT NULL PRIMARY KEY, aggregate_type NVARCHAR(200) NOT NULL, aggregate_id NVARCHAR(64) NOT NULL,
	aggregate_version BIGINT NOT NULL, operation NVARCHAR(20) NOT NULL, actor_id NVARCHAR(64) NULL, actor_name NVARCHAR(200) NULL,
	channel NVARCHAR(20) NULL, import_source NVARCHAR(200) NULL, import_run_id NVARCHAR(64) NULL, import_file NVARCHAR(500) NULL,
	correlation_id NVARCHAR(64) NULL, occurred_at DATETIME2(7) NOT NULL, changes NVARCHAR(MAX) NULL, events NVARCHAR(2000) NULL)`, table),
		fmt.Sprintf(`CREATE INDEX ix_%s_trail ON %s (aggregate_type, aggregate_id, occurred_at)`, table, table),
	}
}

var _ sqlrepo.Dialect = Dialect{}

// InboxDDL devuelve las sentencias que crean la tabla del inbox (consumidor, id del mensaje).
func InboxDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultInboxTable
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE %s (
	consumer NVARCHAR(100) NOT NULL, message_id NVARCHAR(64) NOT NULL, processed_at DATETIME2(7) NOT NULL,
	CONSTRAINT pk_%s PRIMARY KEY (consumer, message_id))`, table, table),
	}
}

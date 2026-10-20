// Package mysql ofrece el dialecto MySQL / MariaDB para sqlrepo. No importa ningún driver: se
// registra uno en la raíz de composición (github.com/go-sql-driver/mysql, driver "mysql"; con
// parseTime=true&loc=UTC en el DSN).
//
// Las identidades se guardan como CHAR(36), los booleanos como TINYINT(1)/BOOLEAN y los instantes
// como DATETIME(6) en UTC. Nota de semántica: las collations por defecto no distinguen mayúsculas
// de minúsculas.
package mysql

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Dialect implementa sqlrepo.Dialect para MySQL 8+ / MariaDB 10.5+.
type Dialect struct{}

// New devuelve el dialecto MySQL.
func New() Dialect { return Dialect{} }

// Open envuelve un *sql.DB de MySQL ya abierto.
func Open(db *sql.DB, opts ...sqlrepo.Option) *sqlrepo.DB { return sqlrepo.New(db, New(), opts...) }

func (Dialect) Name() string                         { return "mysql" }
func (Dialect) Placeholder(int) string               { return "?" }
func (Dialect) Quote(ident string) string            { return "`" + strings.ReplaceAll(ident, "`", "``") + "`" }
func (Dialect) LimitOffset(l, o int) string          { return fmt.Sprintf("LIMIT %d OFFSET %d", l, o) }
func (Dialect) LikeEscape() string                   { return `ESCAPE '\\'` } // en los literales de MySQL la barra invertida escapa
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

// DecimalExpr convierte el texto del decimal con CAST(... AS DECIMAL(p,s)) a la medida del valor:
// MySQL compara un texto con un DECIMAL como coma flotante (double), lo que pierde cifras. El
// driver lee DECIMAL como []byte con punto.
func (Dialect) DecimalExpr(ph string, precision, scale int) string {
	return fmt.Sprintf("CAST(%s AS DECIMAL(%d,%d))", ph, precision, scale)
}

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "Error 1062", "Duplicate entry")
}

// OutboxDDL devuelve las sentencias que crean la tabla del outbox.
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

// AuditDDL devuelve las sentencias que crean la tabla del registro de auditoría.
func AuditDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultAuditTable
	}
	return []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` ("+`
	id VARCHAR(64) NOT NULL PRIMARY KEY, aggregate_type VARCHAR(200) NOT NULL, aggregate_id VARCHAR(64) NOT NULL,
	aggregate_version BIGINT NOT NULL, operation VARCHAR(20) NOT NULL, actor_id VARCHAR(64), actor_name VARCHAR(200),
	channel VARCHAR(20), import_source VARCHAR(200), import_run_id VARCHAR(64), import_file VARCHAR(500),
	correlation_id VARCHAR(64), occurred_at DATETIME(6) NOT NULL, changes LONGTEXT, events VARCHAR(2000),
	INDEX ix_trail (aggregate_type, aggregate_id, occurred_at))`, table),
	}
}

var _ sqlrepo.Dialect = Dialect{}

// InboxDDL devuelve las sentencias que crean la tabla del inbox (consumidor, id del mensaje).
func InboxDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultInboxTable
	}
	return []string{
		fmt.Sprintf("CREATE TABLE IF NOT EXISTS `%s` ("+`
	consumer VARCHAR(100) NOT NULL, message_id VARCHAR(64) NOT NULL, processed_at DATETIME(6) NOT NULL,
	PRIMARY KEY (consumer, message_id))`, table),
	}
}

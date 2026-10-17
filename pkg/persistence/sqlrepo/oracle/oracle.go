// Package oracle ofrece el dialecto Oracle Database (12c+) para sqlrepo. No importa ningún
// driver: se registra uno en la raíz de composición (p. ej. github.com/sijms/go-ora/v2, Go puro,
// driver "oracle", que enlaza los argumentos posicionales como :1..:N).
//
// Convenciones:
//   - los identificadores se entrecomillan en mayúsculas, como las tablas creadas con nombres sin
//     comillas;
//   - las identidades se guardan como RAW(16); por defecto en el orden de bytes del RFC, o en el
//     orden de Guid.ToByteArray de .NET (WithDotNetGUIDs) para compartir tablas con los servicios
//     de Karpo en C#, que guardan los GUID con las convenciones de HEXTORAW / EF Core;
//   - los booleanos se guardan como NUMBER(1) (0/1) y los instantes como TIMESTAMP WITH TIME ZONE
//     (UTC);
//   - las fechas civiles (vocab.Date) se enlazan como TO_DATE('YYYY-MM-DD'), nunca como instantes;
//   - las listas IN se parten en trozos de 1000 (ORA-01795).
//
// Zonas horarias: go-ora pone como zona horaria de la sesión la del cliente. Los instantes se
// guardan en columnas TIMESTAMP WITH TIME ZONE y se comparan bien; si se comparten tablas con los
// servicios en C# que usan columnas DATE/TIMESTAMP sin zona para los instantes, hay que poner la
// zona horaria de la sesión en UTC (ALTER SESSION SET TIME_ZONE = 'UTC') o ejecutar el proceso con
// TZ=UTC.
//
// Nota de semántica: Oracle trata la cadena vacía como NULL.
package oracle

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Dialect implementa sqlrepo.Dialect para Oracle.
type Dialect struct {
	dotNetGUIDs bool
}

// DialectOption configura el dialecto Oracle.
type DialectOption func(*Dialect)

// WithDotNetGUIDs guarda y lee las identidades RAW(16) en el orden de bytes del Guid de .NET.
func WithDotNetGUIDs() DialectOption { return func(d *Dialect) { d.dotNetGUIDs = true } }

// New devuelve el dialecto Oracle.
func New(opts ...DialectOption) Dialect {
	var d Dialect
	for _, opt := range opts {
		opt(&d)
	}
	return d
}

// Open envuelve un *sql.DB de Oracle ya abierto.
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
		if len(x) == 32 { // salida de RAWTOHEX
			u, err := domain.ParseUUID(x)
			if err == nil && d.dotNetGUIDs {
				u = domain.UUID(sqlrepo.SwapGUIDBytes([16]byte(u)))
			}
			return u, err
		}
	}
	return sqlrepo.ParseUUIDDefault(v)
}

// DateValue enlaza una fecha civil como texto ISO; DateExpr la convierte con TO_DATE. Enlazar un
// time.Time enviaría un TIMESTAMP WITH TIME ZONE, y al compararlo con una columna DATE la columna
// se convierte con la zona horaria de la sesión (que go-ora pone a la del cliente), lo que
// desplaza el día.
func (Dialect) DateValue(y int, m time.Month, d int) any {
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
}

// DateExpr envuelve el placeholder en TO_DATE(..., 'YYYY-MM-DD'), con independencia de la
// configuración NLS.
func (Dialect) DateExpr(ph string) string { return "TO_DATE(" + ph + ", 'YYYY-MM-DD')" }

func (Dialect) IsUniqueViolation(err error) bool {
	return sqlrepo.ErrorContains(err, "ORA-00001")
}

// OutboxDDL devuelve las sentencias que crean la tabla del outbox.
func OutboxDDL(table string) []string {
	if table == "" {
		table = sqlrepo.DefaultOutboxTable
	}
	return []string{
		fmt.Sprintf(`CREATE TABLE %s (
	id VARCHAR2(64 CHAR) NOT NULL PRIMARY KEY,
	event_type VARCHAR2(200 CHAR) NOT NULL,
	aggregate_type VARCHAR2(200 CHAR),
	aggregate_id VARCHAR2(64 CHAR),
	aggregate_version NUMBER(19),
	payload CLOB NOT NULL,
	occurred_at TIMESTAMP(6) WITH TIME ZONE NOT NULL,
	correlation_id VARCHAR2(64 CHAR),
	causation_id VARCHAR2(64 CHAR),
	attempts NUMBER(10) DEFAULT 0 NOT NULL,
	last_error VARCHAR2(1000 CHAR),
	processed_at TIMESTAMP(6) WITH TIME ZONE)`, table),
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
	id VARCHAR2(64 CHAR) NOT NULL PRIMARY KEY, aggregate_type VARCHAR2(200 CHAR) NOT NULL, aggregate_id VARCHAR2(64 CHAR) NOT NULL,
	aggregate_version NUMBER(19) NOT NULL, operation VARCHAR2(20 CHAR) NOT NULL, actor_id VARCHAR2(64 CHAR), actor_name VARCHAR2(200 CHAR),
	channel VARCHAR2(20 CHAR), import_source VARCHAR2(200 CHAR), import_run_id VARCHAR2(64 CHAR), import_file VARCHAR2(500 CHAR),
	correlation_id VARCHAR2(64 CHAR), occurred_at TIMESTAMP(6) WITH TIME ZONE NOT NULL, changes CLOB, events VARCHAR2(2000 CHAR))`, table),
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
	consumer VARCHAR2(100 CHAR) NOT NULL, message_id VARCHAR2(64 CHAR) NOT NULL, processed_at TIMESTAMP(6) WITH TIME ZONE NOT NULL,
	CONSTRAINT pk_%s PRIMARY KEY (consumer, message_id))`, table, table),
	}
}

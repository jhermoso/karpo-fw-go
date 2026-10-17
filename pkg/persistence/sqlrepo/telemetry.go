package sqlrepo

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

// Atributos de los spans de base de datos. El texto de la sentencia y sus argumentos no se
// registran nunca.
const (
	AttrEngine      = "db.system"
	AttrOperation   = "db.operation"
	AttrTransaction = "db.transaction" // "committed" o "rolled_back"

	OperationTransaction = "TRANSACTION"
	OperationOther       = "OTHER"

	outcomeOK    = "ok"
	outcomeError = "error"
)

// telemetry es la instrumentación de un DB; nil si no se usó WithTelemetry.
type telemetry struct {
	tracer     trace.Tracer
	meter      metrics.Meter
	duration   metrics.Histogram
	unregister []func() // indicadores del pool, los retira Close
}

// WithTelemetry instrumenta el DB: un span por unidad de trabajo (con commit o con rollback) y
// por sentencia, con el motor y la operación (SELECT, INSERT...) y nunca los argumentos; el
// histograma db.client.operation.duration por motor, operación y resultado; y el estado del pool
// de conexiones (db.client.connections) leído de sql.DB.Stats, que Close vuelve a retirar. Las
// series del pool se etiquetan con WithName (por defecto, el dialecto): cada DB que comparta un
// meter con otro del mismo motor debe tener su propio nombre. Cualquiera de los dos argumentos
// puede ser nil. Sin esta opción el DB hace exactamente lo mismo que antes.
func WithTelemetry(tracer trace.Tracer, meter metrics.Meter) Option {
	return func(db *DB) {
		m := metrics.OrNoop(meter)
		db.tel = &telemetry{
			tracer: trace.OrNoop(tracer),
			meter:  m,
			duration: m.Histogram(metrics.DBOperationDuration, metrics.UnitSeconds,
				"Duration of database client operations."),
		}
	}
}

// registerPool registra los indicadores del pool una vez aplicadas todas las opciones (el nombre
// ya es definitivo).
func (db *DB) registerPool() {
	const help = "Connections of the database pool by state."
	stat := func(read func(sql.DBStats) int) func() float64 {
		return func() float64 { return float64(read(db.sqlDB.Stats())) }
	}
	labels := func(state string) []string {
		return []string{metrics.LabelEngine, db.d.Name(), "db", db.name, metrics.LabelState, state}
	}
	m := db.tel.meter
	db.tel.unregister = []func(){
		m.Gauge(metrics.DBConnections, metrics.UnitNone, help, stat(func(s sql.DBStats) int { return s.OpenConnections }), labels("open")...),
		m.Gauge(metrics.DBConnections, metrics.UnitNone, help, stat(func(s sql.DBStats) int { return s.InUse }), labels("in_use")...),
		m.Gauge(metrics.DBConnections, metrics.UnitNone, help, stat(func(s sql.DBStats) int { return s.Idle }), labels("idle")...),
		m.Gauge(metrics.DBConnections, metrics.UnitNone, help, stat(func(s sql.DBStats) int { return int(s.WaitCount) }), labels("waited")...),
	}
}

// unregisterPool retira los indicadores del pool: un pool cerrado (el backend antiguo tras un hot
// swap) no debe seguir informando ni el meter debe mantenerlo vivo.
func (db *DB) unregisterPool() {
	if db.tel == nil {
		return
	}
	for _, f := range db.tel.unregister {
		f()
	}
	db.tel.unregister = nil
}

func (t *telemetry) record(ctx context.Context, engine, operation string, start time.Time, err error) {
	outcome := outcomeOK
	if err != nil {
		outcome = outcomeError
	}
	t.duration.Record(ctx, time.Since(start).Seconds(),
		metrics.LabelEngine, engine, metrics.LabelOperation, operation, metrics.LabelOutcome, outcome)
}

// Operation devuelve el tipo de una sentencia SQL a partir de su primera palabra clave, dentro de
// un conjunto cerrado.
func Operation(query string) string {
	q := strings.TrimLeft(query, " \t\r\n(")
	end := strings.IndexAny(q, " \t\r\n(;")
	if end < 0 {
		end = len(q)
	}
	switch op := strings.ToUpper(q[:end]); op {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "MERGE", "WITH", "CREATE", "ALTER", "DROP", "BEGIN", "DECLARE":
		return op
	}
	return OperationOther
}

// tracedExecutor envuelve el ejecutor de un DB con un span y una duración por sentencia.
type tracedExecutor struct {
	db *DB
	ex executor
}

func (t tracedExecutor) start(ctx context.Context, query string) (string, time.Time, trace.Span) {
	op := Operation(query)
	_, span := t.db.tel.tracer.Start(ctx, "db "+op, trace.WithKind(trace.KindClient))
	span.SetAttributes(AttrEngine, t.db.d.Name(), AttrOperation, op)
	return op, time.Now(), span
}

func (t tracedExecutor) end(ctx context.Context, op string, start time.Time, span trace.Span, err error) {
	t.db.tel.record(ctx, t.db.d.Name(), op, start, err)
	span.RecordError(err)
	span.End()
}

func (t tracedExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	op, start, span := t.start(ctx, query)
	res, err := t.ex.ExecContext(ctx, query, args...)
	t.end(ctx, op, start, span, err)
	return res, err
}

func (t tracedExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	op, start, span := t.start(ctx, query)
	rows, err := t.ex.QueryContext(ctx, query, args...)
	t.end(ctx, op, start, span, err)
	return rows, err
}

func (t tracedExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	op, start, span := t.start(ctx, query)
	row := t.ex.QueryRowContext(ctx, query, args...)
	t.end(ctx, op, start, span, row.Err())
	return row
}

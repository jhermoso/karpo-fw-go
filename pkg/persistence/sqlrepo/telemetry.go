package sqlrepo

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

// Span attributes of database spans. The statement text and its arguments are never recorded.
const (
	AttrEngine      = "db.system"
	AttrOperation   = "db.operation"
	AttrTransaction = "db.transaction" // "committed" or "rolled_back"

	OperationTransaction = "TRANSACTION"
	OperationOther       = "OTHER"

	outcomeOK    = "ok"
	outcomeError = "error"
)

// telemetry is the instrumentation of a DB; nil when WithTelemetry was not used.
type telemetry struct {
	tracer   trace.Tracer
	meter    metrics.Meter
	duration metrics.Histogram
}

// WithTelemetry instruments the DB: a span per unit of work (committed or rolled back) and per
// statement, with the engine and the operation (SELECT, INSERT...) and never the arguments; the
// histogram db.client.operation.duration by engine, operation and outcome; and the state of the
// connection pool (db.client.connections) read from sql.DB.Stats. Either argument may be nil.
// Without this option the DB does exactly what it did before.
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

// registerPool registers the pool gauges once every option is applied (the name is final).
func (db *DB) registerPool() {
	const help = "Connections of the database pool by state."
	stat := func(read func(sql.DBStats) int) func() float64 {
		return func() float64 { return float64(read(db.sqlDB.Stats())) }
	}
	labels := func(state string) []string {
		return []string{metrics.LabelEngine, db.d.Name(), "db", db.name, metrics.LabelState, state}
	}
	m := db.tel.meter
	m.Gauge(metrics.DBConnections, metrics.UnitNone, help, stat(func(s sql.DBStats) int { return s.OpenConnections }), labels("open")...)
	m.Gauge(metrics.DBConnections, metrics.UnitNone, help, stat(func(s sql.DBStats) int { return s.InUse }), labels("in_use")...)
	m.Gauge(metrics.DBConnections, metrics.UnitNone, help, stat(func(s sql.DBStats) int { return s.Idle }), labels("idle")...)
	m.Gauge(metrics.DBConnections, metrics.UnitNone, help, stat(func(s sql.DBStats) int { return int(s.WaitCount) }), labels("waited")...)
}

func (t *telemetry) record(ctx context.Context, engine, operation string, start time.Time, err error) {
	outcome := outcomeOK
	if err != nil {
		outcome = outcomeError
	}
	t.duration.Record(ctx, time.Since(start).Seconds(),
		metrics.LabelEngine, engine, metrics.LabelOperation, operation, metrics.LabelOutcome, outcome)
}

// Operation returns the kind of a SQL statement from its first keyword, out of a closed set.
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

// tracedExecutor wraps the executor of a DB with a span and a duration per statement.
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

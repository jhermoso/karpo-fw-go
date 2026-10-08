package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/metrics"
	metricsvanilla "github.com/jhermoso/karpo-fw-go/pkg/metrics/vanilla"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlconformance"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/trace"
	tracevanilla "github.com/jhermoso/karpo-fw-go/pkg/trace/vanilla"
)

func openObserved(t testing.TB, opts ...sqlrepo.Option) *sqlrepo.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "karpo.db")
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	db := sqlite.Open(raw, opts...)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newOutbox(t testing.TB, db *sqlrepo.DB) *sqlrepo.Outbox {
	t.Helper()
	for _, stmt := range sqlite.OutboxDDL("outbox") {
		if _, err := db.SQL().Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	box, err := sqlrepo.NewOutbox(db, "outbox")
	if err != nil {
		t.Fatal(err)
	}
	return box
}

const secret = "ada@example.com"

func message(id string) application.OutboxMessage {
	return application.OutboxMessage{ID: id, EventType: "parties.party-registered.v1", AggregateType: "Party",
		AggregateID: "p-" + id, Payload: []byte(`{"email":"` + secret + `"}`), OccurredAt: time.Now().UTC(), CorrelationID: "flow-" + id}
}

// The conformance suites must pass unchanged with the telemetry on.
func TestConformance_WithTelemetry(t *testing.T) {
	opt := func() sqlrepo.Option {
		return sqlrepo.WithTelemetry(tracevanilla.New(tracevanilla.WithExporter(tracevanilla.NewRecorder(0))), metricsvanilla.NewRegistry())
	}
	t.Run("repository", func(t *testing.T) { sqlconformance.Run(t, openObserved(t, opt())) })
	t.Run("audit", func(t *testing.T) { sqlconformance.RunAuditLog(t, openObserved(t, opt())) })
	t.Run("inbox", func(t *testing.T) { sqlconformance.RunInbox(t, openObserved(t, opt())) })
}

func TestTelemetry_TransactionAndStatementSpans(t *testing.T) {
	rec := tracevanilla.NewRecorder(0)
	tracer := tracevanilla.New(tracevanilla.WithExporter(rec))
	reg := metricsvanilla.NewRegistry()
	db := openObserved(t, sqlrepo.WithTelemetry(tracer, reg))
	box := newOutbox(t, db)

	ctx, useCase := tracer.Start(context.Background(), "RegisterParty")
	if err := db.Do(ctx, func(ctx context.Context) error { return box.Append(ctx, message("1")) }); err != nil {
		t.Fatal(err)
	}
	useCase.End()

	tx := rec.Named("db transaction")
	insert := rec.Named("db INSERT")
	if len(tx) != 1 || len(insert) != 1 {
		t.Fatalf("expected one transaction and one INSERT span: %+v", rec.Spans())
	}
	if tx[0].ParentID != useCase.Context().SpanID || insert[0].ParentID != tx[0].Context.SpanID {
		t.Fatal("the chain must be use case -> transaction -> INSERT")
	}
	if insert[0].Context.TraceID != useCase.Context().TraceID || insert[0].Kind != trace.KindClient {
		t.Fatalf("unexpected INSERT span: %+v", insert[0])
	}
	if tx[0].Attr(sqlrepo.AttrTransaction) != "committed" || tx[0].Err != nil {
		t.Fatalf("the transaction span must say it committed: %v", tx[0].Attributes)
	}
	if insert[0].Attr(sqlrepo.AttrEngine) != "sqlite" || insert[0].Attr(sqlrepo.AttrOperation) != "INSERT" {
		t.Fatalf("unexpected INSERT attributes: %v", insert[0].Attributes)
	}

	for _, labels := range [][]string{
		{"engine", "sqlite", "operation", "INSERT", "outcome", "ok"},
		{"engine", "sqlite", "operation", "TRANSACTION", "outcome", "ok"},
	} {
		if n := reg.HistogramCount(metrics.DBOperationDuration, labels...); n != 1 {
			t.Fatalf("expected one observation for %v: %v", labels, reg.SeriesLabels(metrics.DBOperationDuration))
		}
	}

	// A statement outside a unit of work is a span of its own, child of the caller.
	rec.Reset()
	if _, err := box.Pending(ctx, 10, 5); err != nil {
		t.Fatal(err)
	}
	if sel := rec.Named("db SELECT"); len(sel) != 1 || sel[0].ParentID != useCase.Context().SpanID {
		t.Fatalf("expected one SELECT span under the caller: %+v", rec.Spans())
	}
}

func TestTelemetry_NeverRecordsArguments(t *testing.T) {
	rec := tracevanilla.NewRecorder(0)
	reg := metricsvanilla.NewRegistry()
	db := openObserved(t, sqlrepo.WithTelemetry(tracevanilla.New(tracevanilla.WithExporter(rec)), reg))
	box := newOutbox(t, db)

	ctx := context.Background()
	if err := db.Do(ctx, func(ctx context.Context) error { return box.Append(ctx, message("1"), message("2")) }); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Pending(ctx, 10, 5); err != nil {
		t.Fatal(err)
	}
	if err := box.MarkFailed(ctx, "1", errors.New("smtp: "+secret+" rejected")); err != nil {
		t.Fatal(err)
	}
	// A failing statement: its error text reaches the span, its arguments do not.
	if _, err := db.ExecContext(ctx, "INSERT INTO no_such_table (a) VALUES (?)", secret); err == nil {
		t.Fatal("expected an error")
	}

	spans := rec.Spans()
	if len(spans) < 5 {
		t.Fatalf("expected spans for every statement, got %d", len(spans))
	}
	allowed := map[string]bool{sqlrepo.AttrEngine: true, sqlrepo.AttrOperation: true, sqlrepo.AttrTransaction: true}
	for _, s := range spans {
		for i := 0; i+1 < len(s.Attributes); i += 2 {
			if key, _ := s.Attributes[i].(string); !allowed[key] {
				t.Errorf("span %q carries an attribute outside the closed set: %v", s.Name, s.Attributes)
			}
		}
		dump := fmt.Sprintf("%s %v %v", s.Name, s.Attributes, s.Err)
		for _, forbidden := range []string{secret, "flow-1", "p-1", "party-registered", "VALUES"} {
			if strings.Contains(dump, forbidden) {
				t.Errorf("span %q leaks %q: %s", s.Name, forbidden, dump)
			}
		}
	}
	for _, labels := range reg.SeriesLabels(metrics.DBOperationDuration) {
		if len(labels) != 6 || labels[1] != "sqlite" {
			t.Errorf("unexpected series labels: %v", labels)
		}
	}
	if n := reg.HistogramCount(metrics.DBOperationDuration, "engine", "sqlite", "operation", "INSERT", "outcome", "error"); n != 1 {
		t.Fatalf("the failing statement must be measured as an error: %v", reg.SeriesLabels(metrics.DBOperationDuration))
	}
}

func TestTelemetry_RollbackAndPanic(t *testing.T) {
	rec := tracevanilla.NewRecorder(0)
	reg := metricsvanilla.NewRegistry()
	db := openObserved(t, sqlrepo.WithTelemetry(tracevanilla.New(tracevanilla.WithExporter(rec)), reg))
	box := newOutbox(t, db)
	boom := errors.New("rule violated")

	err := db.Do(context.Background(), func(ctx context.Context) error {
		if err := box.Append(ctx, message("1")); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("unexpected error: %v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("the panic must cross the unit of work")
			}
		}()
		_ = db.Do(context.Background(), func(context.Context) error { panic("kaboom") })
	}()

	tx := rec.Named("db transaction")
	if len(tx) != 2 {
		t.Fatalf("expected two transaction spans, got %d", len(tx))
	}
	for _, s := range tx {
		if s.Attr(sqlrepo.AttrTransaction) != "rolled_back" {
			t.Fatalf("the transaction span must say it rolled back: %v", s.Attributes)
		}
	}
	if !errors.Is(tx[0].Err, boom) {
		t.Fatalf("the span must carry the error that rolled it back: %v", tx[0].Err)
	}
	pending, err := box.Pending(context.Background(), 10, 5)
	if err != nil || len(pending) != 0 {
		t.Fatalf("nothing may survive the rollback: %v, %v", pending, err)
	}
	// Both roll back, so both are failures: the error and the panic.
	if n := reg.HistogramCount(metrics.DBOperationDuration, "engine", "sqlite", "operation", "TRANSACTION", "outcome", "error"); n != 2 {
		t.Fatalf("both rolled back transactions must be measured as errors: %v", reg.SeriesLabels(metrics.DBOperationDuration))
	}
	if n := reg.HistogramCount(metrics.DBOperationDuration, "engine", "sqlite", "operation", "TRANSACTION", "outcome", "ok"); n != 0 {
		t.Fatalf("a panic is not an ok transaction: %v", reg.SeriesLabels(metrics.DBOperationDuration))
	}
}

func TestTelemetry_PoolGauges(t *testing.T) {
	reg := metricsvanilla.NewRegistry()
	db := openObserved(t, sqlrepo.WithTelemetry(nil, reg), sqlrepo.WithName("parties"))
	if err := db.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	open, ok := reg.GaugeValue(metrics.DBConnections, "engine", "sqlite", "db", "parties", "state", "open")
	if !ok || open != 1 {
		t.Fatalf("open connections = %v, %v", open, ok)
	}
	for _, state := range []string{"in_use", "idle", "waited"} {
		if _, ok := reg.GaugeValue(metrics.DBConnections, "engine", "sqlite", "db", "parties", "state", state); !ok {
			t.Errorf("missing pool gauge %q", state)
		}
	}
}

func TestOperation(t *testing.T) {
	for query, want := range map[string]string{
		"SELECT 1":                       "SELECT",
		"  \n\tinsert into t values (?)": "INSERT",
		"(SELECT 1)":                     "SELECT",
		"WITH x AS (SELECT 1) SELECT 1":  "WITH",
		"UPDATE t SET a = ?":             "UPDATE",
		"DELETE FROM t":                  "DELETE",
		"PRAGMA foreign_keys":            "OTHER",
		"":                               "OTHER",
		"'; DROP TABLE users; --":        "OTHER",
	} {
		if got := sqlrepo.Operation(query); got != want {
			t.Errorf("Operation(%q) = %q, want %q", query, got, want)
		}
	}
}

// BenchmarkAppend compares a unit of work with one INSERT with and without telemetry.
func BenchmarkAppend(b *testing.B) {
	for _, c := range []struct {
		name string
		opts []sqlrepo.Option
	}{
		{"plain", nil},
		{"telemetry", []sqlrepo.Option{sqlrepo.WithTelemetry(
			tracevanilla.New(tracevanilla.WithExporter(tracevanilla.NewRecorder(16))), metricsvanilla.NewRegistry())}},
	} {
		b.Run(c.name, func(b *testing.B) {
			db := openObserved(b, c.opts...)
			box := newOutbox(b, db)
			ctx := context.Background()
			i := 0
			for b.Loop() {
				i++
				if err := db.Do(ctx, func(ctx context.Context) error { return box.Append(ctx, message(fmt.Sprint(i))) }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A panic inside the unit of work rolls it back and must be measured as a failure.
func TestTelemetry_PanicInsideTheUnitOfWorkIsAFailure(t *testing.T) {
	rec := tracevanilla.NewRecorder(0)
	reg := metricsvanilla.NewRegistry()
	db := openObserved(t, sqlrepo.WithTelemetry(tracevanilla.New(tracevanilla.WithExporter(rec)), reg))

	func() {
		defer func() {
			if p := recover(); p != "boom" {
				t.Fatalf("the panic must keep propagating, got %v", p)
			}
		}()
		_ = db.Do(context.Background(), func(context.Context) error { panic("boom") })
	}()

	engine := db.Dialect().Name()
	if n := reg.HistogramCount(metrics.DBOperationDuration, "engine", engine, "operation", sqlrepo.OperationTransaction, "outcome", "error"); n != 1 {
		t.Fatalf("a panicking transaction must be measured as an error: %v", reg.SeriesLabels(metrics.DBOperationDuration))
	}
	tx := rec.Named("db transaction")
	if len(tx) != 1 || tx[0].Err == nil || !strings.Contains(tx[0].Err.Error(), "boom") {
		t.Fatalf("the transaction span must carry the panic: %+v", tx)
	}
}

// After a hot swap the old pool is closed: its gauges go away (they no longer report a closed
// pool nor keep it alive), and closing it never removes the gauges of the new pool.
func TestTelemetry_ClosingThePoolRemovesItsGauges(t *testing.T) {
	reg := metricsvanilla.NewRegistry()
	old := openObserved(t, sqlrepo.WithTelemetry(nil, reg), sqlrepo.WithName("old"))
	if _, ok := reg.GaugeValue(metrics.DBConnections, "engine", "sqlite", "db", "old", "state", "open"); !ok {
		t.Fatal("the pool gauges must be registered")
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.GaugeValue(metrics.DBConnections, "engine", "sqlite", "db", "old", "state", "open"); ok {
		t.Fatal("a closed pool must not keep its gauges")
	}

	// Same name: the new pool replaces the series, and closing the old one leaves it alone.
	first := openObserved(t, sqlrepo.WithTelemetry(nil, reg), sqlrepo.WithName("parties"))
	second := openObserved(t, sqlrepo.WithTelemetry(nil, reg), sqlrepo.WithName("parties"))
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if open, ok := reg.GaugeValue(metrics.DBConnections, "engine", "sqlite", "db", "parties", "state", "open"); !ok || open != 1 {
		t.Fatalf("the gauges of the live pool must survive closing the replaced one: %v %v", open, ok)
	}
}

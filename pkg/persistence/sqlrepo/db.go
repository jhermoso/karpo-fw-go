package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/trace"
)

// DB binds a *sql.DB to a Dialect and provides the unit of work shared by every repository and
// outbox created on it. It satisfies domain.UnitOfWork and hotswap.Backend.
type DB struct {
	sqlDB  *sql.DB
	d      Dialect
	name   string
	txOpts *sql.TxOptions
	tel    *telemetry
}

// Option configures a DB.
type Option func(*DB)

// WithName sets the backend name reported to diagnostics and hot-swap (default: dialect name).
func WithName(name string) Option { return func(db *DB) { db.name = name } }

// WithTxOptions sets the isolation level / read-only flag used by units of work.
func WithTxOptions(opts *sql.TxOptions) Option { return func(db *DB) { db.txOpts = opts } }

// New wraps an opened *sql.DB. The driver must match the dialect.
func New(sqlDB *sql.DB, d Dialect, opts ...Option) *DB {
	db := &DB{sqlDB: sqlDB, d: d, name: d.Name()}
	for _, opt := range opts {
		opt(db)
	}
	if db.tel != nil {
		db.registerPool()
	}
	return db
}

// Name identifies the backend.
func (db *DB) Name() string { return db.name }

// Dialect returns the SQL dialect.
func (db *DB) Dialect() Dialect { return db.d }

// SQL exposes the underlying *sql.DB (pool tuning, migrations).
func (db *DB) SQL() *sql.DB { return db.sqlDB }

// Ping checks connectivity (readiness probes).
func (db *DB) Ping(ctx context.Context) error { return db.sqlDB.PingContext(ctx) }

// Close closes the connection pool.
func (db *DB) Close() error { return db.sqlDB.Close() }

type txKey struct{ db *DB }

type sqlTx struct {
	tx         *sql.Tx
	onRollback []func()
}

func (t *sqlTx) rollbackHooks() {
	for i := len(t.onRollback) - 1; i >= 0; i-- {
		t.onRollback[i]()
	}
	t.onRollback = nil
}

// Do runs fn in a database transaction bound to the context passed to fn. Repositories and
// outboxes of this DB called with that context join the transaction. Nested calls join the
// outer transaction. The transaction commits if fn returns nil and rolls back otherwise
// (including panics).
func (db *DB) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if db.tx(ctx) != nil {
		return fn(ctx)
	}
	outcome := "rolled_back" // also what a panic leaves
	if db.tel != nil {
		start := time.Now()
		var span trace.Span
		ctx, span = db.tel.tracer.Start(ctx, "db transaction", trace.WithKind(trace.KindClient))
		span.SetAttributes(AttrEngine, db.d.Name(), AttrOperation, OperationTransaction)
		defer func() {
			span.SetAttributes(AttrTransaction, outcome)
			span.RecordError(err)
			span.End()
			db.tel.record(ctx, db.d.Name(), OperationTransaction, start, err)
		}()
	}
	tx, err := db.sqlDB.BeginTx(ctx, db.txOpts)
	if err != nil {
		return err
	}
	st := &sqlTx{tx: tx}
	ctx = context.WithValue(ctx, txKey{db}, st)

	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			st.rollbackHooks()
			panic(r)
		}
	}()

	if err = fn(ctx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			err = errors.Join(err, rbErr)
		}
		st.rollbackHooks()
		return err
	}
	if err = tx.Commit(); err != nil {
		st.rollbackHooks()
		return err
	}
	outcome = "committed"
	return nil
}

func (db *DB) tx(ctx context.Context) *sqlTx {
	st, _ := ctx.Value(txKey{db}).(*sqlTx)
	return st
}

// InTx reports whether ctx carries a unit of work of this DB.
func (db *DB) InTx(ctx context.Context) bool { return db.tx(ctx) != nil }

func (db *DB) onRollback(ctx context.Context, fn func()) {
	if st := db.tx(ctx); st != nil {
		st.onRollback = append(st.onRollback, fn)
	}
}

type executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (db *DB) executor(ctx context.Context) executor {
	var ex executor = db.sqlDB
	if st := db.tx(ctx); st != nil {
		ex = st.tx
	}
	if db.tel != nil {
		return tracedExecutor{db: db, ex: ex}
	}
	return ex
}

// ExecContext executes a statement, joining the unit of work in ctx if any.
// Useful for read-model projections and migrations that must share the transaction.
func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.executor(ctx).ExecContext(ctx, query, args...)
}

// QueryContext runs a query, joining the unit of work in ctx if any
// (read models / query handlers that bypass aggregates).
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.executor(ctx).QueryContext(ctx, query, args...)
}

// Insert inserts one row into table with the dialect's value binding (UUIDs, booleans, dates,
// decimals...), joining the unit of work in ctx. Meant for seeds and data migrations
// (Migration.Run); aggregates are saved through repositories.
func (db *DB) Insert(ctx context.Context, table string, v Values) error {
	if err := checkIdent("table", table); err != nil {
		return err
	}
	cols := make([]string, 0, len(v))
	for c := range v {
		if err := checkIdent("column", c); err != nil {
			return err
		}
		cols = append(cols, c)
	}
	slices.Sort(cols)
	b := &Builder{d: db.d}
	quoted := make([]string, len(cols))
	phs := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = db.d.Quote(c)
		ph, err := b.Arg(v[c])
		if err != nil {
			return err
		}
		phs[i] = ph
	}
	_, err := db.executor(ctx).ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		db.d.Quote(table), strings.Join(quoted, ", "), strings.Join(phs, ", ")), b.args...)
	return err
}

// Select reads whole tables such as small catalogs: SELECT columns FROM table ORDER BY orderBy,
// returned as typed rows. It joins the unit of work in ctx. Queries over aggregates go through
// repositories and specifications.
func (db *DB) Select(ctx context.Context, table string, columns []string, orderBy ...string) ([]*Row, error) {
	if err := checkIdent("table", table); err != nil {
		return nil, err
	}
	quote := func(cs []string) ([]string, error) {
		out := make([]string, len(cs))
		for i, c := range cs {
			if err := checkIdent("column", c); err != nil {
				return nil, err
			}
			out[i] = db.d.Quote(c)
		}
		return out, nil
	}
	sel, err := quote(columns)
	if err != nil {
		return nil, err
	}
	ord, err := quote(orderBy)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(sel, ", "), db.d.Quote(table))
	if len(ord) > 0 {
		query += " ORDER BY " + strings.Join(ord, ", ")
	}
	rows, err := db.executor(ctx).QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: select %s: %w", table, err)
	}
	return scanAll(db.d, rows, columns)
}

// Update sets columns of the rows of table matching every where column (equality), with the
// dialect's value binding, joining the unit of work in ctx. It returns the rows affected.
// Meant for data migrations; aggregates are saved through repositories.
func (db *DB) Update(ctx context.Context, table string, set, where Values) (int64, error) {
	if err := checkIdent("table", table); err != nil {
		return 0, err
	}
	if len(set) == 0 || len(where) == 0 {
		return 0, fmt.Errorf("sqlrepo: update %s needs columns to set and a condition", table)
	}
	b := &Builder{d: db.d}
	part := func(v Values, sep string) (string, error) {
		cols := make([]string, 0, len(v))
		for c := range v {
			if err := checkIdent("column", c); err != nil {
				return "", err
			}
			cols = append(cols, c)
		}
		slices.Sort(cols)
		out := make([]string, len(cols))
		for i, c := range cols {
			ph, err := b.Arg(v[c])
			if err != nil {
				return "", err
			}
			out[i] = db.d.Quote(c) + " = " + ph
		}
		return strings.Join(out, sep), nil
	}
	s, err := part(set, ", ")
	if err != nil {
		return 0, err
	}
	w, err := part(where, " AND ")
	if err != nil {
		return 0, err
	}
	res, err := db.executor(ctx).ExecContext(ctx, fmt.Sprintf("UPDATE %s SET %s WHERE %s", db.d.Quote(table), s, w), b.args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// maxBatchParams keeps every batch under the smallest parameter limit (SQL Server: 2100).
const maxBatchParams = 2000

// InsertMany inserts rows (values in columns order) in batches with the dialect's value binding,
// joining the unit of work in ctx: multi-row VALUES, or INSERT ALL on Oracle (valid before 23ai).
// Meant for seeds and data migrations of reference data.
func (db *DB) InsertMany(ctx context.Context, table string, columns []string, rows [][]any) error {
	if err := checkIdent("table", table); err != nil {
		return err
	}
	cols := make([]string, len(columns))
	for i, c := range columns {
		if err := checkIdent("column", c); err != nil {
			return err
		}
		cols[i] = db.d.Quote(c)
	}
	if len(cols) == 0 {
		return fmt.Errorf("sqlrepo: insert into %s needs columns", table)
	}
	per := max(1, min(500, maxBatchParams/len(cols)))
	colList := strings.Join(cols, ", ")
	for start := 0; start < len(rows); start += per {
		batch := rows[start:min(start+per, len(rows))]
		b := &Builder{d: db.d}
		tuples := make([]string, len(batch))
		for i, row := range batch {
			if len(row) != len(cols) {
				return fmt.Errorf("sqlrepo: insert into %s: row %d has %d values for %d columns", table, start+i, len(row), len(cols))
			}
			phs := make([]string, len(row))
			for j, v := range row {
				ph, err := b.Arg(v)
				if err != nil {
					return err
				}
				phs[j] = ph
			}
			tuples[i] = "(" + strings.Join(phs, ", ") + ")"
		}
		var query string
		if db.d.Name() == "oracle" {
			var sb strings.Builder
			sb.WriteString("INSERT ALL")
			for _, t := range tuples {
				fmt.Fprintf(&sb, " INTO %s (%s) VALUES %s", db.d.Quote(table), colList, t)
			}
			sb.WriteString(" SELECT 1 FROM DUAL")
			query = sb.String()
		} else {
			query = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", db.d.Quote(table), colList, strings.Join(tuples, ", "))
		}
		if _, err := db.executor(ctx).ExecContext(ctx, query, b.args...); err != nil {
			return fmt.Errorf("sqlrepo: insert into %s: %w", table, err)
		}
	}
	return nil
}

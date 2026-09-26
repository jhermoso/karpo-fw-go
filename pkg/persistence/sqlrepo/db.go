package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
)

// DB binds a *sql.DB to a Dialect and provides the unit of work shared by every repository and
// outbox created on it. It satisfies domain.UnitOfWork and hotswap.Backend.
type DB struct {
	sqlDB  *sql.DB
	d      Dialect
	name   string
	txOpts *sql.TxOptions
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
	if st := db.tx(ctx); st != nil {
		return st.tx
	}
	return db.sqlDB
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

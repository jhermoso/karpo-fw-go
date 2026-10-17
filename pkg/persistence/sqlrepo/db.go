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

// DB une un *sql.DB a un Dialect y aporta la unidad de trabajo que comparten todos los
// repositorios y outboxes creados sobre él. Cumple domain.UnitOfWork y hotswap.Backend.
type DB struct {
	sqlDB  *sql.DB
	d      Dialect
	name   string
	txOpts *sql.TxOptions
	tel    *telemetry
}

// Option configura un DB.
type Option func(*DB)

// WithName fija el nombre del backend que se comunica al diagnóstico y al cambio en caliente (por defecto,
// el nombre del dialecto).
func WithName(name string) Option { return func(db *DB) { db.name = name } }

// WithTxOptions fija el nivel de aislamiento y la marca de sólo lectura que usan las unidades de
// trabajo.
func WithTxOptions(opts *sql.TxOptions) Option { return func(db *DB) { db.txOpts = opts } }

// New envuelve un *sql.DB ya abierto. El driver debe corresponder al dialecto.
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

// Name identifica el backend.
func (db *DB) Name() string { return db.name }

// Dialect devuelve el dialecto SQL.
func (db *DB) Dialect() Dialect { return db.d }

// SQL expone el *sql.DB subyacente (ajuste del pool, migraciones).
func (db *DB) SQL() *sql.DB { return db.sqlDB }

// Ping comprueba la conectividad (sondas de disponibilidad).
func (db *DB) Ping(ctx context.Context) error { return db.sqlDB.PingContext(ctx) }

// Close cierra el pool de conexiones y retira sus indicadores.
func (db *DB) Close() error {
	db.unregisterPool()
	return db.sqlDB.Close()
}

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

// Do ejecuta fn en una transacción de base de datos ligada al contexto que recibe fn. Los
// repositorios y outboxes de este DB llamados con ese contexto se unen a la transacción. Las
// llamadas anidadas se unen a la transacción exterior. La transacción hace commit si fn devuelve
// nil y rollback en otro caso (también ante un panic).
func (db *DB) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if db.tx(ctx) != nil {
		return fn(ctx)
	}
	outcome := "rolled_back" // también lo que deja un panic
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
			// El defer de la telemetría se ejecuta después de éste: debe ver el panic como un
			// fallo, no medir como correcta una transacción deshecha.
			err = panicError(r)
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

// panicError convierte un valor recuperado en el error que dejó un panic.
func panicError(r any) error {
	if err, ok := r.(error); ok {
		return fmt.Errorf("panic: %w", err)
	}
	return fmt.Errorf("panic: %v", r)
}

func (db *DB) tx(ctx context.Context) *sqlTx {
	st, _ := ctx.Value(txKey{db}).(*sqlTx)
	return st
}

// InTx dice si ctx lleva una unidad de trabajo de este DB.
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

// ExecContext ejecuta una sentencia, uniéndose a la unidad de trabajo de ctx si la hay.
// Sirve para las proyecciones de modelos de lectura y las migraciones que deben compartir la
// transacción.
func (db *DB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.executor(ctx).ExecContext(ctx, query, args...)
}

// QueryContext ejecuta una consulta, uniéndose a la unidad de trabajo de ctx si la hay
// (modelos de lectura y manejadores de consultas que no pasan por los agregados).
func (db *DB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return db.executor(ctx).QueryContext(ctx, query, args...)
}

// Insert inserta una fila en table con el enlace de valores del dialecto (UUID, booleanos,
// fechas, decimales...), uniéndose a la unidad de trabajo de ctx. Pensado para semillas y
// migraciones de datos (Migration.Run); los agregados se guardan con los repositorios.
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

// Select lee tablas enteras, como los catálogos pequeños: SELECT columns FROM table ORDER BY
// orderBy, devuelto como filas tipadas. Se une a la unidad de trabajo de ctx. Las consultas sobre
// agregados pasan por los repositorios y las especificaciones.
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

// Update fija columnas de las filas de table que cumplen todas las columnas de where (igualdad),
// con el enlace de valores del dialecto, uniéndose a la unidad de trabajo de ctx. Devuelve las
// filas afectadas. Pensado para migraciones de datos; los agregados se guardan con los
// repositorios.
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

// Delete borra las filas de table que cumplen todas las columnas de where, uniéndose a la unidad
// de trabajo de ctx, y devuelve cuántas ha borrado. Pensado para migraciones de datos; los
// agregados se borran con su repositorio.
func (db *DB) Delete(ctx context.Context, table string, where Values) (int64, error) {
	if err := checkIdent("table", table); err != nil {
		return 0, err
	}
	if len(where) == 0 {
		return 0, fmt.Errorf("sqlrepo: delete from %s needs a condition", table)
	}
	b := &Builder{d: db.d}
	cols := make([]string, 0, len(where))
	for c := range where {
		if err := checkIdent("column", c); err != nil {
			return 0, err
		}
		cols = append(cols, c)
	}
	slices.Sort(cols)
	conds := make([]string, len(cols))
	for i, c := range cols {
		ph, err := b.Arg(where[c])
		if err != nil {
			return 0, err
		}
		conds[i] = db.d.Quote(c) + " = " + ph
	}
	res, err := db.executor(ctx).ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s", db.d.Quote(table), strings.Join(conds, " AND ")), b.args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// maxBatchParams mantiene cada lote por debajo del límite de parámetros más bajo (SQL Server:
// 2100).
const maxBatchParams = 2000

// InsertMany inserta rows (valores en el orden de columns) por lotes con el enlace de valores del
// dialecto, uniéndose a la unidad de trabajo de ctx: VALUES de varias filas, o INSERT ALL en
// Oracle (válido antes de 23ai). Pensado para semillas y migraciones de datos de referencia.
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

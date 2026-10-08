package sqlrepo

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Repository is the generic SQL implementation of domain.Repository.
type Repository[ID domain.Identifier, T domain.AggregateRoot[ID]] struct {
	db   *DB
	m    Mapping[ID, T]
	root *table
	kind string
}

var _ domain.Repository[domain.UUID, domain.AggregateRoot[domain.UUID]] = (*Repository[domain.UUID, domain.AggregateRoot[domain.UUID]])(nil)

// NewRepository validates the mapping and creates a repository bound to db.
func NewRepository[ID domain.Identifier, T domain.AggregateRoot[ID]](db *DB, m Mapping[ID, T]) (*Repository[ID, T], error) {
	root, err := m.normalize()
	if err != nil {
		return nil, err
	}
	return &Repository[ID, T]{db: db, m: m, root: root, kind: strings.TrimPrefix(reflect.TypeFor[T]().String(), "*")}, nil
}

// MustRepository is like NewRepository but panics on an invalid mapping (composition roots).
func MustRepository[ID domain.Identifier, T domain.AggregateRoot[ID]](db *DB, m Mapping[ID, T]) *Repository[ID, T] {
	r, err := NewRepository(db, m)
	if err != nil {
		panic(err)
	}
	return r
}

func (r *Repository[ID, T]) q(ident string) string { return r.db.d.Quote(ident) }

func (r *Repository[ID, T]) rootColumns() []string {
	return append([]string{r.m.IDColumn, r.m.VersionColumn}, r.m.Columns...)
}

func (r *Repository[ID, T]) selectList(alias string, cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = alias + "." + r.q(c)
	}
	return strings.Join(parts, ", ")
}

func (r *Repository[ID, T]) where(b *Builder, s spec.Specification[T]) (string, error) {
	if s == nil {
		return "1=1", nil
	}
	return b.Translate(s.Expr())
}

func orderItems[T any](orders []spec.Order[T]) []orderItem {
	items := make([]orderItem, len(orders))
	for i, o := range orders {
		items[i] = orderItem{field: o.Field, desc: o.Desc}
	}
	return items
}

// Get loads an aggregate by identity.
func (r *Repository[ID, T]) Get(ctx context.Context, id ID) (T, error) {
	var zero T
	b := newBuilder(r.db.d, r.root, r.m.Custom)
	ph, err := b.Arg(id)
	if err != nil {
		return zero, err
	}
	query := fmt.Sprintf("SELECT %s FROM %s t0 WHERE t0.%s = %s",
		r.selectList("t0", r.rootColumns()), r.q(r.m.Table), r.q(r.m.IDColumn), ph)
	aggs, err := r.load(ctx, query, b.args)
	if err != nil {
		return zero, err
	}
	if len(aggs) == 0 {
		return zero, domain.NotFound(r.kind, id)
	}
	return aggs[0], nil
}

// Find returns the aggregates satisfying s, ordered by order then identity.
func (r *Repository[ID, T]) Find(ctx context.Context, s spec.Specification[T], order ...spec.Order[T]) ([]T, error) {
	query, args, err := r.selectQuery(s, order, -1, 0)
	if err != nil {
		return nil, err
	}
	return r.load(ctx, query, args)
}

// FindPage returns one page of matches plus the total count, both computed by the database.
func (r *Repository[ID, T]) FindPage(ctx context.Context, s spec.Specification[T], page domain.PageRequest[T]) (domain.Page[T], error) {
	page = page.Normalize()
	total, err := r.Count(ctx, s)
	if err != nil {
		return domain.Page[T]{}, err
	}
	items := []T{}
	if total > int64(page.Offset()) {
		query, args, err := r.selectQuery(s, page.Sort, page.Size, page.Offset())
		if err != nil {
			return domain.Page[T]{}, err
		}
		if items, err = r.load(ctx, query, args); err != nil {
			return domain.Page[T]{}, err
		}
	}
	return domain.NewPage(items, total, page.Number, page.Size), nil
}

// Explain returns the SQL and bind arguments FindPage would execute for s and page, without
// touching the database. Useful for diagnostics, logging and dialect tests.
func (r *Repository[ID, T]) Explain(s spec.Specification[T], page domain.PageRequest[T]) (string, []any, error) {
	page = page.Normalize()
	return r.selectQuery(s, page.Sort, page.Size, page.Offset())
}

func (r *Repository[ID, T]) selectQuery(s spec.Specification[T], order []spec.Order[T], limit, offset int) (string, []any, error) {
	b := newBuilder(r.db.d, r.root, r.m.Custom)
	where, err := r.where(b, s)
	if err != nil {
		return "", nil, err
	}
	orderBy, err := b.orderBy(orderItems(order))
	if err != nil {
		return "", nil, err
	}
	query := fmt.Sprintf("SELECT %s FROM %s t0 WHERE %s ORDER BY %s",
		r.selectList("t0", r.rootColumns()), r.q(r.m.Table), where, orderBy)
	if limit >= 0 {
		query += " " + r.db.d.LimitOffset(limit, offset)
	}
	return query, b.args, nil
}

// Count returns how many aggregates satisfy s.
func (r *Repository[ID, T]) Count(ctx context.Context, s spec.Specification[T]) (int64, error) {
	b := newBuilder(r.db.d, r.root, r.m.Custom)
	where, err := r.where(b, s)
	if err != nil {
		return 0, err
	}
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s t0 WHERE %s", r.q(r.m.Table), where)
	var raw any
	if err := r.db.executor(ctx).QueryRowContext(ctx, query, b.args...).Scan(&raw); err != nil {
		return 0, fmt.Errorf("sqlrepo: count %s: %w", r.kind, err)
	}
	return toInt64(raw)
}

// Exists reports whether at least one aggregate satisfies s.
func (r *Repository[ID, T]) Exists(ctx context.Context, s spec.Specification[T]) (bool, error) {
	b := newBuilder(r.db.d, r.root, r.m.Custom)
	where, err := r.where(b, s)
	if err != nil {
		return false, err
	}
	query := fmt.Sprintf("SELECT t0.%s FROM %s t0 WHERE %s ORDER BY t0.%s %s",
		r.q(r.m.IDColumn), r.q(r.m.Table), where, r.q(r.m.IDColumn), r.db.d.LimitOffset(1, 0))
	rows, err := r.db.executor(ctx).QueryContext(ctx, query, b.args...)
	if err != nil {
		return false, fmt.Errorf("sqlrepo: exists %s: %w", r.kind, err)
	}
	defer rows.Close()
	found := rows.Next()
	return found, rows.Err()
}

func scanAll(d Dialect, rows *sql.Rows, cols []string) ([]*Row, error) {
	defer rows.Close()
	var out []*Row
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		out = append(out, newRow(d, cols, vals))
	}
	return out, rows.Err()
}

func (r *Repository[ID, T]) load(ctx context.Context, query string, args []any) ([]T, error) {
	rows, err := r.db.executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: query %s: %w", r.kind, err)
	}
	scanned, err := scanAll(r.db.d, rows, r.rootColumns())
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: scan %s: %w", r.kind, err)
	}
	children, err := r.loadChildren(ctx, scanned)
	if err != nil {
		return nil, err
	}
	aggs := make([]T, 0, len(scanned))
	for _, row := range scanned {
		key, _, err := r.keyArg(row.Raw(r.m.IDColumn))
		if err != nil {
			return nil, err
		}
		kids := children[key]
		version := row.Int64(r.m.VersionColumn)
		agg, err := r.m.Hydrate(row, kids)
		if err == nil {
			err = row.Err()
		}
		for _, list := range kids {
			for _, kr := range list {
				if err == nil {
					err = kr.Err()
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("sqlrepo: hydrate %s: %w", r.kind, err)
		}
		domain.MarkPersisted(agg, version)
		agg.ClearEvents()
		aggs = append(aggs, agg)
	}
	return aggs, nil
}

// keyArg normalizes a scanned identity/foreign-key value into a grouping key and a bind argument
// that is valid for the dialect, according to the kind of the aggregate identifier.
func (r *Repository[ID, T]) keyArg(raw any) (string, any, error) {
	var zero ID
	switch any(zero).(type) {
	case domain.UUIDBacked:
		u, err := r.db.d.ParseUUID(raw)
		if err != nil {
			return "", nil, err
		}
		return u.String(), u, nil
	case domain.LongBacked:
		n, err := toInt64(raw)
		if err != nil {
			return "", nil, err
		}
		return strconv.FormatInt(n, 10), n, nil
	}
	if b, ok := raw.([]byte); ok {
		return string(b), string(b), nil
	}
	return fmt.Sprint(raw), raw, nil
}

// loadChildren loads the child rows of all scanned roots in batches (one query per child
// collection and IN-list chunk) and groups them by root key.
func (r *Repository[ID, T]) loadChildren(ctx context.Context, roots []*Row) (map[string]ChildRows, error) {
	out := make(map[string]ChildRows, len(roots))
	if len(roots) == 0 || len(r.m.Children) == 0 {
		return out, nil
	}
	keys := make([]any, 0, len(roots))
	for _, row := range roots {
		k, arg, err := r.keyArg(row.Raw(r.m.IDColumn))
		if err != nil {
			return nil, err
		}
		out[k] = ChildRows{}
		keys = append(keys, arg)
	}

	chunk := r.db.d.MaxInList()
	if chunk <= 0 {
		chunk = len(keys)
	}
	for _, c := range r.m.Children {
		ct := r.root.children[c.Name]
		cols := append([]string{c.ForeignKey}, c.Columns...)
		order := []string{"c." + r.q(c.ForeignKey)}
		for _, o := range ct.orderBy {
			order = append(order, "c."+r.q(o))
		}
		for start := 0; start < len(keys); start += chunk {
			end := min(start+chunk, len(keys))
			b := newBuilder(r.db.d, &ct.table, nil)
			phs := make([]string, 0, end-start)
			for _, k := range keys[start:end] {
				ph, err := b.Arg(k)
				if err != nil {
					return nil, err
				}
				phs = append(phs, ph)
			}
			query := fmt.Sprintf("SELECT %s FROM %s c WHERE c.%s IN (%s) ORDER BY %s",
				r.selectList("c", cols), r.q(c.Table), r.q(c.ForeignKey), strings.Join(phs, ", "), strings.Join(order, ", "))
			rows, err := r.db.executor(ctx).QueryContext(ctx, query, b.args...)
			if err != nil {
				return nil, fmt.Errorf("sqlrepo: query %s.%s: %w", r.kind, c.Name, err)
			}
			scanned, err := scanAll(r.db.d, rows, cols)
			if err != nil {
				return nil, fmt.Errorf("sqlrepo: scan %s.%s: %w", r.kind, c.Name, err)
			}
			for _, row := range scanned {
				k, _, err := r.keyArg(row.Raw(c.ForeignKey))
				if err != nil {
					return nil, err
				}
				if cr, ok := out[k]; ok {
					cr[c.Name] = append(cr[c.Name], row)
				}
			}
		}
	}
	return out, nil
}

func (r *Repository[ID, T]) rowArgs(b *Builder, cols []string, vals Values, what string) ([]string, error) {
	if len(vals) != len(cols) {
		for k := range vals {
			if !contains(cols, k) {
				return nil, fmt.Errorf("sqlrepo: %s: unknown column %q", what, k)
			}
		}
	}
	phs := make([]string, 0, len(cols))
	for _, c := range cols {
		v, ok := vals[c]
		if !ok {
			return nil, fmt.Errorf("sqlrepo: %s: missing value for column %q", what, c)
		}
		ph, err := b.Arg(v)
		if err != nil {
			return nil, fmt.Errorf("sqlrepo: %s column %q: %w", what, c, err)
		}
		phs = append(phs, ph)
	}
	return phs, nil
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Save inserts (version 0) or updates (optimistic concurrency) the aggregate and replaces its
// children, atomically.
func (r *Repository[ID, T]) Save(ctx context.Context, agg T) error {
	vals, err := r.m.Dehydrate(agg)
	if err != nil {
		return err
	}
	id, v := agg.ID(), agg.Version()

	return r.db.Do(ctx, func(ctx context.Context) error {
		ex := r.db.executor(ctx)
		b := newBuilder(r.db.d, r.root, nil)

		if v == 0 {
			idPh, _ := b.Arg(id)
			verPh, _ := b.Arg(int64(1))
			phs, err := r.rowArgs(b, r.m.Columns, vals, "insert "+r.kind)
			if err != nil {
				return err
			}
			cols := make([]string, 0, len(r.m.Columns)+2)
			for _, c := range r.rootColumns() {
				cols = append(cols, r.q(c))
			}
			query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
				r.q(r.m.Table), strings.Join(cols, ", "), strings.Join(append([]string{idPh, verPh}, phs...), ", "))
			if _, err := ex.ExecContext(ctx, query, b.args...); err != nil {
				if r.db.d.IsUniqueViolation(err) {
					return domain.Conflict(r.kind, id, v, "identity already exists")
				}
				return fmt.Errorf("sqlrepo: insert %s: %w", r.kind, err)
			}
		} else {
			phs, err := r.rowArgs(b, r.m.Columns, vals, "update "+r.kind)
			if err != nil {
				return err
			}
			sets := make([]string, 0, len(r.m.Columns)+1)
			for i, c := range r.m.Columns {
				sets = append(sets, r.q(c)+" = "+phs[i])
			}
			verPh, _ := b.Arg(v + 1)
			sets = append(sets, r.q(r.m.VersionColumn)+" = "+verPh)
			idPh, err := b.Arg(id)
			if err != nil {
				return err
			}
			oldPh, _ := b.Arg(v)
			query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = %s AND %s = %s",
				r.q(r.m.Table), strings.Join(sets, ", "), r.q(r.m.IDColumn), idPh, r.q(r.m.VersionColumn), oldPh)
			res, err := ex.ExecContext(ctx, query, b.args...)
			if err != nil {
				return fmt.Errorf("sqlrepo: update %s: %w", r.kind, err)
			}
			if n, err := res.RowsAffected(); err != nil {
				return err
			} else if n == 0 {
				return r.concurrencyError(ctx, id, v)
			}
		}

		if err := r.replaceChildren(ctx, ex, agg); err != nil {
			return err
		}
		domain.MarkPersisted(agg, v+1)
		r.db.onRollback(ctx, func() { domain.MarkPersisted(agg, v) })
		return nil
	})
}

func (r *Repository[ID, T]) concurrencyError(ctx context.Context, id ID, v int64) error {
	exists, err := r.Exists(ctx, spec.New[T](spec.Compare{Field: r.m.IDColumn, Op: spec.OpEq, Value: id}, nil))
	if err != nil {
		return err
	}
	if !exists {
		return domain.Conflict(r.kind, id, v, "aggregate no longer exists")
	}
	return domain.Conflict(r.kind, id, v, "stale version")
}

func (r *Repository[ID, T]) deleteChildren(ctx context.Context, ex executor, id ID) error {
	for _, c := range r.m.Children {
		b := newBuilder(r.db.d, r.root, nil)
		ph, err := b.Arg(id)
		if err != nil {
			return err
		}
		query := fmt.Sprintf("DELETE FROM %s WHERE %s = %s", r.q(c.Table), r.q(c.ForeignKey), ph)
		if _, err := ex.ExecContext(ctx, query, b.args...); err != nil {
			return fmt.Errorf("sqlrepo: delete %s.%s: %w", r.kind, c.Name, err)
		}
	}
	return nil
}

func (r *Repository[ID, T]) replaceChildren(ctx context.Context, ex executor, agg T) error {
	if len(r.m.Children) == 0 {
		return nil
	}
	if agg.Version() > 0 {
		if err := r.deleteChildren(ctx, ex, agg.ID()); err != nil {
			return err
		}
	}
	for _, c := range r.m.Children {
		rows, err := c.Dehydrate(agg)
		if err != nil {
			return err
		}
		quoted := make([]string, 0, len(c.Columns)+1)
		quoted = append(quoted, r.q(c.ForeignKey))
		for _, col := range c.Columns {
			quoted = append(quoted, r.q(col))
		}
		for _, vals := range rows {
			b := newBuilder(r.db.d, r.root, nil)
			fk, err := b.Arg(agg.ID())
			if err != nil {
				return err
			}
			phs, err := r.rowArgs(b, c.Columns, vals, "insert "+r.kind+"."+c.Name)
			if err != nil {
				return err
			}
			query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
				r.q(c.Table), strings.Join(quoted, ", "), strings.Join(append([]string{fk}, phs...), ", "))
			if _, err := ex.ExecContext(ctx, query, b.args...); err != nil {
				return fmt.Errorf("sqlrepo: insert %s.%s: %w", r.kind, c.Name, err)
			}
		}
	}
	return nil
}

// Delete removes the aggregate and its children with the optimistic concurrency check.
func (r *Repository[ID, T]) Delete(ctx context.Context, agg T) error {
	id, v := agg.ID(), agg.Version()
	return r.db.Do(ctx, func(ctx context.Context) error {
		ex := r.db.executor(ctx)
		if err := r.deleteChildren(ctx, ex, id); err != nil {
			return err
		}
		b := newBuilder(r.db.d, r.root, nil)
		idPh, err := b.Arg(id)
		if err != nil {
			return err
		}
		verPh, _ := b.Arg(v)
		query := fmt.Sprintf("DELETE FROM %s WHERE %s = %s AND %s = %s",
			r.q(r.m.Table), r.q(r.m.IDColumn), idPh, r.q(r.m.VersionColumn), verPh)
		res, err := ex.ExecContext(ctx, query, b.args...)
		if err != nil {
			return fmt.Errorf("sqlrepo: delete %s: %w", r.kind, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			exists, err := r.Exists(ctx, spec.New[T](spec.Compare{Field: r.m.IDColumn, Op: spec.OpEq, Value: id}, nil))
			if err != nil {
				return err
			}
			if !exists {
				return domain.NotFound(r.kind, id)
			}
			return domain.Conflict(r.kind, id, v, "stale version")
		}
		return nil
	})
}

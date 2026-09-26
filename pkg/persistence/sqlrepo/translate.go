package sqlrepo

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Builder translates specification expressions into SQL for one statement, collecting bind
// arguments. It is handed to CustomSQL translators.
type Builder struct {
	d      Dialect
	args   []any
	nAlias int
	scope  *scope
	custom map[string]CustomSQL
}

type scope struct {
	alias    string
	tbl      *table
	children map[string]*childTable
}

func newBuilder(d Dialect, root *table, custom map[string]CustomSQL) *Builder {
	b := &Builder{d: d, custom: custom}
	b.scope = &scope{alias: b.newAlias(), tbl: root, children: root.children}
	return b
}

func (b *Builder) newAlias() string {
	a := "t" + strconv.Itoa(b.nAlias)
	b.nAlias++
	return a
}

// Dialect returns the target dialect.
func (b *Builder) Dialect() Dialect { return b.d }

// Alias returns the alias of the table in the current scope.
func (b *Builder) Alias() string { return b.scope.alias }

// Args returns the bind arguments collected so far.
func (b *Builder) Args() []any { return b.args }

// Column returns the qualified, quoted column of a logical field in the current scope.
func (b *Builder) Column(field string) (string, error) {
	col, ok := b.scope.tbl.fields[field]
	if !ok {
		return "", fmt.Errorf("%w: field %q is not mapped on table %q", domain.ErrUnsupported, field, b.scope.tbl.name)
	}
	return b.scope.alias + "." + b.d.Quote(col), nil
}

// Arg binds v (converted for the dialect) and returns its placeholder.
func (b *Builder) Arg(v any) (string, error) {
	dv, err := toDriver(b.d, v)
	if err != nil {
		return "", err
	}
	b.args = append(b.args, dv)
	return b.d.Placeholder(len(b.args)), nil
}

// Translate converts an expression to a SQL boolean condition in the current scope.
func (b *Builder) Translate(e spec.Expr) (string, error) {
	switch n := e.(type) {
	case nil:
		return "1=1", nil
	case spec.Const:
		if n.Value {
			return "1=1", nil
		}
		return "1=0", nil
	case spec.AndExpr:
		return b.join(n.Exprs, " AND ", "1=1")
	case spec.OrExpr:
		return b.join(n.Exprs, " OR ", "1=0")
	case spec.NotExpr:
		inner, err := b.Translate(n.Expr)
		if err != nil {
			return "", err
		}
		return "NOT (" + inner + ")", nil
	case spec.Compare:
		return b.compare(n)
	case spec.AnyExpr:
		return b.exists(n)
	case spec.CustomExpr:
		fn, ok := b.custom[n.Name]
		if !ok {
			return "", fmt.Errorf("%w: custom specification %q has no SQL translation (dialect %s)", domain.ErrUnsupported, n.Name, b.d.Name())
		}
		sql, err := fn(b, n.Args)
		if err != nil {
			return "", err
		}
		return "(" + sql + ")", nil
	default:
		return "", fmt.Errorf("%w: expression node %T", domain.ErrUnsupported, e)
	}
}

func (b *Builder) join(exprs []spec.Expr, sep, empty string) (string, error) {
	if len(exprs) == 0 {
		return empty, nil
	}
	parts := make([]string, 0, len(exprs))
	for _, e := range exprs {
		s, err := b.Translate(e)
		if err != nil {
			return "", err
		}
		parts = append(parts, "("+s+")")
	}
	return strings.Join(parts, sep), nil
}

func (b *Builder) compare(n spec.Compare) (string, error) {
	col, err := b.Column(n.Field)
	if err != nil {
		return "", err
	}
	binary := func(op string) (string, error) {
		ph, err := b.Arg(n.Value)
		if err != nil {
			return "", err
		}
		return col + " " + op + " " + ph, nil
	}
	like := func(pattern string, fold bool) (string, error) {
		ph, err := b.Arg(pattern)
		if err != nil {
			return "", err
		}
		if fold {
			return "LOWER(" + col + ") LIKE LOWER(" + ph + ") " + b.d.LikeEscape(), nil
		}
		return col + " LIKE " + ph + " " + b.d.LikeEscape(), nil
	}
	text := func() (string, error) {
		s, ok := n.Value.(string)
		if !ok {
			return "", fmt.Errorf("%w: operator %s needs a string, got %T", domain.ErrUnsupported, n.Op, n.Value)
		}
		return EscapeLike(s), nil
	}

	switch n.Op {
	case spec.OpEq:
		if n.Value == nil {
			return col + " IS NULL", nil
		}
		return binary("=")
	case spec.OpNe:
		if n.Value == nil {
			return col + " IS NOT NULL", nil
		}
		return binary("<>")
	case spec.OpGt:
		return binary(">")
	case spec.OpGe:
		return binary(">=")
	case spec.OpLt:
		return binary("<")
	case spec.OpLe:
		return binary("<=")
	case spec.OpIsNull:
		return col + " IS NULL", nil
	case spec.OpIsNotNull:
		return col + " IS NOT NULL", nil
	case spec.OpIn, spec.OpNotIn:
		in, err := b.in(col, n.Value)
		if err != nil {
			return "", err
		}
		if n.Op == spec.OpNotIn {
			return "NOT (" + in + ")", nil
		}
		return in, nil
	case spec.OpContains:
		s, err := text()
		if err != nil {
			return "", err
		}
		return like("%"+s+"%", false)
	case spec.OpStartsWith:
		s, err := text()
		if err != nil {
			return "", err
		}
		return like(s+"%", false)
	case spec.OpEndsWith:
		s, err := text()
		if err != nil {
			return "", err
		}
		return like("%"+s, false)
	case spec.OpContainsFold:
		s, err := text()
		if err != nil {
			return "", err
		}
		return like("%"+s+"%", true)
	case spec.OpEqualFold:
		ph, err := b.Arg(n.Value)
		if err != nil {
			return "", err
		}
		return "LOWER(" + col + ") = LOWER(" + ph + ")", nil
	}
	return "", fmt.Errorf("%w: operator %s", domain.ErrUnsupported, n.Op)
}

func (b *Builder) in(col string, v any) (string, error) {
	vals, ok := v.([]any)
	if !ok {
		return "", fmt.Errorf("%w: IN needs a []any, got %T", domain.ErrUnsupported, v)
	}
	if len(vals) == 0 {
		return "1=0", nil
	}
	chunk := b.d.MaxInList()
	if chunk <= 0 {
		chunk = len(vals)
	}
	var groups []string
	for start := 0; start < len(vals); start += chunk {
		end := min(start+chunk, len(vals))
		phs := make([]string, 0, end-start)
		for _, x := range vals[start:end] {
			ph, err := b.Arg(x)
			if err != nil {
				return "", err
			}
			phs = append(phs, ph)
		}
		groups = append(groups, col+" IN ("+strings.Join(phs, ", ")+")")
	}
	if len(groups) == 1 {
		return groups[0], nil
	}
	return "(" + strings.Join(groups, " OR ") + ")", nil
}

func (b *Builder) exists(n spec.AnyExpr) (string, error) {
	child, ok := b.scope.children[n.Collection]
	if !ok {
		return "", fmt.Errorf("%w: collection %q is not mapped on table %q", domain.ErrUnsupported, n.Collection, b.scope.tbl.name)
	}
	parent := b.scope
	b.scope = &scope{alias: b.newAlias(), tbl: &child.table}
	where, err := b.Translate(n.Where)
	alias := b.scope.alias
	b.scope = parent
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("EXISTS (SELECT 1 FROM %s %s WHERE %s.%s = %s.%s AND (%s))",
		b.d.Quote(child.name), alias,
		alias, b.d.Quote(child.key),
		parent.alias, b.d.Quote(parent.tbl.key),
		where), nil
}

type orderItem struct {
	field string
	desc  bool
}

func (b *Builder) orderBy(items []orderItem) (string, error) {
	parts := make([]string, 0, len(items)+1)
	for _, it := range items {
		col, err := b.Column(it.field)
		if err != nil {
			return "", err
		}
		dir := " ASC"
		if it.desc {
			dir = " DESC"
		}
		parts = append(parts, col+dir)
	}
	parts = append(parts, b.scope.alias+"."+b.d.Quote(b.scope.tbl.key)+" ASC")
	return strings.Join(parts, ", "), nil
}

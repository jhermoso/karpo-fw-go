package spec

import (
	"fmt"
	"strings"
)

// Expr is a node of the specification expression tree: the Go counterpart of the C#
// Expression<Func<T,bool>> used by Specification<T>. It is data, not code, so every persistence
// adapter can translate it to its own query language (SQL dialects, document filters, ...).
//
// The set of nodes is closed: AndExpr, OrExpr, NotExpr, Const, Compare, AnyExpr and CustomExpr. Business-specific
// rules that cannot be expressed with the standard nodes are CustomExpr nodes, which each adapter
// implements explicitly (see Custom).
type Expr interface {
	isExpr()
}

// Op is a comparison operator of a Compare node.
type Op uint8

// Comparison operators. Text operators (Contains, StartsWith, EndsWith) follow the collation of
// the backing store; the *Fold operators are case-insensitive everywhere.
const (
	OpEq Op = iota + 1
	OpNe
	OpGt
	OpGe
	OpLt
	OpLe
	OpIn
	OpNotIn
	OpIsNull
	OpIsNotNull
	OpContains
	OpStartsWith
	OpEndsWith
	OpEqualFold
	OpContainsFold
)

var opNames = map[Op]string{
	OpEq: "=", OpNe: "<>", OpGt: ">", OpGe: ">=", OpLt: "<", OpLe: "<=",
	OpIn: "IN", OpNotIn: "NOT IN", OpIsNull: "IS NULL", OpIsNotNull: "IS NOT NULL",
	OpContains: "CONTAINS", OpStartsWith: "STARTS WITH", OpEndsWith: "ENDS WITH",
	OpEqualFold: "EQUALS IGNORE CASE", OpContainsFold: "CONTAINS IGNORE CASE",
}

func (o Op) String() string {
	if s, ok := opNames[o]; ok {
		return s
	}
	return fmt.Sprintf("Op(%d)", uint8(o))
}

// Compare compares a logical field with a value. Field is the logical (domain) name declared by
// the typed field; adapters map it to a physical column. For OpIn/OpNotIn Value is a []any.
// For OpIsNull/OpIsNotNull Value is nil.
type Compare struct {
	Field string
	Op    Op
	Value any
}

// AndExpr is satisfied when every child expression is satisfied (true when empty).
type AndExpr struct{ Exprs []Expr }

// OrExpr is satisfied when at least one child expression is satisfied (false when empty).
type OrExpr struct{ Exprs []Expr }

// NotExpr negates its child expression.
type NotExpr struct{ Expr Expr }

// Const is a constant truth value.
type Const struct{ Value bool }

// AnyExpr is satisfied when at least one element of the named child collection satisfies Where.
// SQL adapters translate it to EXISTS (SELECT 1 FROM child WHERE fk = parent.id AND ...).
type AnyExpr struct {
	Collection string
	Where      Expr
}

// CustomExpr is a named business rule whose translation is supplied per adapter
// (the "implement the specification contract for each database" extension point).
// Adapters that do not know Name must fail with domain.ErrUnsupported.
type CustomExpr struct {
	Name string
	Args []any
}

func (Compare) isExpr()    {}
func (AndExpr) isExpr()    {}
func (OrExpr) isExpr()     {}
func (NotExpr) isExpr()    {}
func (Const) isExpr()      {}
func (AnyExpr) isExpr()    {}
func (CustomExpr) isExpr() {}

// Walk traverses e depth-first, calling fn for each node. If fn returns false the children of
// that node are skipped.
func Walk(e Expr, fn func(Expr) bool) {
	if e == nil || !fn(e) {
		return
	}
	switch n := e.(type) {
	case AndExpr:
		for _, c := range n.Exprs {
			Walk(c, fn)
		}
	case OrExpr:
		for _, c := range n.Exprs {
			Walk(c, fn)
		}
	case NotExpr:
		Walk(n.Expr, fn)
	case AnyExpr:
		Walk(n.Where, fn)
	}
}

// Format renders e as a human-readable string, for logs and error messages.
func Format(e Expr) string {
	var b strings.Builder
	format(&b, e)
	return b.String()
}

func format(b *strings.Builder, e Expr) {
	switch n := e.(type) {
	case nil:
		b.WriteString("TRUE")
	case Const:
		if n.Value {
			b.WriteString("TRUE")
		} else {
			b.WriteString("FALSE")
		}
	case Compare:
		b.WriteString(n.Field)
		b.WriteByte(' ')
		b.WriteString(n.Op.String())
		if n.Op != OpIsNull && n.Op != OpIsNotNull {
			fmt.Fprintf(b, " %#v", n.Value)
		}
	case AndExpr:
		formatList(b, "AND", n.Exprs, "TRUE")
	case OrExpr:
		formatList(b, "OR", n.Exprs, "FALSE")
	case NotExpr:
		b.WriteString("NOT (")
		format(b, n.Expr)
		b.WriteByte(')')
	case AnyExpr:
		fmt.Fprintf(b, "ANY %s (", n.Collection)
		format(b, n.Where)
		b.WriteByte(')')
	case CustomExpr:
		fmt.Fprintf(b, "%s%v", n.Name, n.Args)
	default:
		fmt.Fprintf(b, "%T", e)
	}
}

func formatList(b *strings.Builder, op string, exprs []Expr, empty string) {
	if len(exprs) == 0 {
		b.WriteString(empty)
		return
	}
	b.WriteByte('(')
	for i, c := range exprs {
		if i > 0 {
			b.WriteString(" " + op + " ")
		}
		format(b, c)
	}
	b.WriteByte(')')
}

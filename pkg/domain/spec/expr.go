package spec

import (
	"fmt"
	"strings"
)

// Expr es un nodo del árbol de expresiones de una especificación: la contrapartida en Go de la
// Expression<Func<T,bool>> de C# que usa Specification<T>. Es dato, no código, así que cada adaptador
// de persistencia puede traducirlo a su propio lenguaje de consulta (dialectos SQL, filtros de documentos, ...).
//
// El conjunto de nodos es cerrado: AndExpr, OrExpr, NotExpr, Const, Compare, AnyExpr y CustomExpr. Las reglas
// propias del negocio que no se pueden expresar con los nodos estándar son nodos CustomExpr, que cada
// adaptador implementa explícitamente (véase Custom).
type Expr interface {
	isExpr()
}

// Op es un operador de comparación de un nodo Compare.
type Op uint8

// Operadores de comparación. Los operadores de texto (Contains, StartsWith, EndsWith) siguen la
// intercalación del almacén subyacente; los operadores *Fold no distinguen mayúsculas en ninguno.
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

// Compare compara un campo lógico con un valor. Field es el nombre lógico (de dominio) que declara
// el campo tipado; los adaptadores lo traducen a una columna física. Para OpIn/OpNotIn Value es un []any.
// Para OpIsNull/OpIsNotNull Value es nil.
type Compare struct {
	Field string
	Op    Op
	Value any
}

// AndExpr se cumple cuando se cumplen todas las expresiones hijas (true si está vacía).
type AndExpr struct{ Exprs []Expr }

// OrExpr se cumple cuando se cumple al menos una expresión hija (false si está vacía).
type OrExpr struct{ Exprs []Expr }

// NotExpr niega su expresión hija.
type NotExpr struct{ Expr Expr }

// Const es un valor de verdad constante.
type Const struct{ Value bool }

// AnyExpr se cumple cuando al menos un elemento de la colección hija nombrada cumple Where.
// Los adaptadores SQL lo traducen a EXISTS (SELECT 1 FROM child WHERE fk = parent.id AND ...).
type AnyExpr struct {
	Collection string
	Where      Expr
}

// CustomExpr es una regla de negocio con nombre cuya traducción aporta cada adaptador
// (el punto de extensión «implementar el contrato de especificación para cada base de datos»).
// Los adaptadores que no conocen Name deben fallar con domain.ErrUnsupported.
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

// Walk recorre e en profundidad y llama a fn en cada nodo. Si fn devuelve false, se saltan los
// hijos de ese nodo.
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

// Format presenta e como una cadena legible, para logs y mensajes de error.
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

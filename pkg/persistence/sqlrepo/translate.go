package sqlrepo

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/shopspring/decimal"
)

// Builder traduce a SQL las expresiones de especificación de una sentencia, y va reuniendo los
// argumentos enlazados. Se entrega a los traductores CustomSQL.
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

// Dialect devuelve el dialecto de destino.
func (b *Builder) Dialect() Dialect { return b.d }

// Alias devuelve el alias de la tabla del ámbito en curso.
func (b *Builder) Alias() string { return b.scope.alias }

// Args devuelve los argumentos enlazados reunidos hasta ahora.
func (b *Builder) Args() []any { return b.args }

// Column devuelve la columna, cualificada y entrecomillada, de un campo lógico en el ámbito en
// curso.
func (b *Builder) Column(field string) (string, error) {
	col, _, err := b.column(field)
	return col, err
}

// column devuelve la columna cualificada y entrecomillada de un campo lógico y su nombre.
func (b *Builder) column(field string) (qualified, name string, err error) {
	col, ok := b.scope.tbl.fields[field]
	if !ok {
		return "", "", fmt.Errorf("%w: field %q is not mapped on table %q", domain.ErrUnsupported, field, b.scope.tbl.name)
	}
	return b.scope.alias + "." + b.d.Quote(col), col, nil
}

// Arg enlaza v (convertido para el dialecto) y devuelve la expresión de su marcador. Un
// vocab.Decimal se enlaza como texto y se convierte a número con Dialect.DecimalExpr: en Arg (y por
// tanto en CustomSQL y en DB.Insert, Update, Delete e InsertMany, que no conocen el tipo de las
// columnas) un decimal es para una columna {dec:p,s}; a una columna de texto se le pasa su texto.
// Sin conocer la escala de la columna, Arg no comprueba que el valor quepa ni reescribe la
// comparación, y en SQLite compara texto: eso sólo lo hace el repositorio con Mapping.Decimals.
func (b *Builder) Arg(v any) (string, error) {
	if p, ok := v.(*decimal.Decimal); ok && p != nil {
		v = *p
	}
	var precision, scale int
	dec, isDecimal := v.(decimal.Decimal)
	if isDecimal {
		var err error
		if precision, scale, err = decimalShape(dec); err != nil {
			return "", err
		}
	}
	dv, err := toDriver(b.d, v)
	if err != nil {
		return "", err
	}
	b.args = append(b.args, dv)
	ph := b.d.Placeholder(len(b.args))
	if _, isDate := v.(domain.DateBacked); isDate {
		ph = b.d.DateExpr(ph)
	}
	if isDecimal {
		ph = b.d.DecimalExpr(ph, precision, scale)
	}
	return ph, nil
}

// decimalKeyed devuelve el dialecto como TextDecimals si guarda los decimales como texto.
func decimalKeyed(d Dialect) (TextDecimals, bool) {
	k, ok := d.(TextDecimals)
	return k, ok
}

// orderColumn devuelve la expresión con la que se ordena una columna: la propia columna o, si es
// decimal y el motor la guarda como texto, su clave de orden numérico.
func orderColumn(d Dialect, tbl *table, qualified, name string) string {
	if k, ok := decimalKeyed(d); ok {
		if _, dec := tbl.decimals[name]; dec {
			return k.DecimalKeyExpr(qualified)
		}
	}
	return qualified
}

// hasDecimal dice si v es un decimal o una lista (IN) con alguno.
func hasDecimal(v any) bool {
	switch x := v.(type) {
	case decimal.Decimal, *decimal.Decimal:
		return true
	case []any:
		for _, e := range x {
			if hasDecimal(e) {
				return true
			}
		}
	}
	return false
}

// Translate convierte una expresión en una condición booleana SQL en el ámbito en curso.
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
	col, name, err := b.column(n.Field)
	if err != nil {
		return "", err
	}
	if dc, ok := b.scope.tbl.decimals[name]; ok {
		return b.compareDecimal(n, col, dc)
	}
	if hasDecimal(n.Value) {
		return "", fmt.Errorf("%w: field %q is compared with a decimal but column %q of %q is not in Mapping.Decimals",
			domain.ErrUnsupported, n.Field, name, b.scope.tbl.name)
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
		in, err := b.in(col, n.Value, b.Arg)
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

// compareDecimal traduce una comparación sobre una columna decimal dc: por valor numérico, con el
// valor convertido por Dialect.DecimalExpr o, en los motores que guardan el decimal como texto
// (TextDecimals), comparando las claves de orden numérico de la columna y del valor.
//
// Un valor con más decimales que la columna se reescribe sobre la escala de la columna, de forma
// exacta: como todo valor guardado v es múltiplo de 10^-s, con f = x redondeado hacia abajo a s
// decimales (y x != f), v > x y v >= x son v > f, v < x y v <= x son v <= f, v = x es falso y
// v <> x es cierto salvo en NULL. Así ningún motor redondea ni desborda el valor comparado.
// Un *vocab.Decimal nil en Eq o Ne es IS NULL o IS NOT NULL; en los demás operadores y en las
// listas IN es un error.
func (b *Builder) compareDecimal(n spec.Compare, col string, dc DecimalColumn) (string, error) {
	keyed, isText := decimalKeyed(b.d)
	lhs := col
	if isText {
		lhs = keyed.DecimalKeyExpr(col)
	}
	value := func(v any) (d decimal.Decimal, isNil bool, err error) {
		switch x := v.(type) {
		case nil:
			return d, true, nil
		case *decimal.Decimal:
			if x == nil {
				return d, true, nil
			}
			return *x, false, nil
		case decimal.Decimal:
			return x, false, nil
		}
		return d, false, fmt.Errorf("%w: decimal field %q compared with %T; use vocab.Decimal", domain.ErrUnsupported, n.Field, v)
	}
	bind := func(d decimal.Decimal) (string, error) {
		// Los mismos límites en todos los motores, para que una consulta valga en todos.
		if _, _, err := decimalShape(d); err != nil {
			return "", err
		}
		if isText {
			return b.Arg(keyed.DecimalKey(d))
		}
		return b.Arg(d)
	}
	// floor devuelve x sobre la escala de la columna y si x ya era representable.
	floor := func(x decimal.Decimal) (decimal.Decimal, bool) {
		f := x.RoundFloor(int32(dc.Scale))
		return f, f.Equal(x)
	}

	switch n.Op {
	case spec.OpIsNull:
		return col + " IS NULL", nil
	case spec.OpIsNotNull:
		return col + " IS NOT NULL", nil
	case spec.OpEq, spec.OpNe, spec.OpGt, spec.OpGe, spec.OpLt, spec.OpLe:
		x, isNil, err := value(n.Value)
		if err != nil {
			return "", err
		}
		if isNil {
			switch n.Op {
			case spec.OpEq:
				return col + " IS NULL", nil
			case spec.OpNe:
				return col + " IS NOT NULL", nil
			}
			return "", fmt.Errorf("%w: decimal field %q compared (%s) with NULL", domain.ErrUnsupported, n.Field, n.Op)
		}
		f, exact := floor(x)
		op := map[spec.Op]string{spec.OpEq: "=", spec.OpNe: "<>", spec.OpGt: ">", spec.OpGe: ">=", spec.OpLt: "<", spec.OpLe: "<="}[n.Op]
		if !exact {
			switch n.Op {
			case spec.OpEq:
				return "1=0", nil
			case spec.OpNe:
				return col + " IS NOT NULL", nil
			case spec.OpGt, spec.OpGe:
				op = ">"
			default:
				op = "<="
			}
		}
		ph, err := bind(f)
		if err != nil {
			return "", err
		}
		return lhs + " " + op + " " + ph, nil
	case spec.OpIn, spec.OpNotIn:
		vals, ok := n.Value.([]any)
		if !ok {
			return "", fmt.Errorf("%w: IN needs a []any, got %T", domain.ErrUnsupported, n.Value)
		}
		// Un valor que la columna no puede guardar nunca coincide: se quita de la lista.
		kept := make([]any, 0, len(vals))
		for _, v := range vals {
			x, isNil, err := value(v)
			if err != nil {
				return "", err
			}
			if isNil {
				return "", fmt.Errorf("%w: NULL in an IN list of decimal field %q; use IsNull", domain.ErrUnsupported, n.Field)
			}
			if f, exact := floor(x); exact {
				kept = append(kept, f)
			}
		}
		in, err := b.in(lhs, kept, func(v any) (string, error) { return bind(v.(decimal.Decimal)) })
		if err != nil {
			return "", err
		}
		if n.Op == spec.OpNotIn {
			return "NOT (" + in + ")", nil
		}
		return in, nil
	}
	return "", fmt.Errorf("%w: operator %s on decimal field %q", domain.ErrUnsupported, n.Op, n.Field)
}

func (b *Builder) in(col string, v any, arg func(any) (string, error)) (string, error) {
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
			ph, err := arg(x)
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
		col, name, err := b.column(it.field)
		if err != nil {
			return "", err
		}
		dir := " ASC"
		if it.desc {
			dir = " DESC"
		}
		parts = append(parts, orderColumn(b.d, b.scope.tbl, col, name)+dir)
	}
	parts = append(parts, b.scope.alias+"."+b.d.Quote(b.scope.tbl.key)+" ASC")
	return strings.Join(parts, ", "), nil
}

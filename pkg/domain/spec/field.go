package spec

import (
	"cmp"
	"strings"
	"time"
)

// Field es un atributo tipado y comparable de T con un nombre lógico. El accesor da la semántica
// en memoria; el nombre es lo que los adaptadores traducen al almacenamiento. Los valores deben ser
// primitivos, identificadores tipados (domain.UUIDBacked / domain.LongBacked) o tipos primitivos con nombre.
type Field[T any, V comparable] struct {
	name string
	get  func(T) V
}

// Comparable declara un campo que admite operadores de igualdad.
func Comparable[T any, V comparable](name string, get func(T) V) Field[T, V] {
	return Field[T, V]{name: name, get: get}
}

// Name devuelve el nombre lógico del campo.
func (f Field[T, V]) Name() string { return f.name }

// Value devuelve el valor del campo en candidate.
func (f Field[T, V]) Value(candidate T) V { return f.get(candidate) }

// Eq selecciona los candidatos cuyo campo es igual a v.
func (f Field[T, V]) Eq(v V) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpEq, Value: v}, eval: func(c T) bool { return f.get(c) == v }}
}

// Ne selecciona los candidatos cuyo campo es distinto de v.
func (f Field[T, V]) Ne(v V) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpNe, Value: v}, eval: func(c T) bool { return f.get(c) != v }}
}

// In selecciona los candidatos cuyo campo es igual a alguno de vs (sin valores: no selecciona nada).
func (f Field[T, V]) In(vs ...V) Spec[T] {
	set := make(map[V]struct{}, len(vs))
	for _, v := range vs {
		set[v] = struct{}{}
	}
	return Spec[T]{
		expr: Compare{Field: f.name, Op: OpIn, Value: toAny(vs)},
		eval: func(c T) bool { _, ok := set[f.get(c)]; return ok },
	}
}

// NotIn selecciona los candidatos cuyo campo no es igual a ninguno de vs (sin valores: lo selecciona todo).
func (f Field[T, V]) NotIn(vs ...V) Spec[T] {
	in := f.In(vs...)
	return Spec[T]{
		expr: Compare{Field: f.name, Op: OpNotIn, Value: toAny(vs)},
		eval: func(c T) bool { return !in.IsSatisfiedBy(c) },
	}
}

// OrderedField es un campo cuyos valores tienen un orden natural (números, cadenas).
type OrderedField[T any, V cmp.Ordered] struct {
	Field[T, V]
}

// Ordered declara un campo que admite igualdad, operadores de rango y ordenación.
func Ordered[T any, V cmp.Ordered](name string, get func(T) V) OrderedField[T, V] {
	return OrderedField[T, V]{Field: Comparable(name, get)}
}

func (f OrderedField[T, V]) cmpSpec(op Op, v V, ok func(int) bool) Spec[T] {
	return Spec[T]{
		expr: Compare{Field: f.name, Op: op, Value: v},
		eval: func(c T) bool { return ok(cmp.Compare(f.get(c), v)) },
	}
}

// Gt selecciona campo > v.
func (f OrderedField[T, V]) Gt(v V) Spec[T] {
	return f.cmpSpec(OpGt, v, func(r int) bool { return r > 0 })
}

// Ge selecciona campo >= v.
func (f OrderedField[T, V]) Ge(v V) Spec[T] {
	return f.cmpSpec(OpGe, v, func(r int) bool { return r >= 0 })
}

// Lt selecciona campo < v.
func (f OrderedField[T, V]) Lt(v V) Spec[T] {
	return f.cmpSpec(OpLt, v, func(r int) bool { return r < 0 })
}

// Le selecciona campo <= v.
func (f OrderedField[T, V]) Le(v V) Spec[T] {
	return f.cmpSpec(OpLe, v, func(r int) bool { return r <= 0 })
}

// Between selecciona lo <= campo <= hi.
func (f OrderedField[T, V]) Between(lo, hi V) Spec[T] { return f.Ge(lo).And(f.Le(hi)) }

// Asc ordena ascendentemente por este campo.
func (f OrderedField[T, V]) Asc() Order[T] {
	return Order[T]{Field: f.name, compare: func(a, b T) int { return cmp.Compare(f.get(a), f.get(b)) }}
}

// Desc ordena descendentemente por este campo.
func (f OrderedField[T, V]) Desc() Order[T] { o := f.Asc(); o.Desc = true; return o }

// CompareField es un campo ordenado cuyos valores definen su propio orden total mediante una función
// de comparación en lugar de los operadores de Go (decimales, fechas civiles, importes...). La igualdad es
// compare(a, b) == 0, no ==.
type CompareField[T any, V any] struct {
	name    string
	get     func(T) V
	compare func(a, b V) int
}

// OrderedBy declara un campo ordenado por compare (por ejemplo Decimal.Cmp o Date.Compare).
func OrderedBy[T any, V any](name string, get func(T) V, compare func(a, b V) int) CompareField[T, V] {
	return CompareField[T, V]{name: name, get: get, compare: compare}
}

// Name devuelve el nombre lógico del campo.
func (f CompareField[T, V]) Name() string { return f.name }

func (f CompareField[T, V]) spec(op Op, v V, ok func(int) bool) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: op, Value: v}, eval: func(c T) bool { return ok(f.compare(f.get(c), v)) }}
}

// Eq selecciona los valores iguales a v según compare.
func (f CompareField[T, V]) Eq(v V) Spec[T] {
	return f.spec(OpEq, v, func(r int) bool { return r == 0 })
}

// Ne selecciona los valores distintos de v según compare.
func (f CompareField[T, V]) Ne(v V) Spec[T] {
	return f.spec(OpNe, v, func(r int) bool { return r != 0 })
}

// Gt selecciona campo > v.
func (f CompareField[T, V]) Gt(v V) Spec[T] {
	return f.spec(OpGt, v, func(r int) bool { return r > 0 })
}

// Ge selecciona campo >= v.
func (f CompareField[T, V]) Ge(v V) Spec[T] {
	return f.spec(OpGe, v, func(r int) bool { return r >= 0 })
}

// Lt selecciona campo < v.
func (f CompareField[T, V]) Lt(v V) Spec[T] {
	return f.spec(OpLt, v, func(r int) bool { return r < 0 })
}

// Le selecciona campo <= v.
func (f CompareField[T, V]) Le(v V) Spec[T] {
	return f.spec(OpLe, v, func(r int) bool { return r <= 0 })
}

// Between selecciona lo <= campo <= hi.
func (f CompareField[T, V]) Between(lo, hi V) Spec[T] { return f.Ge(lo).And(f.Le(hi)) }

// Asc ordena ascendentemente por este campo.
func (f CompareField[T, V]) Asc() Order[T] {
	return Order[T]{Field: f.name, compare: func(a, b T) int { return f.compare(f.get(a), f.get(b)) }}
}

// Desc ordena descendentemente por este campo.
func (f CompareField[T, V]) Desc() Order[T] { o := f.Asc(); o.Desc = true; return o }

// TextField es un campo de cadena ordenado con operadores de patrón.
type TextField[T any] struct {
	OrderedField[T, string]
}

// Text declara un campo de cadena.
func Text[T any](name string, get func(T) string) TextField[T] {
	return TextField[T]{OrderedField: Ordered(name, get)}
}

func (f TextField[T]) textSpec(op Op, v string, ok func(string) bool) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: op, Value: v}, eval: func(c T) bool { return ok(f.get(c)) }}
}

// Contains selecciona los valores que contienen sub (la distinción de mayúsculas sigue la intercalación del almacén).
func (f TextField[T]) Contains(sub string) Spec[T] {
	return f.textSpec(OpContains, sub, func(s string) bool { return strings.Contains(s, sub) })
}

// StartsWith selecciona los valores que empiezan por prefix (la distinción de mayúsculas sigue la intercalación del almacén).
func (f TextField[T]) StartsWith(prefix string) Spec[T] {
	return f.textSpec(OpStartsWith, prefix, func(s string) bool { return strings.HasPrefix(s, prefix) })
}

// EndsWith selecciona los valores que terminan en suffix (la distinción de mayúsculas sigue la intercalación del almacén).
func (f TextField[T]) EndsWith(suffix string) Spec[T] {
	return f.textSpec(OpEndsWith, suffix, func(s string) bool { return strings.HasSuffix(s, suffix) })
}

// EqualFold selecciona los valores iguales a v sin distinguir mayúsculas, en todos los almacenes.
func (f TextField[T]) EqualFold(v string) Spec[T] {
	return f.textSpec(OpEqualFold, v, func(s string) bool { return strings.EqualFold(s, v) })
}

// ContainsFold selecciona los valores que contienen sub sin distinguir mayúsculas, en todos los almacenes.
func (f TextField[T]) ContainsFold(sub string) Spec[T] {
	lower := strings.ToLower(sub)
	return f.textSpec(OpContainsFold, sub, func(s string) bool { return strings.Contains(strings.ToLower(s), lower) })
}

// TimeField es un campo cuyo valor es un instante. Las comparaciones usan time.Time.Compare (independiente de la zona).
type TimeField[T any] struct {
	name string
	get  func(T) time.Time
}

// Time declara un campo time.Time.
func Time[T any](name string, get func(T) time.Time) TimeField[T] {
	return TimeField[T]{name: name, get: get}
}

// Name devuelve el nombre lógico del campo.
func (f TimeField[T]) Name() string { return f.name }

func (f TimeField[T]) timeSpec(op Op, t time.Time, ok func(int) bool) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: op, Value: t}, eval: func(c T) bool { return ok(f.get(c).Compare(t)) }}
}

// Eq selecciona el mismo instante.
func (f TimeField[T]) Eq(t time.Time) Spec[T] {
	return f.timeSpec(OpEq, t, func(r int) bool { return r == 0 })
}

// After selecciona los instantes estrictamente posteriores a t.
func (f TimeField[T]) After(t time.Time) Spec[T] {
	return f.timeSpec(OpGt, t, func(r int) bool { return r > 0 })
}

// AtOrAfter selecciona los instantes iguales o posteriores a t.
func (f TimeField[T]) AtOrAfter(t time.Time) Spec[T] {
	return f.timeSpec(OpGe, t, func(r int) bool { return r >= 0 })
}

// Before selecciona los instantes estrictamente anteriores a t.
func (f TimeField[T]) Before(t time.Time) Spec[T] {
	return f.timeSpec(OpLt, t, func(r int) bool { return r < 0 })
}

// AtOrBefore selecciona los instantes iguales o anteriores a t.
func (f TimeField[T]) AtOrBefore(t time.Time) Spec[T] {
	return f.timeSpec(OpLe, t, func(r int) bool { return r <= 0 })
}

// Between selecciona from <= instante <= to.
func (f TimeField[T]) Between(from, to time.Time) Spec[T] {
	return f.AtOrAfter(from).And(f.AtOrBefore(to))
}

// Asc ordena ascendentemente por este campo.
func (f TimeField[T]) Asc() Order[T] {
	return Order[T]{Field: f.name, compare: func(a, b T) int { return f.get(a).Compare(f.get(b)) }}
}

// Desc ordena descendentemente por este campo.
func (f TimeField[T]) Desc() Order[T] { o := f.Asc(); o.Desc = true; return o }

// OptionalField es un campo que admite nulo; el accesor devuelve nil cuando no hay valor.
type OptionalField[T any, V comparable] struct {
	name string
	get  func(T) *V
}

// Optional declara un campo que admite nulo.
func Optional[T any, V comparable](name string, get func(T) *V) OptionalField[T, V] {
	return OptionalField[T, V]{name: name, get: get}
}

// Name devuelve el nombre lógico del campo.
func (f OptionalField[T, V]) Name() string { return f.name }

// IsNull selecciona los candidatos sin valor.
func (f OptionalField[T, V]) IsNull() Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpIsNull}, eval: func(c T) bool { return f.get(c) == nil }}
}

// IsNotNull selecciona los candidatos con valor.
func (f OptionalField[T, V]) IsNotNull() Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpIsNotNull}, eval: func(c T) bool { return f.get(c) != nil }}
}

// Eq selecciona los candidatos con un valor igual a v (el nulo nunca coincide).
func (f OptionalField[T, V]) Eq(v V) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpEq, Value: v}, eval: func(c T) bool {
		p := f.get(c)
		return p != nil && *p == v
	}}
}

// In selecciona los candidatos con un valor igual a alguno de vs (el nulo nunca coincide).
func (f OptionalField[T, V]) In(vs ...V) Spec[T] {
	set := make(map[V]struct{}, len(vs))
	for _, v := range vs {
		set[v] = struct{}{}
	}
	return Spec[T]{expr: Compare{Field: f.name, Op: OpIn, Value: toAny(vs)}, eval: func(c T) bool {
		p := f.get(c)
		if p == nil {
			return false
		}
		_, ok := set[*p]
		return ok
	}}
}

// CollectionField es una colección hija del agregado (entidades hijas u objetos de valor).
type CollectionField[T any, C any] struct {
	name string
	get  func(T) []C
}

// Collection declara una colección hija. El nombre identifica el mapeo de la hija en los adaptadores.
func Collection[T any, C any](name string, get func(T) []C) CollectionField[T, C] {
	return CollectionField[T, C]{name: name, get: get}
}

// Name devuelve el nombre lógico de la colección.
func (f CollectionField[T, C]) Name() string { return f.name }

// Any selecciona los candidatos con al menos un elemento que cumple where (where nil: no vacía).
func (f CollectionField[T, C]) Any(where Specification[C]) Spec[T] {
	inner := From(where)
	return Spec[T]{
		expr: AnyExpr{Collection: f.name, Where: inner.Expr()},
		eval: func(c T) bool {
			for _, el := range f.get(c) {
				if inner.IsSatisfiedBy(el) {
					return true
				}
			}
			return false
		},
	}
}

// None selecciona los candidatos sin ningún elemento que cumpla where.
func (f CollectionField[T, C]) None(where Specification[C]) Spec[T] { return Not[T](f.Any(where)) }

// IsEmpty selecciona los candidatos con la colección vacía.
func (f CollectionField[T, C]) IsEmpty() Spec[T] { return f.None(nil) }

func toAny[V any](vs []V) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}

// OptionalTimeField es un instante que admite nulo (p. ej. el final de un periodo abierto). Las
// comparaciones nunca coinciden con un valor nulo, ni en memoria ni en SQL, así que «sigue abierto o
// termina más tarde» es f.IsNull().Or(f.After(t)).
type OptionalTimeField[T any] struct {
	name string
	get  func(T) *time.Time
}

// OptionalTime declara un campo time.Time que admite nulo.
func OptionalTime[T any](name string, get func(T) *time.Time) OptionalTimeField[T] {
	return OptionalTimeField[T]{name: name, get: get}
}

// Name devuelve el nombre lógico del campo.
func (f OptionalTimeField[T]) Name() string { return f.name }

// IsNull selecciona los candidatos sin valor.
func (f OptionalTimeField[T]) IsNull() Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpIsNull}, eval: func(c T) bool { return f.get(c) == nil }}
}

// IsNotNull selecciona los candidatos con valor.
func (f OptionalTimeField[T]) IsNotNull() Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpIsNotNull}, eval: func(c T) bool { return f.get(c) != nil }}
}

func (f OptionalTimeField[T]) timeSpec(op Op, t time.Time, ok func(int) bool) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: op, Value: t}, eval: func(c T) bool {
		v := f.get(c)
		return v != nil && ok(v.Compare(t))
	}}
}

// After selecciona los instantes estrictamente posteriores a t (el nulo nunca coincide).
func (f OptionalTimeField[T]) After(t time.Time) Spec[T] {
	return f.timeSpec(OpGt, t, func(r int) bool { return r > 0 })
}

// AtOrAfter selecciona los instantes iguales o posteriores a t (el nulo nunca coincide).
func (f OptionalTimeField[T]) AtOrAfter(t time.Time) Spec[T] {
	return f.timeSpec(OpGe, t, func(r int) bool { return r >= 0 })
}

// Before selecciona los instantes estrictamente anteriores a t (el nulo nunca coincide).
func (f OptionalTimeField[T]) Before(t time.Time) Spec[T] {
	return f.timeSpec(OpLt, t, func(r int) bool { return r < 0 })
}

// AtOrBefore selecciona los instantes iguales o anteriores a t (el nulo nunca coincide).
func (f OptionalTimeField[T]) AtOrBefore(t time.Time) Spec[T] {
	return f.timeSpec(OpLe, t, func(r int) bool { return r <= 0 })
}

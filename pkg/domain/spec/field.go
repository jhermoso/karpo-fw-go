package spec

import (
	"cmp"
	"strings"
	"time"
)

// Field is a typed, comparable attribute of T with a logical name. The accessor gives in-memory
// semantics; the name is what adapters map to storage. Values should be primitives, typed
// identifiers (domain.UUIDBacked / domain.LongBacked) or named primitive types.
type Field[T any, V comparable] struct {
	name string
	get  func(T) V
}

// Comparable declares a field supporting equality operators.
func Comparable[T any, V comparable](name string, get func(T) V) Field[T, V] {
	return Field[T, V]{name: name, get: get}
}

// Name returns the logical field name.
func (f Field[T, V]) Name() string { return f.name }

// Value returns the field value of candidate.
func (f Field[T, V]) Value(candidate T) V { return f.get(candidate) }

// Eq matches candidates whose field equals v.
func (f Field[T, V]) Eq(v V) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpEq, Value: v}, eval: func(c T) bool { return f.get(c) == v }}
}

// Ne matches candidates whose field differs from v.
func (f Field[T, V]) Ne(v V) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpNe, Value: v}, eval: func(c T) bool { return f.get(c) != v }}
}

// In matches candidates whose field equals any of vs (no values: matches nothing).
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

// NotIn matches candidates whose field equals none of vs (no values: matches everything).
func (f Field[T, V]) NotIn(vs ...V) Spec[T] {
	in := f.In(vs...)
	return Spec[T]{
		expr: Compare{Field: f.name, Op: OpNotIn, Value: toAny(vs)},
		eval: func(c T) bool { return !in.IsSatisfiedBy(c) },
	}
}

// OrderedField is a field whose values have a natural order (numbers, strings).
type OrderedField[T any, V cmp.Ordered] struct {
	Field[T, V]
}

// Ordered declares a field supporting equality, range operators and sorting.
func Ordered[T any, V cmp.Ordered](name string, get func(T) V) OrderedField[T, V] {
	return OrderedField[T, V]{Field: Comparable(name, get)}
}

func (f OrderedField[T, V]) cmpSpec(op Op, v V, ok func(int) bool) Spec[T] {
	return Spec[T]{
		expr: Compare{Field: f.name, Op: op, Value: v},
		eval: func(c T) bool { return ok(cmp.Compare(f.get(c), v)) },
	}
}

// Gt matches field > v.
func (f OrderedField[T, V]) Gt(v V) Spec[T] {
	return f.cmpSpec(OpGt, v, func(r int) bool { return r > 0 })
}

// Ge matches field >= v.
func (f OrderedField[T, V]) Ge(v V) Spec[T] {
	return f.cmpSpec(OpGe, v, func(r int) bool { return r >= 0 })
}

// Lt matches field < v.
func (f OrderedField[T, V]) Lt(v V) Spec[T] {
	return f.cmpSpec(OpLt, v, func(r int) bool { return r < 0 })
}

// Le matches field <= v.
func (f OrderedField[T, V]) Le(v V) Spec[T] {
	return f.cmpSpec(OpLe, v, func(r int) bool { return r <= 0 })
}

// Between matches lo <= field <= hi.
func (f OrderedField[T, V]) Between(lo, hi V) Spec[T] { return f.Ge(lo).And(f.Le(hi)) }

// Asc sorts ascending by this field.
func (f OrderedField[T, V]) Asc() Order[T] {
	return Order[T]{Field: f.name, compare: func(a, b T) int { return cmp.Compare(f.get(a), f.get(b)) }}
}

// Desc sorts descending by this field.
func (f OrderedField[T, V]) Desc() Order[T] { o := f.Asc(); o.Desc = true; return o }

// TextField is an ordered string field with pattern operators.
type TextField[T any] struct {
	OrderedField[T, string]
}

// Text declares a string field.
func Text[T any](name string, get func(T) string) TextField[T] {
	return TextField[T]{OrderedField: Ordered(name, get)}
}

func (f TextField[T]) textSpec(op Op, v string, ok func(string) bool) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: op, Value: v}, eval: func(c T) bool { return ok(f.get(c)) }}
}

// Contains matches values containing sub (case sensitivity follows the store collation).
func (f TextField[T]) Contains(sub string) Spec[T] {
	return f.textSpec(OpContains, sub, func(s string) bool { return strings.Contains(s, sub) })
}

// StartsWith matches values starting with prefix (case sensitivity follows the store collation).
func (f TextField[T]) StartsWith(prefix string) Spec[T] {
	return f.textSpec(OpStartsWith, prefix, func(s string) bool { return strings.HasPrefix(s, prefix) })
}

// EndsWith matches values ending with suffix (case sensitivity follows the store collation).
func (f TextField[T]) EndsWith(suffix string) Spec[T] {
	return f.textSpec(OpEndsWith, suffix, func(s string) bool { return strings.HasSuffix(s, suffix) })
}

// EqualFold matches values equal to v ignoring case, on every store.
func (f TextField[T]) EqualFold(v string) Spec[T] {
	return f.textSpec(OpEqualFold, v, func(s string) bool { return strings.EqualFold(s, v) })
}

// ContainsFold matches values containing sub ignoring case, on every store.
func (f TextField[T]) ContainsFold(sub string) Spec[T] {
	lower := strings.ToLower(sub)
	return f.textSpec(OpContainsFold, sub, func(s string) bool { return strings.Contains(strings.ToLower(s), lower) })
}

// TimeField is an instant-valued field. Comparisons use time.Time.Compare (location independent).
type TimeField[T any] struct {
	name string
	get  func(T) time.Time
}

// Time declares a time.Time field.
func Time[T any](name string, get func(T) time.Time) TimeField[T] {
	return TimeField[T]{name: name, get: get}
}

// Name returns the logical field name.
func (f TimeField[T]) Name() string { return f.name }

func (f TimeField[T]) timeSpec(op Op, t time.Time, ok func(int) bool) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: op, Value: t}, eval: func(c T) bool { return ok(f.get(c).Compare(t)) }}
}

// Eq matches the same instant.
func (f TimeField[T]) Eq(t time.Time) Spec[T] {
	return f.timeSpec(OpEq, t, func(r int) bool { return r == 0 })
}

// After matches instants strictly after t.
func (f TimeField[T]) After(t time.Time) Spec[T] {
	return f.timeSpec(OpGt, t, func(r int) bool { return r > 0 })
}

// AtOrAfter matches instants at or after t.
func (f TimeField[T]) AtOrAfter(t time.Time) Spec[T] {
	return f.timeSpec(OpGe, t, func(r int) bool { return r >= 0 })
}

// Before matches instants strictly before t.
func (f TimeField[T]) Before(t time.Time) Spec[T] {
	return f.timeSpec(OpLt, t, func(r int) bool { return r < 0 })
}

// AtOrBefore matches instants at or before t.
func (f TimeField[T]) AtOrBefore(t time.Time) Spec[T] {
	return f.timeSpec(OpLe, t, func(r int) bool { return r <= 0 })
}

// Between matches from <= instant <= to.
func (f TimeField[T]) Between(from, to time.Time) Spec[T] {
	return f.AtOrAfter(from).And(f.AtOrBefore(to))
}

// Asc sorts ascending by this field.
func (f TimeField[T]) Asc() Order[T] {
	return Order[T]{Field: f.name, compare: func(a, b T) int { return f.get(a).Compare(f.get(b)) }}
}

// Desc sorts descending by this field.
func (f TimeField[T]) Desc() Order[T] { o := f.Asc(); o.Desc = true; return o }

// OptionalField is a nullable field; the accessor returns nil when there is no value.
type OptionalField[T any, V comparable] struct {
	name string
	get  func(T) *V
}

// Optional declares a nullable field.
func Optional[T any, V comparable](name string, get func(T) *V) OptionalField[T, V] {
	return OptionalField[T, V]{name: name, get: get}
}

// Name returns the logical field name.
func (f OptionalField[T, V]) Name() string { return f.name }

// IsNull matches candidates without a value.
func (f OptionalField[T, V]) IsNull() Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpIsNull}, eval: func(c T) bool { return f.get(c) == nil }}
}

// IsNotNull matches candidates with a value.
func (f OptionalField[T, V]) IsNotNull() Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpIsNotNull}, eval: func(c T) bool { return f.get(c) != nil }}
}

// Eq matches candidates with a value equal to v (null never matches).
func (f OptionalField[T, V]) Eq(v V) Spec[T] {
	return Spec[T]{expr: Compare{Field: f.name, Op: OpEq, Value: v}, eval: func(c T) bool {
		p := f.get(c)
		return p != nil && *p == v
	}}
}

// In matches candidates with a value equal to any of vs (null never matches).
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

// CollectionField is a child collection of the aggregate (child entities or value objects).
type CollectionField[T any, C any] struct {
	name string
	get  func(T) []C
}

// Collection declares a child collection. The name identifies the child mapping in adapters.
func Collection[T any, C any](name string, get func(T) []C) CollectionField[T, C] {
	return CollectionField[T, C]{name: name, get: get}
}

// Name returns the logical collection name.
func (f CollectionField[T, C]) Name() string { return f.name }

// Any matches candidates with at least one element satisfying where (nil where: non-empty).
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

// None matches candidates with no element satisfying where.
func (f CollectionField[T, C]) None(where Specification[C]) Spec[T] { return Not[T](f.Any(where)) }

// IsEmpty matches candidates with an empty collection.
func (f CollectionField[T, C]) IsEmpty() Spec[T] { return f.None(nil) }

func toAny[V any](vs []V) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}

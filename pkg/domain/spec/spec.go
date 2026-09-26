// Package spec implements the Specification pattern as a typed, translatable expression tree.
//
// A specification has two faces that are built together and can never diverge:
//   - IsSatisfiedBy: evaluation in memory against a candidate (domain rules, the memory adapter);
//   - Expr: an expression tree that persistence adapters translate to their query language,
//     so filtering, ordering and paging always run inside the database.
//
// Specifications are composed from typed fields declared next to the aggregate:
//
//	var (
//		PartyName   = spec.Text[*Party]("legal_name", (*Party).LegalName)
//		PartyActive = spec.Comparable[*Party, bool]("is_active", (*Party).IsActive)
//	)
//	active := PartyActive.Eq(true).And(PartyName.StartsWith("Acme"))
//
// Field names are logical: each adapter maps them to physical columns, keeping the domain
// independent of any storage technology.
package spec

// Specification is the contract repositories accept. Spec[T] is the standard implementation;
// custom types may implement it as long as their Expr is translatable.
type Specification[T any] interface {
	// IsSatisfiedBy evaluates the rule in memory.
	IsSatisfiedBy(candidate T) bool
	// Expr returns the translatable expression tree of the rule.
	Expr() Expr
}

// Spec is an immutable specification over candidates of type T. The zero value matches everything.
type Spec[T any] struct {
	expr Expr
	eval func(T) bool
}

// New builds a specification from an expression and its in-memory evaluation. Prefer the typed
// field constructors; New is the extension point for exotic cases.
func New[T any](expr Expr, eval func(T) bool) Spec[T] {
	return Spec[T]{expr: expr, eval: eval}
}

// All matches every candidate.
func All[T any]() Spec[T] {
	return Spec[T]{expr: Const{Value: true}, eval: func(T) bool { return true }}
}

// None matches no candidate.
func None[T any]() Spec[T] {
	return Spec[T]{expr: Const{Value: false}, eval: func(T) bool { return false }}
}

// IsSatisfiedBy evaluates the specification in memory.
func (s Spec[T]) IsSatisfiedBy(candidate T) bool {
	if s.eval == nil {
		return true
	}
	return s.eval(candidate)
}

// Expr returns the expression tree (Const{true} for the zero Spec).
func (s Spec[T]) Expr() Expr {
	if s.expr == nil {
		return Const{Value: true}
	}
	return s.expr
}

// And returns a specification satisfied when s and all others are satisfied.
func (s Spec[T]) And(others ...Specification[T]) Spec[T] {
	return And(append([]Specification[T]{s}, others...)...)
}

// Or returns a specification satisfied when s or any of others is satisfied.
func (s Spec[T]) Or(others ...Specification[T]) Spec[T] {
	return Or(append([]Specification[T]{s}, others...)...)
}

// Not returns the negation of s.
func (s Spec[T]) Not() Spec[T] { return Not[T](s) }

// From converts any Specification into a Spec (nil becomes All).
func From[T any](s Specification[T]) Spec[T] {
	switch v := s.(type) {
	case nil:
		return All[T]()
	case Spec[T]:
		return v
	default:
		return Spec[T]{expr: s.Expr(), eval: s.IsSatisfiedBy}
	}
}

// And combines specifications with logical AND. Nested ANDs are flattened and TRUE constants dropped.
func And[T any](specs ...Specification[T]) Spec[T] {
	parts := make([]Spec[T], 0, len(specs))
	exprs := make([]Expr, 0, len(specs))
	for _, raw := range specs {
		if raw == nil {
			continue
		}
		s := From(raw)
		switch e := s.Expr().(type) {
		case Const:
			if e.Value {
				continue
			}
			return None[T]()
		case AndExpr:
			exprs = append(exprs, e.Exprs...)
		default:
			exprs = append(exprs, e)
		}
		parts = append(parts, s)
	}
	switch len(parts) {
	case 0:
		return All[T]()
	case 1:
		return parts[0]
	}
	return Spec[T]{
		expr: AndExpr{Exprs: exprs},
		eval: func(c T) bool {
			for _, p := range parts {
				if !p.IsSatisfiedBy(c) {
					return false
				}
			}
			return true
		},
	}
}

// Or combines specifications with logical OR. Nested ORs are flattened and FALSE constants dropped.
func Or[T any](specs ...Specification[T]) Spec[T] {
	parts := make([]Spec[T], 0, len(specs))
	exprs := make([]Expr, 0, len(specs))
	for _, raw := range specs {
		if raw == nil {
			continue
		}
		s := From(raw)
		switch e := s.Expr().(type) {
		case Const:
			if !e.Value {
				continue
			}
			return All[T]()
		case OrExpr:
			exprs = append(exprs, e.Exprs...)
		default:
			exprs = append(exprs, e)
		}
		parts = append(parts, s)
	}
	switch len(parts) {
	case 0:
		return None[T]()
	case 1:
		return parts[0]
	}
	return Spec[T]{
		expr: OrExpr{Exprs: exprs},
		eval: func(c T) bool {
			for _, p := range parts {
				if p.IsSatisfiedBy(c) {
					return true
				}
			}
			return false
		},
	}
}

// Not negates a specification.
func Not[T any](s Specification[T]) Spec[T] {
	inner := From(s)
	eval := func(c T) bool { return !inner.IsSatisfiedBy(c) }
	switch e := inner.Expr().(type) {
	case Const:
		if e.Value {
			return None[T]()
		}
		return All[T]()
	case NotExpr: // double negation
		return Spec[T]{expr: e.Expr, eval: eval}
	}
	return Spec[T]{expr: NotExpr{Expr: inner.Expr()}, eval: eval}
}

// Custom declares a named business rule. eval is its in-memory semantics; every persistence
// adapter that must support the rule registers a translation for name (for SQL adapters see
// sqlrepo.Mapping.Custom). Adapters without a translation fail with domain.ErrUnsupported
// instead of loading data to filter it in memory.
//
//	func HighCreditRisk(limit int64) spec.Spec[*Party] {
//		return spec.Custom("parties.high_credit_risk",
//			func(p *Party) bool { return p.Exposure() > limit }, limit)
//	}
func Custom[T any](name string, eval func(T) bool, args ...any) Spec[T] {
	return Spec[T]{expr: CustomExpr{Name: name, Args: args}, eval: eval}
}

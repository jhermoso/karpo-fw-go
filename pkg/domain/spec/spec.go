// Package spec implementa el patrón Especificación como un árbol de expresiones tipado y traducible.
//
// Una especificación tiene dos caras que se construyen juntas y nunca pueden divergir:
//   - IsSatisfiedBy: evaluación en memoria contra un candidato (reglas de dominio, el adaptador en memoria);
//   - Expr: un árbol de expresiones que los adaptadores de persistencia traducen a su lenguaje de consulta,
//     de modo que el filtrado, la ordenación y la paginación se ejecutan siempre dentro de la base de datos.
//
// Las especificaciones se componen a partir de campos tipados declarados junto al agregado:
//
//	var (
//		PartyName   = spec.Text[*Party]("legal_name", (*Party).LegalName)
//		PartyActive = spec.Comparable[*Party, bool]("is_active", (*Party).IsActive)
//	)
//	active := PartyActive.Eq(true).And(PartyName.StartsWith("Acme"))
//
// Los nombres de campo son lógicos: cada adaptador los traduce a columnas físicas, lo que mantiene
// el dominio independiente de cualquier tecnología de almacenamiento.
package spec

// Specification es el contrato que aceptan los repositorios. Spec[T] es la implementación estándar;
// otros tipos pueden implementarlo siempre que su Expr sea traducible.
type Specification[T any] interface {
	// IsSatisfiedBy evalúa la regla en memoria.
	IsSatisfiedBy(candidate T) bool
	// Expr devuelve el árbol de expresiones traducible de la regla.
	Expr() Expr
}

// Spec es una especificación inmutable sobre candidatos de tipo T. El valor cero lo selecciona todo.
type Spec[T any] struct {
	expr Expr
	eval func(T) bool
}

// New construye una especificación a partir de una expresión y su evaluación en memoria. Son
// preferibles los constructores de campos tipados; New es el punto de extensión para casos raros.
func New[T any](expr Expr, eval func(T) bool) Spec[T] {
	return Spec[T]{expr: expr, eval: eval}
}

// All selecciona todos los candidatos.
func All[T any]() Spec[T] {
	return Spec[T]{expr: Const{Value: true}, eval: func(T) bool { return true }}
}

// None no selecciona ningún candidato.
func None[T any]() Spec[T] {
	return Spec[T]{expr: Const{Value: false}, eval: func(T) bool { return false }}
}

// IsSatisfiedBy evalúa la especificación en memoria.
func (s Spec[T]) IsSatisfiedBy(candidate T) bool {
	if s.eval == nil {
		return true
	}
	return s.eval(candidate)
}

// Expr devuelve el árbol de expresiones (Const{true} para la Spec cero).
func (s Spec[T]) Expr() Expr {
	if s.expr == nil {
		return Const{Value: true}
	}
	return s.expr
}

// And devuelve una especificación que se cumple cuando se cumplen s y todas las demás.
func (s Spec[T]) And(others ...Specification[T]) Spec[T] {
	return And(append([]Specification[T]{s}, others...)...)
}

// Or devuelve una especificación que se cumple cuando se cumple s o cualquiera de las demás.
func (s Spec[T]) Or(others ...Specification[T]) Spec[T] {
	return Or(append([]Specification[T]{s}, others...)...)
}

// Not devuelve la negación de s.
func (s Spec[T]) Not() Spec[T] { return Not[T](s) }

// From convierte cualquier Specification en una Spec (nil pasa a ser All).
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

// And combina especificaciones con un AND lógico. Los AND anidados se aplanan y las constantes TRUE se descartan.
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

// Or combina especificaciones con un OR lógico. Los OR anidados se aplanan y las constantes FALSE se descartan.
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

// Not niega una especificación.
func Not[T any](s Specification[T]) Spec[T] {
	inner := From(s)
	eval := func(c T) bool { return !inner.IsSatisfiedBy(c) }
	switch e := inner.Expr().(type) {
	case Const:
		if e.Value {
			return None[T]()
		}
		return All[T]()
	case NotExpr: // doble negación
		return Spec[T]{expr: e.Expr, eval: eval}
	}
	return Spec[T]{expr: NotExpr{Expr: inner.Expr()}, eval: eval}
}

// Custom declara una regla de negocio con nombre. eval es su semántica en memoria; cada adaptador
// de persistencia que deba admitir la regla registra una traducción para name (para los adaptadores
// SQL véase sqlrepo.Mapping.Custom). Los adaptadores sin traducción fallan con domain.ErrUnsupported
// en lugar de cargar datos para filtrarlos en memoria.
//
//	func HighCreditRisk(limit int64) spec.Spec[*Party] {
//		return spec.Custom("parties.high_credit_risk",
//			func(p *Party) bool { return p.Exposure() > limit }, limit)
//	}
func Custom[T any](name string, eval func(T) bool, args ...any) Spec[T] {
	return Spec[T]{expr: CustomExpr{Name: name, Args: args}, eval: eval}
}

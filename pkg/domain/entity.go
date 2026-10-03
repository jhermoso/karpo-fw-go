// Package domain defines the tactical Domain-Driven Design building blocks of Karpo:
// typed identifiers, entities, aggregate roots, domain events, value-object guidance,
// factories, repository contracts and the domain error taxonomy.
//
// The package is pure: it depends only on the standard library and on its own sub-packages
// (pkg/domain/spec). The architecture tests in pkg/testing/archtest enforce this.
//
// Translation notes from the C# framework (Paranoia.Karpo.Fw.Domain.Contracts):
//   - abstract classes become embeddable structs (BaseEntity, BaseAggregateRoot);
//   - "virtual" template methods become composition (strategies passed in) because Go
//     embedding has no dynamic dispatch: an embedded struct never sees methods of its outer type;
//   - generic algorithms become free generic functions (SameIdentity, spec.And, ...);
//   - exceptions become typed errors that wrap the sentinels in errors.go.
package domain

import (
	"fmt"
	"reflect"
)

// Entity is a domain object defined by its identity rather than by its attributes.
type Entity[ID Identifier] interface {
	ID() ID
}

// BaseEntity is the embeddable foundation for entities. It holds a validated, non-zero identity.
type BaseEntity[ID Identifier] struct {
	id ID
}

// NewBaseEntity validates id and returns the embeddable entity base.
func NewBaseEntity[ID Identifier](id ID) (BaseEntity[ID], error) {
	if id.IsZero() {
		var zero ID
		return BaseEntity[ID]{}, fmt.Errorf("%w: %T must not be zero", ErrInvalidIdentity, zero)
	}
	return BaseEntity[ID]{id: id}, nil
}

// ID returns the entity identity.
func (e BaseEntity[ID]) ID() ID { return e.id }

// SameIdentity reports whether a and b are the same entity: same concrete type and same identity.
// It replaces the C# Entity.Equals override (identity equality plus GetType() check).
func SameIdentity[ID Identifier](a, b Entity[ID]) bool {
	if isNil(a) || isNil(b) {
		return false
	}
	return reflect.TypeOf(a) == reflect.TypeOf(b) && a.ID() == b.ID()
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return rv.IsNil()
	}
	return false
}

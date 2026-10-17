// Package domain define las piezas tácticas de diseño guiado por el dominio de Karpo:
// identificadores tipados, entidades, raíces de agregado, eventos de dominio, pautas para objetos
// de valor, factorías, contratos de repositorio y la taxonomía de errores del dominio.
//
// El paquete es puro: sólo depende de la biblioteca estándar y de sus propios subpaquetes
// (pkg/domain/spec). Las pruebas de arquitectura de pkg/testing/archtest lo hacen cumplir.
//
// Notas de traducción desde el framework de C# (Paranoia.Karpo.Fw.Domain.Contracts):
//   - las clases abstractas pasan a ser structs incrustables (BaseEntity, BaseAggregateRoot);
//   - los métodos plantilla "virtual" pasan a ser composición (estrategias que se pasan) porque la
//     incrustación de Go no tiene despacho dinámico: un struct incrustado nunca ve los métodos de su tipo exterior;
//   - los algoritmos genéricos pasan a ser funciones genéricas libres (SameIdentity, spec.And, ...);
//   - las excepciones pasan a ser errores tipados que envuelven los centinelas de errors.go.
package domain

import (
	"fmt"
	"reflect"
)

// Entity es un objeto de dominio definido por su identidad y no por sus atributos.
type Entity[ID Identifier] interface {
	ID() ID
}

// BaseEntity es la base incrustable de las entidades. Guarda una identidad validada y no nula.
type BaseEntity[ID Identifier] struct {
	id ID
}

// NewBaseEntity valida id y devuelve la base incrustable de la entidad.
func NewBaseEntity[ID Identifier](id ID) (BaseEntity[ID], error) {
	if id.IsZero() {
		var zero ID
		return BaseEntity[ID]{}, fmt.Errorf("%w: %T must not be zero", ErrInvalidIdentity, zero)
	}
	return BaseEntity[ID]{id: id}, nil
}

// ID devuelve la identidad de la entidad.
func (e BaseEntity[ID]) ID() ID { return e.id }

// SameIdentity dice si a y b son la misma entidad: mismo tipo concreto y misma identidad.
// Sustituye a la redefinición de Entity.Equals de C# (igualdad de identidad más la comprobación de GetType()).
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

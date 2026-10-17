package domain

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// ReadRepository es el lado de consulta de un repositorio de agregados.
//
// Contrato que toda implementación debe cumplir (lo comprueba pkg/testing/repotest):
//   - las especificaciones, el orden y la paginación los ejecuta el almacén, nunca cargando la
//     colección entera y filtrando en memoria; una expresión que el almacén no sabe traducir falla
//     con ErrUnsupported;
//   - una especificación nil coincide con todos los agregados;
//   - los agregados devueltos llevan su Version persistida;
//   - Get devuelve un error que coincide con ErrNotFound cuando el agregado no existe.
type ReadRepository[ID Identifier, T AggregateRoot[ID]] interface {
	// Get carga un agregado por su identidad.
	Get(ctx context.Context, id ID) (T, error)

	// Find devuelve los agregados que cumplen s, ordenados por order (y después por identidad).
	Find(ctx context.Context, s spec.Specification[T], order ...spec.Order[T]) ([]T, error)

	// FindPage devuelve una página de los agregados que cumplen s más el total.
	FindPage(ctx context.Context, s spec.Specification[T], page PageRequest[T]) (Page[T], error)

	// Count devuelve cuántos agregados cumplen s.
	Count(ctx context.Context, s spec.Specification[T]) (int64, error)

	// Exists dice si al menos un agregado cumple s.
	Exists(ctx context.Context, s spec.Specification[T]) (bool, error)
}

// WriteRepository es el lado de órdenes de un repositorio de agregados.
//
// Contrato:
//   - Save inserta cuando agg.Version() == 0 y actualiza en otro caso, con concurrencia optimista:
//     la versión almacenada debe ser igual a agg.Version(); si no, devuelve un error que coincide con ErrConflict.
//     Insertar una identidad que ya existe también devuelve ErrConflict. Si sale bien, la
//     versión del agregado se incrementa (véase MarkPersisted);
//   - Save persiste el agregado entero (raíz e hijos) de forma atómica;
//   - Delete borra el agregado con la misma comprobación de concurrencia optimista;
//   - ambos se unen a la unidad de trabajo presente en ctx, si la hay.
//
// Los repositorios nunca despachan eventos de dominio: eso es tarea de la capa de aplicación
// (application.Orchestrator + outbox), para que eventos y estado se confirmen juntos.
type WriteRepository[ID Identifier, T AggregateRoot[ID]] interface {
	Save(ctx context.Context, agg T) error
	Delete(ctx context.Context, agg T) error
}

// Repository es el contrato completo de repositorio de una raíz de agregado.
type Repository[ID Identifier, T AggregateRoot[ID]] interface {
	ReadRepository[ID, T]
	WriteRepository[ID, T]
}

// UnitOfWork define una frontera atómica. Do ejecuta fn en una transacción ligada al contexto
// que recibe fn: toda llamada a repositorio hecha con ese contexto se une a ella. La transacción hace
// commit cuando fn devuelve nil y rollback cuando devuelve un error o entra en pánico. Las llamadas
// anidadas se unen a la unidad de trabajo exterior.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// UnitOfWorkFunc adapta una función a UnitOfWork.
type UnitOfWorkFunc func(ctx context.Context, fn func(ctx context.Context) error) error

// Do llama a f.
func (f UnitOfWorkFunc) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return f(ctx, fn)
}

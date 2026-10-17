package domain

import (
	"math"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
)

// Valores por defecto y límites de paginación.
const (
	DefaultPageSize = 20
	MaxPageSize     = 1000
)

// PageRequest selecciona una página (empezando en 1) y su orden. Sort va tipado por el agregado.
type PageRequest[T any] struct {
	Number int
	Size   int
	Sort   []spec.Order[T]
}

// NewPageRequest construye una petición normalizada.
func NewPageRequest[T any](number, size int, sort ...spec.Order[T]) PageRequest[T] {
	return PageRequest[T]{Number: number, Size: size, Sort: sort}.Normalize()
}

// Normalize aplica los valores por defecto (página 1, DefaultPageSize) y limita Size a MaxPageSize.
func (p PageRequest[T]) Normalize() PageRequest[T] {
	if p.Number < 1 {
		p.Number = 1
	}
	if p.Size <= 0 {
		p.Size = DefaultPageSize
	}
	if p.Size > MaxPageSize {
		p.Size = MaxPageSize
	}
	return p
}

// Offset devuelve el número de elementos que hay que saltar. Un número de página tan grande que el
// producto desbordaría da math.MaxInt: una página más allá del final de cualquier resultado, nunca un desplazamiento negativo.
func (p PageRequest[T]) Offset() int {
	n := p.Normalize()
	if n.Number-1 > math.MaxInt/n.Size {
		return math.MaxInt
	}
	return (n.Number - 1) * n.Size
}

// Page es una página de resultados más el número total de coincidencias.
type Page[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Number     int   `json:"number"`
	Size       int   `json:"size"`
	TotalPages int   `json:"totalPages"`
}

// NewPage construye una Page y calcula TotalPages.
func NewPage[T any](items []T, total int64, number, size int) Page[T] {
	if size <= 0 {
		size = DefaultPageSize
	}
	if items == nil {
		items = []T{}
	}
	return Page[T]{
		Items:      items,
		Total:      total,
		Number:     number,
		Size:       size,
		TotalPages: int((total + int64(size) - 1) / int64(size)),
	}
}

// MapPage convierte los elementos de una página (p. ej. agregados en DTO) y conserva los metadatos de paginación.
func MapPage[T, U any](p Page[T], fn func(T) U) Page[U] {
	items := make([]U, len(p.Items))
	for i, it := range p.Items {
		items[i] = fn(it)
	}
	return Page[U]{Items: items, Total: p.Total, Number: p.Number, Size: p.Size, TotalPages: p.TotalPages}
}

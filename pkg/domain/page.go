package domain

import "github.com/jhermoso/karpo-fw-go/pkg/domain/spec"

// Paging defaults and limits.
const (
	DefaultPageSize = 20
	MaxPageSize     = 1000
)

// PageRequest selects a page (1-based) and its ordering. Sort is typed by the aggregate.
type PageRequest[T any] struct {
	Number int
	Size   int
	Sort   []spec.Order[T]
}

// NewPageRequest builds a normalized request.
func NewPageRequest[T any](number, size int, sort ...spec.Order[T]) PageRequest[T] {
	return PageRequest[T]{Number: number, Size: size, Sort: sort}.Normalize()
}

// Normalize applies defaults (page 1, DefaultPageSize) and caps Size at MaxPageSize.
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

// Offset returns the number of items to skip.
func (p PageRequest[T]) Offset() int {
	n := p.Normalize()
	return (n.Number - 1) * n.Size
}

// Page is one page of results plus the total count of matches.
type Page[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Number     int   `json:"number"`
	Size       int   `json:"size"`
	TotalPages int   `json:"totalPages"`
}

// NewPage builds a Page, computing TotalPages.
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

// MapPage converts the items of a page (e.g. aggregates to DTOs), keeping paging metadata.
func MapPage[T, U any](p Page[T], fn func(T) U) Page[U] {
	items := make([]U, len(p.Items))
	for i, it := range p.Items {
		items[i] = fn(it)
	}
	return Page[U]{Items: items, Total: p.Total, Number: p.Number, Size: p.Size, TotalPages: p.TotalPages}
}

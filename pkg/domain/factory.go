package domain

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/result"
)

// Factory encapsulates complex creation and reconstitution logic for an Aggregate Root or Entity of type T.
type Factory[T any, TParams any] interface {
	// Create instantiates a new T ensuring all creation invariants are satisfied.
	Create(ctx context.Context, params TParams) result.Result[T]
}

// FactoryFunc adapts a function into a Factory.
type FactoryFunc[T any, TParams any] func(ctx context.Context, params TParams) result.Result[T]

func (fn FactoryFunc[T, TParams]) Create(ctx context.Context, params TParams) result.Result[T] {
	return fn(ctx, params)
}

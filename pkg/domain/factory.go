package domain

import "context"

// Factory encapsulates complex creation logic for an aggregate or entity T from parameters P,
// guaranteeing creation invariants (equivalent to the C# IFactory<TProduct, TParam>).
// Simple aggregates just expose a New... constructor function; use Factory when creation needs
// collaborators (sequences, policies, other repositories).
type Factory[T any, P any] interface {
	Create(ctx context.Context, params P) (T, error)
}

// FactoryFunc adapts a function into a Factory.
type FactoryFunc[T any, P any] func(ctx context.Context, params P) (T, error)

// Create calls fn.
func (fn FactoryFunc[T, P]) Create(ctx context.Context, params P) (T, error) {
	return fn(ctx, params)
}

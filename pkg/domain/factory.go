package domain

import "context"

// Factory encapsula la lógica de creación compleja de un agregado o entidad T a partir de los
// parámetros P, garantizando los invariantes de creación (equivale a IFactory<TProduct, TParam> de C#).
// Los agregados sencillos sólo exponen una función constructora New...; Factory se usa cuando la
// creación necesita colaboradores (secuencias, políticas, otros repositorios).
type Factory[T any, P any] interface {
	Create(ctx context.Context, params P) (T, error)
}

// FactoryFunc adapta una función a Factory.
type FactoryFunc[T any, P any] func(ctx context.Context, params P) (T, error)

// Create llama a fn.
func (fn FactoryFunc[T, P]) Create(ctx context.Context, params P) (T, error) {
	return fn(ctx, params)
}

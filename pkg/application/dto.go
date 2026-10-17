package application

// Mapper convierte un valor de origen en un DTO (equivale a IDtoMapper<TSource,TDestination> de C#).
// Los DTO son structs simples con etiquetas JSON; en Go no necesitan interfaz marcadora.
type Mapper[S, D any] func(S) D

// MapSlice convierte con m cada elemento de src.
func MapSlice[S, D any](src []S, m Mapper[S, D]) []D {
	out := make([]D, len(src))
	for i, s := range src {
		out[i] = m(s)
	}
	return out
}

package application

// Mapper converts a source value into a DTO (equivalent to the C# IDtoMapper<TSource,TDestination>).
// DTOs are plain structs with JSON tags; they need no marker interface in Go.
type Mapper[S, D any] func(S) D

// MapSlice converts every element of src with m.
func MapSlice[S, D any](src []S, m Mapper[S, D]) []D {
	out := make([]D, len(src))
	for i, s := range src {
		out[i] = m(s)
	}
	return out
}

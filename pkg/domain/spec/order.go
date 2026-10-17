package spec

// Order es un criterio de ordenación tipado que se crea a partir de un campo ordenado (Field.Asc / Field.Desc).
// Al estar tipado por T, una ordenación de un agregado no puede pasarse al repositorio de otro.
type Order[T any] struct {
	// Field es el nombre lógico del campo que los adaptadores traducen al almacenamiento.
	Field string
	// Desc elige el orden descendente.
	Desc bool

	compare func(a, b T) int
}

// Compare compara a y b según este criterio (adaptadores en memoria).
func (o Order[T]) Compare(a, b T) int {
	if o.compare == nil {
		return 0
	}
	r := o.compare(a, b)
	if o.Desc {
		return -r
	}
	return r
}

// CompareAll aplica los órdenes en secuencia hasta que uno distingue a de b.
func CompareAll[T any](orders []Order[T], a, b T) int {
	for _, o := range orders {
		if r := o.Compare(a, b); r != 0 {
			return r
		}
	}
	return 0
}

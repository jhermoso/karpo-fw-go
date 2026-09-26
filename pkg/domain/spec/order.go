package spec

// Order is a typed sort criterion created from an ordered field (Field.Asc / Field.Desc).
// Being typed by T, an ordering for one aggregate cannot be passed to another's repository.
type Order[T any] struct {
	// Field is the logical field name adapters map to storage.
	Field string
	// Desc selects descending order.
	Desc bool

	compare func(a, b T) int
}

// Compare compares a and b according to this criterion (in-memory adapters).
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

// CompareAll applies orders in sequence until one differentiates a and b.
func CompareAll[T any](orders []Order[T], a, b T) int {
	for _, o := range orders {
		if r := o.Compare(a, b); r != 0 {
			return r
		}
	}
	return 0
}

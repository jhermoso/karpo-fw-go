package domain

// Value objects in Go
//
// A value object is an immutable descriptive concept without identity, compared by value.
// In Go this needs no base type or interface (the C# ValueObject<T> base class exists only to
// override Equals/GetHashCode, which Go gives for free on comparable structs):
//
//	type Email struct{ value string }          // unexported fields: immutable from outside
//
//	func NewEmail(s string) (Email, error) {    // the only way in: always valid
//		var v domain.Validation
//		v.Require(strings.Contains(s, "@"), "email", "format", "invalid e-mail")
//		if err := v.Err(); err != nil { return Email{}, err }
//		return Email{value: strings.ToLower(s)}, nil
//	}
//
//	func (e Email) String() string { return e.value }
//
// Compare with ==. Only when a value object contains slices or maps (not comparable) implement
// Equaler so collaborators can still compare it.

// Equaler is implemented by value objects that are not comparable with == (they contain slices
// or maps) and therefore define structural equality explicitly.
type Equaler[T any] interface {
	Equal(other T) bool
}

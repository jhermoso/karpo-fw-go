package domain

// Objetos de valor en Go
//
// Un objeto de valor es un concepto descriptivo inmutable sin identidad, que se compara por valor.
// En Go no necesita tipo base ni interfaz (la clase base ValueObject<T> de C# sólo existe para
// redefinir Equals/GetHashCode, que Go da gratis en los structs comparables):
//
//	type Email struct{ value string }          // campos no exportados: inmutable desde fuera
//
//	func NewEmail(s string) (Email, error) {    // la única entrada: siempre válido
//		var v domain.Validation
//		v.Require(strings.Contains(s, "@"), "email", "format", "invalid e-mail")
//		if err := v.Err(); err != nil { return Email{}, err }
//		return Email{value: strings.ToLower(s)}, nil
//	}
//
//	func (e Email) String() string { return e.value }
//
// Se compara con ==. Sólo cuando un objeto de valor contiene slices o mapas (no comparables) se
// implementa Equaler, para que los colaboradores puedan seguir comparándolo.

// Equaler lo implementan los objetos de valor que no son comparables con == (contienen slices
// o mapas) y por eso definen explícitamente la igualdad estructural.
type Equaler[T any] interface {
	Equal(other T) bool
}

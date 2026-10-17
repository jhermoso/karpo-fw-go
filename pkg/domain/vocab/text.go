package vocab

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// DescriptionMaxLength mantiene las descripciones compatibles con el esquema de C# (200 caracteres).
const DescriptionMaxLength = 200

// RemarkMaxLength es la longitud máxima de una observación.
const RemarkMaxLength = 1000

// Description es un texto descriptivo corto (1..200 caracteres, espacios simples, sin caracteres
// de control). A diferencia del valor de C#, no hay un mínimo arbitrario de 3 caracteres.
type Description struct{ value string }

// NewDescription valida y normaliza una descripción.
func NewDescription(s string) (Description, error) {
	v, err := freeText(s, "description", DescriptionMaxLength)
	return Description{v}, err
}

// MustDescription es como NewDescription pero entra en pánico si hay error.
func MustDescription(s string) Description {
	d, err := NewDescription(s)
	if err != nil {
		panic(err)
	}
	return d
}

// String devuelve el texto.
func (d Description) String() string { return d.value }

// IsZero dice si falta la descripción.
func (d Description) IsZero() bool { return d.value == "" }

// MarshalText implementa encoding.TextMarshaler.
func (d Description) MarshalText() ([]byte, error) { return []byte(d.value), nil }

// UnmarshalText implementa encoding.TextUnmarshaler (vacío significa ausente).
func (d *Description) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Description{}
		return nil
	}
	v, err := NewDescription(string(b))
	if err == nil {
		*d = v
	}
	return err
}

// Remark es un comentario libre (1..1000 caracteres). Ausente es el valor cero: no hay texto por
// defecto (el Remark de C# tenía por defecto "Party role", lo que filtraba un concepto de Parties al framework).
type Remark struct{ value string }

// NewRemark valida y normaliza una observación.
func NewRemark(s string) (Remark, error) {
	v, err := freeText(s, "remark", RemarkMaxLength)
	return Remark{v}, err
}

// String devuelve el texto.
func (r Remark) String() string { return r.value }

// IsZero dice si falta la observación.
func (r Remark) IsZero() bool { return r.value == "" }

// MarshalText implementa encoding.TextMarshaler.
func (r Remark) MarshalText() ([]byte, error) { return []byte(r.value), nil }

// UnmarshalText implementa encoding.TextUnmarshaler (vacío significa ausente).
func (r *Remark) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*r = Remark{}
		return nil
	}
	v, err := NewRemark(string(b))
	if err == nil {
		*r = v
	}
	return err
}

func freeText(s, field string, max int) (string, error) {
	n := strings.Join(strings.Fields(s), " ")
	switch {
	case n == "":
		return "", invalid(field, "required", field+" is required")
	case utf8.RuneCountInString(n) > max:
		return "", invalid(field, "length", field+" is too long")
	case strings.ContainsFunc(n, unicode.IsControl):
		return "", invalid(field, "format", field+" must not contain control characters")
	}
	return n, nil
}

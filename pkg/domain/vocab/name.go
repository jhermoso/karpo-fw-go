package vocab

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// NameMaxLength es la longitud máxima de un Name en caracteres.
const NameMaxLength = 200

// Name es un nombre legible (de una entrada de catálogo, un rol, un producto...). Su identidad es
// su texto: el uso de mayúsculas es presentación, no parte del valor (el NameFormat de C# ha
// desaparecido; véase CapitalizeWords para la regla de presentación común). Los espacios internos se colapsan.
type Name struct{ value string }

// NewName valida y normaliza un nombre: recortado, con espacios simples, de 1..NameMaxLength
// caracteres y sin caracteres de control.
func NewName(s string) (Name, error) {
	n := strings.Join(strings.Fields(s), " ")
	switch {
	case n == "":
		return Name{}, invalid("name", "required", "name is required")
	case utf8.RuneCountInString(n) > NameMaxLength:
		return Name{}, invalid("name", "length", "name must have at most 200 characters")
	case strings.ContainsFunc(n, unicode.IsControl):
		return Name{}, invalid("name", "format", "name must not contain control characters")
	}
	return Name{n}, nil
}

// MustName es como NewName pero entra en pánico si hay error.
func MustName(s string) Name {
	n, err := NewName(s)
	if err != nil {
		panic(err)
	}
	return n
}

// String devuelve el nombre.
func (n Name) String() string { return n.value }

// IsZero dice si falta el nombre.
func (n Name) IsZero() bool { return n.value == "" }

// EqualFold compara sin distinguir mayúsculas.
func (n Name) EqualFold(o Name) bool { return strings.EqualFold(n.value, o.value) }

// MarshalText implementa encoding.TextMarshaler.
func (n Name) MarshalText() ([]byte, error) { return []byte(n.value), nil }

// UnmarshalText implementa encoding.TextUnmarshaler.
func (n *Name) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*n = Name{}
		return nil
	}
	v, err := NewName(string(b))
	if err == nil {
		*n = v
	}
	return err
}

// nameParticles se quedan en minúsculas dentro de un nombre capitalizado ("Juan de la Fuente").
var nameParticles = map[string]bool{
	"de": true, "del": true, "la": true, "las": true, "los": true, "y": true, "e": true,
	"da": true, "das": true, "do": true, "dos": true, "van": true, "von": true, "der": true, "di": true,
}

// CapitalizeWords pone en mayúscula la primera letra de cada palabra, deja en minúsculas las
// partículas de los nombres de persona y de lugar (salvo al principio) y conserva el resto de cada
// palabra tal como está escrito, para no estropear "McDonald" u "O'Neill". Las palabras escritas
// enteras en mayúsculas o en minúsculas se normalizan ("JUAN" -> "Juan").
func CapitalizeWords(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		lower := strings.ToLower(w)
		if i > 0 && nameParticles[lower] {
			words[i] = lower
			continue
		}
		if w == lower || w == strings.ToUpper(w) {
			w = lower
		}
		r, size := utf8.DecodeRuneInString(w)
		words[i] = string(unicode.ToUpper(r)) + w[size:]
	}
	return strings.Join(words, " ")
}

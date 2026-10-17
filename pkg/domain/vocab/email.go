package vocab

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Email es una dirección de correo electrónico, normalizada a minúsculas (la misma normalización
// que el valor de C#, para que los datos persistidos y las restricciones de unicidad sigan siendo
// compatibles). LocalPart y Domain se derivan del valor normalizado.
type Email struct{ value string }

// NewEmail valida una dirección simple ("user@example.com"; los nombres para mostrar se rechazan).
func NewEmail(s string) (Email, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Email{}, invalid("email", "required", "e-mail is required")
	}
	if utf8.RuneCountInString(s) > 254 {
		return Email{}, invalid("email", "length", "e-mail must have at most 254 characters")
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Name != "" || addr.Address != s {
		return Email{}, invalid("email", "format", "invalid e-mail "+quote(s))
	}
	at := strings.LastIndexByte(s, '@')
	local, domainPart := s[:at], s[at+1:]
	if len(local) > 64 || !validDomain(domainPart) {
		return Email{}, invalid("email", "format", "invalid e-mail "+quote(s))
	}
	return Email{strings.ToLower(s)}, nil
}

func validDomain(d string) bool {
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || strings.HasPrefix(l, "-") || strings.HasSuffix(l, "-") {
			return false
		}
	}
	return true
}

// MustEmail es como NewEmail pero entra en pánico si hay error.
func MustEmail(s string) Email {
	e, err := NewEmail(s)
	if err != nil {
		panic(err)
	}
	return e
}

// String devuelve la dirección normalizada.
func (e Email) String() string { return e.value }

// IsZero dice si falta la dirección.
func (e Email) IsZero() bool { return e.value == "" }

// LocalPart devuelve la parte anterior a '@'.
func (e Email) LocalPart() string { return e.value[:strings.LastIndexByte(e.value, '@')] }

// Domain devuelve la parte posterior a '@'.
func (e Email) Domain() string { return e.value[strings.LastIndexByte(e.value, '@')+1:] }

// MarshalText implementa encoding.TextMarshaler.
func (e Email) MarshalText() ([]byte, error) { return []byte(e.value), nil }

// UnmarshalText implementa encoding.TextUnmarshaler.
func (e *Email) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*e = Email{}
		return nil
	}
	v, err := NewEmail(string(b))
	if err == nil {
		*e = v
	}
	return err
}

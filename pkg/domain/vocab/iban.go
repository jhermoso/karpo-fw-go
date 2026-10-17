package vocab

import "strings"

// IBAN es un número de cuenta bancaria internacional (ISO 13616): código de país, dos dígitos de
// control y hasta 30 letras o dígitos, verificado con ISO 7064 MOD 97-10. La longitud y el formato
// de cada país pertenecen a los datos de referencia (el perfil de país del contexto Geography), no al valor.
type IBAN struct{ value string }

// NewIBAN normaliza (mayúsculas, sin espacios) y valida un IBAN.
func NewIBAN(s string) (IBAN, error) {
	v := strings.ToUpper(strings.Join(strings.Fields(s), ""))
	if len(v) < 15 || len(v) > 34 || !letters(v[:2]) || !digits(v[2:4]) || !alnum(v[4:]) {
		return IBAN{}, invalid("iban", "format", "IBAN must be a country code, 2 check digits and up to 30 letters or digits")
	}
	if ibanMod97(v[4:]+v[:4]) != 1 {
		return IBAN{}, invalid("iban", "check_digit", "IBAN check digits do not match")
	}
	return IBAN{v}, nil
}

// String devuelve la forma electrónica (sin espacios).
func (i IBAN) String() string { return i.value }

// IsZero dice si falta el IBAN.
func (i IBAN) IsZero() bool { return i.value == "" }

// Country devuelve el código ISO 3166 alfa-2 del IBAN.
func (i IBAN) Country() string {
	if i.value == "" {
		return ""
	}
	return i.value[:2]
}

// Formatted devuelve la forma en papel, en grupos de cuatro caracteres.
func (i IBAN) Formatted() string {
	var b strings.Builder
	for k, r := range i.value {
		if k > 0 && k%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// MarshalText implementa encoding.TextMarshaler.
func (i IBAN) MarshalText() ([]byte, error) { return []byte(i.value), nil }

// UnmarshalText implementa encoding.TextUnmarshaler.
func (i *IBAN) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*i = IBAN{}
		return nil
	}
	v, err := NewIBAN(string(b))
	if err != nil {
		return err
	}
	*i = v
	return nil
}

func letters(s string) bool {
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return s != ""
}

// ibanMod97 calcula el valor módulo 97, dígito a dígito, contando las letras como 10..35.
func ibanMod97(s string) int {
	m := 0
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			n := int(r-'A') + 10
			m = (m*100 + n) % 97
		} else {
			m = (m*10 + int(r-'0')) % 97
		}
	}
	return m
}

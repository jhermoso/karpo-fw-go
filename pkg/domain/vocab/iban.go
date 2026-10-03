package vocab

import "strings"

// IBAN is an International Bank Account Number (ISO 13616): country code, two check digits and
// up to 30 letters or digits, verified with ISO 7064 MOD 97-10. The length and format of each
// country belong to reference data (the Geography context's country profile), not to the value.
type IBAN struct{ value string }

// NewIBAN normalizes (upper case, no spaces) and validates an IBAN.
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

// String returns the electronic form (no spaces).
func (i IBAN) String() string { return i.value }

// IsZero reports whether the IBAN is absent.
func (i IBAN) IsZero() bool { return i.value == "" }

// Country returns the ISO 3166 alpha-2 code of the IBAN.
func (i IBAN) Country() string {
	if i.value == "" {
		return ""
	}
	return i.value[:2]
}

// Formatted returns the paper form, in groups of four characters.
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

// MarshalText implements encoding.TextMarshaler.
func (i IBAN) MarshalText() ([]byte, error) { return []byte(i.value), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
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

// ibanMod97 computes the value mod 97, letters counting as 10..35, digit by digit.
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

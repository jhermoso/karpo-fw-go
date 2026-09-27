package vocab

import (
	"net/mail"
	"strings"
	"unicode/utf8"
)

// Email is an e-mail address, normalized to lower case (the same normalization as the C# value,
// so persisted data and uniqueness constraints stay compatible). LocalPart and Domain are
// derived from the normalized value.
type Email struct{ value string }

// NewEmail validates a bare address ("user@example.com"; display names are rejected).
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

// MustEmail is like NewEmail but panics on error.
func MustEmail(s string) Email {
	e, err := NewEmail(s)
	if err != nil {
		panic(err)
	}
	return e
}

// String returns the normalized address.
func (e Email) String() string { return e.value }

// IsZero reports whether the address is absent.
func (e Email) IsZero() bool { return e.value == "" }

// LocalPart returns the part before '@'.
func (e Email) LocalPart() string { return e.value[:strings.LastIndexByte(e.value, '@')] }

// Domain returns the part after '@'.
func (e Email) Domain() string { return e.value[strings.LastIndexByte(e.value, '@')+1:] }

// MarshalText implements encoding.TextMarshaler.
func (e Email) MarshalText() ([]byte, error) { return []byte(e.value), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
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

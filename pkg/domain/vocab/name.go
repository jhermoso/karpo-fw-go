package vocab

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// NameMaxLength is the maximum length of a Name in characters.
const NameMaxLength = 200

// Name is a human-readable name (of a catalog entry, a role, a product...). Its identity is its
// text: casing styles are presentation, not part of the value (the C# NameFormat is gone; see
// CapitalizeWords for the common presentation rule). Internal whitespace is collapsed.
type Name struct{ value string }

// NewName validates and normalizes a name: trimmed, single spaces, 1..NameMaxLength characters,
// no control characters.
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

// MustName is like NewName but panics on error.
func MustName(s string) Name {
	n, err := NewName(s)
	if err != nil {
		panic(err)
	}
	return n
}

// String returns the name.
func (n Name) String() string { return n.value }

// IsZero reports whether the name is absent.
func (n Name) IsZero() bool { return n.value == "" }

// EqualFold compares ignoring case.
func (n Name) EqualFold(o Name) bool { return strings.EqualFold(n.value, o.value) }

// MarshalText implements encoding.TextMarshaler.
func (n Name) MarshalText() ([]byte, error) { return []byte(n.value), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
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

// nameParticles stay in lower case inside a capitalized name ("Juan de la Fuente").
var nameParticles = map[string]bool{
	"de": true, "del": true, "la": true, "las": true, "los": true, "y": true, "e": true,
	"da": true, "das": true, "do": true, "dos": true, "van": true, "von": true, "der": true, "di": true,
}

// CapitalizeWords capitalizes the first letter of each word, keeps the particles of personal
// and place names in lower case (except at the start) and preserves the rest of each word as
// written, so "McDonald" or "O'Neill" are not damaged. Words written entirely in upper or lower
// case are normalized ("JUAN" -> "Juan").
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

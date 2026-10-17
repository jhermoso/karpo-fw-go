package vocab

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Tag es una etiqueta libre de clasificación, normalizada a minúsculas y con espacios simples.
type Tag struct{ value string }

// NewTag valida una etiqueta: 1..50 caracteres, que empieza por una letra o un dígito, seguida de
// letras, dígitos, espacios, '-', '_' o '.'.
func NewTag(s string) (Tag, error) {
	t := strings.ToLower(strings.Join(strings.Fields(s), " "))
	if t == "" {
		return Tag{}, invalid("tag", "required", "tag is required")
	}
	if utf8.RuneCountInString(t) > 50 {
		return Tag{}, invalid("tag", "length", "tag must have at most 50 characters")
	}
	for i, r := range t {
		ok := unicode.IsLetter(r) || unicode.IsDigit(r)
		if i > 0 {
			ok = ok || r == ' ' || r == '-' || r == '_' || r == '.'
		}
		if !ok {
			return Tag{}, invalid("tag", "format", "tag contains invalid characters: "+quote(s))
		}
	}
	return Tag{t}, nil
}

// String devuelve la etiqueta.
func (t Tag) String() string { return t.value }

// IsZero dice si falta la etiqueta.
func (t Tag) IsZero() bool { return t.value == "" }

// MarshalText implementa encoding.TextMarshaler.
func (t Tag) MarshalText() ([]byte, error) { return []byte(t.value), nil }

// UnmarshalText implementa encoding.TextUnmarshaler.
func (t *Tag) UnmarshalText(b []byte) error {
	v, err := NewTag(string(b))
	if err == nil {
		*t = v
	}
	return err
}

// TagSet es un conjunto inmutable y ordenado de etiquetas distintas. A diferencia del TagSet de C#, puede estar vacío.
type TagSet struct{ tags []Tag }

// NewTagSet construye un conjunto a partir de etiquetas en bruto e informa de todas las no válidas.
func NewTagSet(labels ...string) (TagSet, error) {
	tags := make([]Tag, 0, len(labels))
	var errs []error
	for _, l := range labels {
		t, err := NewTag(l)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		tags = append(tags, t)
	}
	if len(errs) > 0 {
		return TagSet{}, errs[0]
	}
	return TagSetOf(tags...), nil
}

// TagSetOf construye un conjunto a partir de etiquetas válidas.
func TagSetOf(tags ...Tag) TagSet {
	out := slices.Clone(tags)
	slices.SortFunc(out, func(a, b Tag) int { return strings.Compare(a.value, b.value) })
	return TagSet{tags: slices.Compact(out)}
}

// Tags devuelve las etiquetas en orden.
func (s TagSet) Tags() []Tag { return slices.Clone(s.tags) }

// Len devuelve el número de etiquetas.
func (s TagSet) Len() int { return len(s.tags) }

// Contains dice si t está en el conjunto.
func (s TagSet) Contains(t Tag) bool {
	_, ok := slices.BinarySearchFunc(s.tags, t, func(a, b Tag) int { return strings.Compare(a.value, b.value) })
	return ok
}

// With devuelve un conjunto nuevo que incluye t.
func (s TagSet) With(t Tag) TagSet { return TagSetOf(append(s.Tags(), t)...) }

// Without devuelve un conjunto nuevo que excluye t.
func (s TagSet) Without(t Tag) TagSet {
	return TagSet{tags: slices.DeleteFunc(s.Tags(), func(x Tag) bool { return x == t })}
}

// Equal dice si ambos conjuntos contienen las mismas etiquetas.
func (s TagSet) Equal(o TagSet) bool { return slices.Equal(s.tags, o.tags) }

// String devuelve "a, b, c".
func (s TagSet) String() string {
	parts := make([]string, len(s.tags))
	for i, t := range s.tags {
		parts[i] = t.value
	}
	return strings.Join(parts, ", ")
}

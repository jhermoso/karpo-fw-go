package vocab

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Tag is a free classification label, normalized to lower case with single spaces.
type Tag struct{ value string }

// NewTag validates a tag: 1..50 characters, starting with a letter or digit, then letters,
// digits, spaces, '-', '_' or '.'.
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

// String returns the tag.
func (t Tag) String() string { return t.value }

// IsZero reports whether the tag is absent.
func (t Tag) IsZero() bool { return t.value == "" }

// MarshalText implements encoding.TextMarshaler.
func (t Tag) MarshalText() ([]byte, error) { return []byte(t.value), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (t *Tag) UnmarshalText(b []byte) error {
	v, err := NewTag(string(b))
	if err == nil {
		*t = v
	}
	return err
}

// TagSet is an immutable, sorted set of distinct tags. Unlike the C# TagSet it may be empty.
type TagSet struct{ tags []Tag }

// NewTagSet builds a set from raw labels, reporting every invalid one.
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

// TagSetOf builds a set from valid tags.
func TagSetOf(tags ...Tag) TagSet {
	out := slices.Clone(tags)
	slices.SortFunc(out, func(a, b Tag) int { return strings.Compare(a.value, b.value) })
	return TagSet{tags: slices.Compact(out)}
}

// Tags returns the tags in order.
func (s TagSet) Tags() []Tag { return slices.Clone(s.tags) }

// Len returns the number of tags.
func (s TagSet) Len() int { return len(s.tags) }

// Contains reports whether t is in the set.
func (s TagSet) Contains(t Tag) bool {
	_, ok := slices.BinarySearchFunc(s.tags, t, func(a, b Tag) int { return strings.Compare(a.value, b.value) })
	return ok
}

// With returns a new set including t.
func (s TagSet) With(t Tag) TagSet { return TagSetOf(append(s.Tags(), t)...) }

// Without returns a new set excluding t.
func (s TagSet) Without(t Tag) TagSet {
	return TagSet{tags: slices.DeleteFunc(s.Tags(), func(x Tag) bool { return x == t })}
}

// Equal reports whether both sets contain the same tags.
func (s TagSet) Equal(o TagSet) bool { return slices.Equal(s.tags, o.tags) }

// String returns "a, b, c".
func (s TagSet) String() string {
	parts := make([]string, len(s.tags))
	for i, t := range s.tags {
		parts[i] = t.value
	}
	return strings.Join(parts, ", ")
}

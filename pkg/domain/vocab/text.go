package vocab

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// DescriptionMaxLength keeps descriptions compatible with the C# schema (200 characters).
const DescriptionMaxLength = 200

// RemarkMaxLength is the maximum length of a remark.
const RemarkMaxLength = 1000

// Description is a short descriptive text (1..200 characters, single spaces, no control
// characters). Unlike the C# value there is no arbitrary 3-character minimum.
type Description struct{ value string }

// NewDescription validates and normalizes a description.
func NewDescription(s string) (Description, error) {
	v, err := freeText(s, "description", DescriptionMaxLength)
	return Description{v}, err
}

// MustDescription is like NewDescription but panics on error.
func MustDescription(s string) Description {
	d, err := NewDescription(s)
	if err != nil {
		panic(err)
	}
	return d
}

// String returns the text.
func (d Description) String() string { return d.value }

// IsZero reports whether the description is absent.
func (d Description) IsZero() bool { return d.value == "" }

// MarshalText implements encoding.TextMarshaler.
func (d Description) MarshalText() ([]byte, error) { return []byte(d.value), nil }

// UnmarshalText implements encoding.TextUnmarshaler (empty means absent).
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

// Remark is a free comment (1..1000 characters). Absent is the zero value: there is no default
// text (the C# Remark defaulted to "Party role", leaking a Parties concept into the framework).
type Remark struct{ value string }

// NewRemark validates and normalizes a remark.
func NewRemark(s string) (Remark, error) {
	v, err := freeText(s, "remark", RemarkMaxLength)
	return Remark{v}, err
}

// String returns the text.
func (r Remark) String() string { return r.value }

// IsZero reports whether the remark is absent.
func (r Remark) IsZero() bool { return r.value == "" }

// MarshalText implements encoding.TextMarshaler.
func (r Remark) MarshalText() ([]byte, error) { return []byte(r.value), nil }

// UnmarshalText implements encoding.TextUnmarshaler (empty means absent).
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

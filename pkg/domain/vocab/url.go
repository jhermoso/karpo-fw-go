package vocab

import (
	"net/url"
	"strings"
)

// URL is an absolute http(s) URL (web sites, documents, callbacks).
type URL struct{ value string }

// NewURL validates an absolute http or https URL with a host.
func NewURL(s string) (URL, error) {
	s = strings.TrimSpace(s)
	u, err := url.Parse(s)
	if err != nil || !u.IsAbs() || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return URL{}, invalid("url", "format", "an absolute http(s) URL is required, got "+quote(s))
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return URL{u.String()}, nil
}

// MustURL is like NewURL but panics on error.
func MustURL(s string) URL {
	u, err := NewURL(s)
	if err != nil {
		panic(err)
	}
	return u
}

// String returns the URL.
func (u URL) String() string { return u.value }

// IsZero reports whether the URL is absent.
func (u URL) IsZero() bool { return u.value == "" }

// Parsed returns a fresh *url.URL (scheme, host, path, query...).
func (u URL) Parsed() *url.URL {
	p, _ := url.Parse(u.value)
	return p
}

// MarshalText implements encoding.TextMarshaler.
func (u URL) MarshalText() ([]byte, error) { return []byte(u.value), nil }

// UnmarshalText implements encoding.TextUnmarshaler.
func (u *URL) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*u = URL{}
		return nil
	}
	v, err := NewURL(string(b))
	if err == nil {
		*u = v
	}
	return err
}

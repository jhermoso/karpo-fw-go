package vocab

import (
	"net/url"
	"strings"
)

// URL es una URL http(s) absoluta (sitios web, documentos, callbacks).
type URL struct{ value string }

// NewURL valida una URL http o https absoluta con host.
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

// MustURL es como NewURL pero entra en pánico si hay error.
func MustURL(s string) URL {
	u, err := NewURL(s)
	if err != nil {
		panic(err)
	}
	return u
}

// String devuelve la URL.
func (u URL) String() string { return u.value }

// IsZero dice si falta la URL.
func (u URL) IsZero() bool { return u.value == "" }

// Parsed devuelve un *url.URL nuevo (esquema, host, ruta, consulta...).
func (u URL) Parsed() *url.URL {
	p, _ := url.Parse(u.value)
	return p
}

// MarshalText implementa encoding.TextMarshaler.
func (u URL) MarshalText() ([]byte, error) { return []byte(u.value), nil }

// UnmarshalText implementa encoding.TextUnmarshaler.
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

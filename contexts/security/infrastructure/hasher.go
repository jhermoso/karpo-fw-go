package infrastructure

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Parámetros de PBKDF2 (decisión 3). 600 000 iteraciones de HMAC-SHA256 es la cifra de OWASP para
// este algoritmo; el C# usaba 10 000.
const (
	DefaultIterations = 600_000
	pbkdf2Algorithm   = "pbkdf2-sha256"
	pbkdf2SaltSize    = 16
	pbkdf2KeySize     = 32
	maxIterations     = 10_000_000 // rechaza costes absurdos encontrados en un hash guardado
	csharpIterations  = 10_000
)

// PBKDF2 calcula el hash de las contraseñas con PBKDF2-HMAC-SHA256 de la biblioteca estándar. La
// forma guardada es autodescriptiva, "pbkdf2-sha256$<iterations>$<salt>$<key>" (base64 sin
// relleno), así que el coste se puede subir, o el algoritmo sustituir, sin una migración: un hash
// más débil que la configuración actual sigue verificándose y se informa como desfasado, y el
// login lo recalcula.
type PBKDF2 struct{ iterations int }

// NewPBKDF2 construye un hasher (iterations <= 0 toma DefaultIterations).
func NewPBKDF2(iterations int) PBKDF2 {
	if iterations <= 0 {
		iterations = DefaultIterations
	}
	return PBKDF2{iterations: iterations}
}

var b64 = base64.RawStdEncoding

func encodeHash(iterations int, salt, key []byte) string {
	return fmt.Sprintf("%s$%d$%s$%s", pbkdf2Algorithm, iterations, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// Hash implementa application.PasswordHasher.
func (h PBKDF2) Hash(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, h.iterations, pbkdf2KeySize)
	if err != nil {
		return "", err
	}
	return encodeHash(h.iterations, salt, key), nil
}

func decodeHash(hash string) (iterations int, salt, key []byte, err error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != pbkdf2Algorithm {
		return 0, nil, nil, errors.New("unknown hash format")
	}
	if iterations, err = strconv.Atoi(parts[1]); err != nil || iterations < 1 || iterations > maxIterations {
		return 0, nil, nil, errors.New("invalid iterations")
	}
	if salt, err = b64.DecodeString(parts[2]); err != nil || len(salt) == 0 {
		return 0, nil, nil, errors.New("invalid salt")
	}
	if key, err = b64.DecodeString(parts[3]); err != nil || len(key) == 0 {
		return 0, nil, nil, errors.New("invalid key")
	}
	return iterations, salt, key, nil
}

// Verify implementa application.PasswordHasher. La comparación tarda un tiempo constante.
func (h PBKDF2) Verify(password, hash string) (ok, stale bool) {
	iterations, salt, want, err := decodeHash(hash)
	if err != nil {
		return false, false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil || subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	return true, iterations < h.iterations
}

// FromCSharp convierte una fila security_user de C# (PasswordHash en base64, PasswordSalt como
// bytes, HashAlgorithm PBKDF2_SHA256 con 10 000 iteraciones) a la forma autodescriptiva, para que
// los usuarios importados conserven su contraseña y reciban un hash actual en su primer login.
func FromCSharp(passwordHash string, salt []byte) (string, error) {
	key, err := base64.StdEncoding.DecodeString(passwordHash)
	if err != nil || len(key) == 0 || len(salt) == 0 {
		return "", errors.New("not a C# PBKDF2_SHA256 hash")
	}
	return encodeHash(csharpIterations, salt, key), nil
}

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

// PBKDF2 parameters (decision 3). 600 000 iterations of HMAC-SHA256 is the OWASP figure for this
// algorithm; the C# used 10 000.
const (
	DefaultIterations = 600_000
	pbkdf2Algorithm   = "pbkdf2-sha256"
	pbkdf2SaltSize    = 16
	pbkdf2KeySize     = 32
	maxIterations     = 10_000_000 // refuses absurd costs found in a stored hash
	csharpIterations  = 10_000
)

// PBKDF2 hashes passwords with PBKDF2-HMAC-SHA256 from the standard library. The stored form is
// self-describing, "pbkdf2-sha256$<iterations>$<salt>$<key>" (base64 without padding), so the
// cost can be raised, or the algorithm replaced, without a migration: a hash weaker than the
// current settings still verifies and is reported as stale, and the login recomputes it.
type PBKDF2 struct{ iterations int }

// NewPBKDF2 builds a hasher (iterations <= 0 takes DefaultIterations).
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

// Hash implements application.PasswordHasher.
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

// Verify implements application.PasswordHasher. The comparison takes constant time.
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

// FromCSharp converts a C# security_user row (PasswordHash in base64, PasswordSalt as bytes,
// HashAlgorithm PBKDF2_SHA256 with 10 000 iterations) into the self-describing form, so imported
// users keep their password and get a current hash on their first login.
func FromCSharp(passwordHash string, salt []byte) (string, error) {
	key, err := base64.StdEncoding.DecodeString(passwordHash)
	if err != nil || len(key) == 0 || len(salt) == 0 {
		return "", errors.New("not a C# PBKDF2_SHA256 hash")
	}
	return encodeHash(csharpIterations, salt, key), nil
}

package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"unicode/utf8"
)

// ClipBytes recorta s a n bytes como mucho sin partir una runa.
func ClipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// MaxEnvelopeKey es cuántos bytes caben en la identidad guardada de un mensaje.
const MaxEnvelopeKey = 64

// EnvelopeKey es la identidad con la que se guarda un mensaje recibido: la suya, o su huella
// SHA-256 en hexadecimal (64 caracteres) cuando no cabe en MaxEnvelopeKey. Un mismo mensaje enviado
// otra vez da la misma huella: así se reconoce lo que ya está guardado. Un mensaje sin identidad
// no se puede reconocer: recibe una identidad nueva cada vez, que no coincide con la de ningún otro,
// y quien lo guarda no ha de buscarlo antes como repetido.
func EnvelopeKey(id string) string {
	switch {
	case id == "":
		return "no-id:" + NewUUID().String()
	case len(id) <= MaxEnvelopeKey:
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

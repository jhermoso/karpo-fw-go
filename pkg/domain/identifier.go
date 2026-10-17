package domain

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Identifier es la restricción que debe cumplir toda identidad de entidad.
//
// Es la contrapartida en Go de la familia IBaseIdentifier / ILongIdentifier de C#: las identidades
// son objetos de valor fuertemente tipados, así que nunca se puede pasar un PartyID donde se espera un OrderID.
// La forma idiomática de declarar uno es incrustar un identificador primitivo:
//
//	type PartyID struct{ domain.UUID }
//	type LineNo  struct{ domain.LongID }
type Identifier interface {
	comparable
	// IsZero dice si el identificador es el valor cero (sin asignar).
	IsZero() bool
	// String devuelve la representación textual canónica del identificador.
	String() string
}

// UUIDBacked lo implementan los identificadores que se almacenan como UUID.
// Los adaptadores de persistencia lo usan para convertir los identificadores tipados en valores de base de datos
// (equivale a ICommonBackedIdentifier.ToBaseType de C#).
type UUIDBacked interface {
	BaseUUID() UUID
}

// LongBacked lo implementan los identificadores que se almacenan como int64
// (equivale a ICommonLongBackedIdentifier de C#).
type LongBacked interface {
	BaseLong() int64
}

// DateBacked lo implementan los objetos de valor de fecha civil (vocab.Date): un día de calendario
// sin hora ni zona horaria. Los adaptadores de persistencia los enlazan como fechas, nunca como
// instantes, para que el día no pueda moverse con la zona horaria de la sesión o del servidor.
type DateBacked interface {
	BaseDate() (year int, month time.Month, day int)
}

// UUID es un identificador único universal de la RFC 9562. Es comparable, así que puede usarse
// como clave de mapa y compararse con ==. El valor cero es el UUID nulo.
type UUID [16]byte

var (
	uuidMu  sync.Mutex
	uuidMS  uint64
	uuidSeq uint16
)

// NewUUID devuelve un UUID nuevo ordenado por tiempo (versión 7). La versión 7 mantiene compactos
// los índices B-tree en todas las bases de datos soportadas. Los UUID generados por un proceso son
// estrictamente crecientes: dentro del mismo milisegundo se usa un contador de 12 bits (RFC 9562,
// método 1), así que ordenar por id conserva el orden de creación (p. ej. mensajes del outbox
// lanzados en el mismo milisegundo).
func NewUUID() UUID {
	uuidMu.Lock()
	ms := uint64(time.Now().UnixMilli())
	if ms <= uuidMS {
		ms = uuidMS
		uuidSeq++
		if uuidSeq > 0x0fff {
			ms++
			uuidSeq = 0
		}
	} else {
		uuidSeq = 0
	}
	uuidMS = ms
	seq := uuidSeq
	uuidMu.Unlock()

	var u UUID
	binary.BigEndian.PutUint16(u[0:2], uint16(ms>>32))
	binary.BigEndian.PutUint32(u[2:6], uint32(ms))
	_, _ = rand.Read(u[8:])
	u[6] = 0x70 | byte(seq>>8)&0x0f // versión 7 + bits altos del contador
	u[7] = byte(seq)
	u[8] = (u[8] & 0x3f) | 0x80 // variante RFC 9562
	return u
}

// ParseUUID analiza la forma canónica de 36 caracteres, la hexadecimal de 32 o la forma entre llaves.
func ParseUUID(s string) (UUID, error) {
	var u UUID
	s = strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}")
	switch len(s) {
	case 36:
		if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
			return u, fmt.Errorf("%w: malformed uuid %q", ErrInvalidIdentity, s)
		}
		s = s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:]
	case 32:
	default:
		return u, fmt.Errorf("%w: malformed uuid %q", ErrInvalidIdentity, s)
	}
	if _, err := hex.Decode(u[:], []byte(s)); err != nil {
		return UUID{}, fmt.Errorf("%w: malformed uuid %q", ErrInvalidIdentity, s)
	}
	return u, nil
}

// MustParseUUID es como ParseUUID pero entra en pánico si hay error. Pensada para constantes y pruebas.
func MustParseUUID(s string) UUID {
	u, err := ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

// UUIDFromBytes construye un UUID a partir de su representación big-endian de 16 bytes (RFC).
func UUIDFromBytes(b []byte) (UUID, error) {
	var u UUID
	if len(b) != len(u) {
		return u, fmt.Errorf("%w: uuid requires 16 bytes, got %d", ErrInvalidIdentity, len(b))
	}
	copy(u[:], b)
	return u, nil
}

// IsZero dice si u es el UUID nulo.
func (u UUID) IsZero() bool { return u == UUID{} }

// BaseUUID devuelve el propio u. Permite que los identificadores tipados que incrustan UUID satisfagan UUIDBacked.
func (u UUID) BaseUUID() UUID { return u }

// Bytes devuelve la representación big-endian de 16 bytes.
func (u UUID) Bytes() []byte { return append([]byte(nil), u[:]...) }

// String devuelve la representación canónica 8-4-4-4-12 en minúsculas.
func (u UUID) String() string {
	var buf [36]byte
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:], u[10:])
	return string(buf[:])
}

// MarshalText implementa encoding.TextMarshaler para que los identificadores tipados se serialicen como cadenas JSON.
func (u UUID) MarshalText() ([]byte, error) { return []byte(u.String()), nil }

// UnmarshalText implementa encoding.TextUnmarshaler.
func (u *UUID) UnmarshalText(b []byte) error {
	parsed, err := ParseUUID(string(b))
	if err != nil {
		return err
	}
	*u = parsed
	return nil
}

// LongID es un identificador respaldado por int64 para claves numéricas de secuencia o naturales.
// Se incrusta para declarar identificadores numéricos tipados: type InvoiceNo struct{ domain.LongID }.
type LongID int64

// IsZero dice si el identificador está sin asignar (0).
func (l LongID) IsZero() bool { return l == 0 }

// BaseLong devuelve el int64 subyacente para que los identificadores tipados satisfagan LongBacked.
func (l LongID) BaseLong() int64 { return int64(l) }

// String devuelve la representación decimal.
func (l LongID) String() string { return strconv.FormatInt(int64(l), 10) }

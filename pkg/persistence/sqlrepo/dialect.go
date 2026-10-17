// Package sqlrepo implementa el contrato de repositorio del dominio (y el outbox) sobre
// database/sql para cualquier base de datos relacional, mediante un Dialect intercambiable.
//
// Es agnóstico de la tecnología en los dos sentidos:
//   - el dominio nunca ve SQL: los agregados se mapean con un Mapping (campos lógicos a columnas,
//     funciones de deshidratación e hidratación) que vive en la capa de infraestructura del
//     contexto delimitado;
//   - el SQL nunca queda atado a un motor: cada detalle propio del motor (marcadores de
//     parámetro, entrecomillado, paginación, representación de booleanos y UUID, escape de LIKE,
//     límites de listas IN, códigos de error) es un método de Dialect. Los dialectos viven en
//     subpaquetes (sqlite, postgres, sqlserver, oracle, mysql) y no importan drivers de base de
//     datos: la raíz de composición elige y registra el driver.
//
// Las especificaciones se traducen a cláusulas WHERE de SQL (incluidas subconsultas EXISTS para
// las colecciones hijas y traducciones por mapeo de las especificaciones propias). Nunca se
// filtra nada en memoria: las expresiones que no se pueden traducir fallan con
// domain.ErrUnsupported.
package sqlrepo

import (
	"fmt"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Dialect recoge todo lo que cambia de un motor SQL a otro.
type Dialect interface {
	// Name identifica el motor ("sqlite", "postgres", "sqlserver", "oracle", "mysql").
	Name() string
	// Placeholder devuelve el marcador de parámetro del argumento n-ésimo (empezando en 1).
	Placeholder(n int) string
	// Quote entrecomilla un identificador (nombre de tabla o de columna).
	Quote(ident string) string
	// LimitOffset devuelve la cláusula de paginación que se añade tras ORDER BY.
	LimitOffset(limit, offset int) string
	// LikeEscape devuelve la cláusula ESCAPE para patrones LIKE escapados con barra invertida.
	LikeEscape() string
	// MaxInList es el número máximo de elementos de una lista IN (...).
	MaxInList() int
	// BoolValue convierte un booleano a su representación en el driver.
	BoolValue(b bool) any
	// UUIDValue convierte un UUID a su representación en el driver.
	UUIDValue(u domain.UUID) any
	// ParseUUID convierte el valor leído de una columna en un UUID.
	ParseUUID(v any) (domain.UUID, error)
	// TimeValue convierte un instante a su representación en el driver (UTC).
	TimeValue(t time.Time) any
	// DateValue convierte una fecha civil (sin hora ni zona) a su representación en el driver.
	DateValue(year int, month time.Month, day int) any
	// DateExpr envuelve el marcador de una fecha civil cuando el motor necesita una conversión
	// explícita (Oracle: TO_DATE(:n, 'YYYY-MM-DD')); la mayoría de motores lo devuelven tal cual.
	DateExpr(placeholder string) string
	// IsUniqueViolation dice si err es una violación de clave primaria o de restricción única.
	IsUniqueViolation(err error) bool
}

// ParseUUIDDefault interpreta las representaciones habituales de un UUID leído: cadenas
// canónicas, 16 bytes en bruto (orden de bytes RFC) y slices de bytes con texto.
func ParseUUIDDefault(v any) (domain.UUID, error) {
	switch x := v.(type) {
	case nil:
		return domain.UUID{}, nil
	case domain.UUID:
		return x, nil
	case [16]byte:
		return domain.UUID(x), nil
	case string:
		return domain.ParseUUID(x)
	case []byte:
		if len(x) == 16 {
			return domain.UUIDFromBytes(x)
		}
		return domain.ParseUUID(string(x))
	default:
		return domain.UUID{}, fmt.Errorf("%w: cannot convert %T to uuid", domain.ErrUnsupported, v)
	}
}

// SwapGUIDBytes convierte entre el orden de bytes RFC y el orden mixto que usan los GUID de
// Microsoft (uniqueidentifier de SQL Server, Guid.ToByteArray de .NET, EF Core sobre RAW(16) de
// Oracle). La transformación es su propia inversa.
func SwapGUIDBytes(b [16]byte) [16]byte {
	b[0], b[1], b[2], b[3] = b[3], b[2], b[1], b[0]
	b[4], b[5] = b[5], b[4]
	b[6], b[7] = b[7], b[6]
	return b
}

// EscapeLike escapa con barra invertida los comodines de LIKE (% y _), el corchete de SQL Server
// y el propio carácter de escape; se usa junto con Dialect.LikeEscape.
func EscapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`, `[`, `\[`)
	return r.Replace(s)
}

// ErrorContains dice si err (o su texto) contiene alguno de los fragmentos.
// Los dialectos lo usan para clasificar errores del driver sin importar sus paquetes.
func ErrorContains(err error, fragments ...string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, f := range fragments {
		if strings.Contains(msg, f) {
			return true
		}
	}
	return false
}

package sqlrepo

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/shopspring/decimal"
)

// toDriver convierte un valor del dominio (identificadores tipados, primitivos con nombre,
// booleanos, instantes, decimales) en un valor que acepta el driver del dialecto d.
func toDriver(d Dialect, v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case decimal.Decimal:
		// Siempre texto con punto, nunca float64: Dialect.DecimalExpr lo convierte a número.
		return x.String(), nil
	case *decimal.Decimal:
		if x == nil {
			return nil, nil
		}
		return x.String(), nil
	case domain.UUIDBacked:
		return d.UUIDValue(x.BaseUUID()), nil
	case domain.LongBacked:
		return x.BaseLong(), nil
	case domain.DateBacked:
		return d.DateValue(x.BaseDate()), nil
	case bool:
		return d.BoolValue(x), nil
	case time.Time:
		return d.TimeValue(x), nil
	case *time.Time:
		if x == nil {
			return nil, nil
		}
		return d.TimeValue(*x), nil
	case driver.Valuer:
		return x, nil
	case string, int64, float64, []byte:
		return x, nil
	case int:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint8:
		return int64(x), nil
	case float32:
		return float64(x), nil
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, nil
		}
		return toDriver(d, rv.Elem().Interface())
	case reflect.String:
		return rv.String(), nil
	case reflect.Bool:
		return d.BoolValue(rv.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := rv.Uint()
		if u > 1<<63-1 {
			return nil, fmt.Errorf("%w: %T value %d overflows int64", domain.ErrUnsupported, v, u)
		}
		return int64(u), nil
	case reflect.Float32, reflect.Float64:
		return rv.Float(), nil
	}
	return nil, fmt.Errorf("%w: cannot convert %T to a database value; expose a primitive in the field accessor or implement driver.Valuer", domain.ErrUnsupported, v)
}

// Límites de un decimal enlazado: los 38 dígitos de DECIMAL en SQL Server y NUMBER en Oracle, y
// los 30 decimales de DECIMAL en MySQL.
const (
	maxBoundPrecision = 38
	maxBoundScale     = 30
)

// decimalShape devuelve la precisión (cifras) y la escala (decimales) de v escrito como texto
// canónico ("-0.005" es 4,3), que es lo que necesita Dialect.DecimalExpr.
func decimalShape(v decimal.Decimal) (precision, scale int, err error) {
	s := strings.TrimPrefix(v.String(), "-")
	intPart, frac, _ := strings.Cut(s, ".")
	precision, scale = len(intPart)+len(frac), len(frac)
	if precision > maxBoundPrecision || scale > maxBoundScale {
		return 0, 0, fmt.Errorf("%w: decimal %s has more than %d digits or %d decimals; round it first",
			domain.ErrUnsupported, v.String(), maxBoundPrecision, maxBoundScale)
	}
	return precision, scale, nil
}

// integerDigits devuelve las cifras enteras significativas de v (0 para |v| < 1).
func integerDigits(v decimal.Decimal) int {
	intPart, _, _ := strings.Cut(strings.TrimPrefix(v.String(), "-"), ".")
	return len(strings.TrimLeft(intPart, "0"))
}

// ErrDecimalDoesNotFit es el error de un decimal que no cabe en su columna {dec:p,s}: más decimales
// que la escala o más cifras enteras que p-s. No envuelve domain.ErrValidation a propósito: cada
// dominio valida la escala de sus valores antes de guardarlos, así que un valor que llega aquí sin
// caber es un fallo del programa (un mapeo que declara menos escala de la que produce su dominio),
// no un dato del usuario. Sale como error interno (500, sin detalle), y no como un 400 que le
// enseñaría al usuario el nombre de la tabla y de la columna. AlterColumnToDecimal lo devuelve
// también para un valor guardado que no cabe.
var ErrDecimalDoesNotFit = errors.New("sqlrepo: decimal does not fit its column")

// fits dice si v cabe en la columna sin redondear: como mucho Scale decimales y
// Precision-Scale cifras enteras.
func (c DecimalColumn) fits(v decimal.Decimal) error {
	_, frac, _ := strings.Cut(v.String(), ".")
	if len(frac) > c.Scale || integerDigits(v) > c.Precision-c.Scale {
		return fmt.Errorf("%w: decimal %s does not fit in %s; round it in the domain first", ErrDecimalDoesNotFit, v.String(), c)
	}
	return nil
}

// plainDecimal es la forma de un decimal escrito con punto, sin espacios, signo más ni exponente.
var plainDecimal = regexp.MustCompile(`^-?([0-9]+(\.[0-9]+)?|\.[0-9]+)$`)

// canonical valida el texto de un decimal guardado en una columna de texto y devuelve su forma
// canónica ("002.50" es "2.5"), o un error si no es un decimal con punto que quepa en c.
func (c DecimalColumn) canonical(s string) (string, error) {
	if !plainDecimal.MatchString(s) {
		return "", fmt.Errorf("%w: %q is not a plain decimal number", domain.ErrValidation, s)
	}
	v, err := decimal.NewFromString(s)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %v", domain.ErrValidation, s, err)
	}
	if err := c.fits(v); err != nil {
		return "", err
	}
	return v.String(), nil
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int32:
		return int64(x), nil
	case int:
		return int64(x), nil
	case float64:
		return int64(x), nil
	case float32:
		return int64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case []byte:
		return parseIntString(string(x))
	case string:
		return parseIntString(x)
	case nil:
		return 0, nil
	}
	return 0, fmt.Errorf("cannot convert %T to int64", v)
}

func parseIntString(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot convert %q to int64", s)
	}
	return int64(f), nil
}

package sqlrepo

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/shopspring/decimal"
)

// Row da acceso tipado, independiente del dialecto, a una fila leída. Las conversiones son
// tolerantes (p. ej. booleanos guardados como NUMBER(1), UUID guardados como RAW(16), CHAR(36) o
// UNIQUEIDENTIFIER), así que una misma función Hydrate funciona en todos los motores. El primer
// error de conversión se guarda y lo devuelve Err; el repositorio lo comprueba tras Hydrate.
type Row struct {
	d     Dialect
	index map[string]int
	vals  []any
	err   error
}

func newRow(d Dialect, cols []string, vals []any) *Row {
	idx := make(map[string]int, len(cols))
	for i, c := range cols {
		idx[c] = i
	}
	return &Row{d: d, index: idx, vals: vals}
}

func (r *Row) fail(col string, err error) {
	if r.err == nil {
		r.err = fmt.Errorf("column %q: %w", col, err)
	}
}

// Err devuelve el primer error de conversión.
func (r *Row) Err() error { return r.err }

// Raw devuelve el valor tal como lo entrega el driver.
func (r *Row) Raw(col string) any {
	i, ok := r.index[col]
	if !ok {
		r.fail(col, fmt.Errorf("not selected"))
		return nil
	}
	return r.vals[i]
}

// IsNull dice si la columna es NULL.
func (r *Row) IsNull(col string) bool { return r.Raw(col) == nil }

// String devuelve una columna de texto ("" si es NULL).
func (r *Row) String(col string) string {
	switch x := r.Raw(col).(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(x)
	}
}

// NullString devuelve nil si es NULL.
func (r *Row) NullString(col string) *string {
	if r.IsNull(col) {
		return nil
	}
	s := r.String(col)
	return &s
}

// Int64 devuelve una columna entera (0 si es NULL).
func (r *Row) Int64(col string) int64 {
	n, err := toInt64(r.Raw(col))
	if err != nil {
		r.fail(col, err)
	}
	return n
}

// NullInt64 devuelve nil si es NULL.
func (r *Row) NullInt64(col string) *int64 {
	if r.IsNull(col) {
		return nil
	}
	n := r.Int64(col)
	return &n
}

// Float64 devuelve una columna numérica (0 si es NULL).
func (r *Row) Float64(col string) float64 {
	switch x := r.Raw(col).(type) {
	case nil:
		return 0
	case float64:
		return x
	case float32:
		return float64(x)
	case int64:
		return float64(x)
	case []byte, string:
		f, err := strconv.ParseFloat(strings.TrimSpace(stringOf(x)), 64)
		if err != nil {
			r.fail(col, err)
		}
		return f
	default:
		r.fail(col, fmt.Errorf("cannot convert %T to float64", x))
		return 0
	}
}

// Bool devuelve una columna booleana; acepta booleanos nativos, números y texto "1"/"true"/"Y".
func (r *Row) Bool(col string) bool {
	switch x := r.Raw(col).(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []byte, string:
		switch strings.ToLower(strings.TrimSpace(stringOf(x))) {
		case "1", "true", "t", "y", "yes":
			return true
		case "0", "false", "f", "n", "no", "":
			return false
		}
		r.fail(col, fmt.Errorf("cannot convert %q to bool", stringOf(x)))
		return false
	default:
		r.fail(col, fmt.Errorf("cannot convert %T to bool", x))
		return false
	}
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02",
}

// Time devuelve una columna de instante en UTC (el tiempo cero si es NULL).
func (r *Row) Time(col string) time.Time {
	switch x := r.Raw(col).(type) {
	case nil:
		return time.Time{}
	case time.Time:
		return x.UTC()
	case []byte, string:
		s := strings.TrimSpace(stringOf(x))
		for _, layout := range timeLayouts {
			if t, err := time.Parse(layout, s); err == nil {
				return t.UTC()
			}
		}
		r.fail(col, fmt.Errorf("cannot parse time %q", s))
		return time.Time{}
	default:
		r.fail(col, fmt.Errorf("cannot convert %T to time", x))
		return time.Time{}
	}
}

// NullTime devuelve nil si es NULL.
func (r *Row) NullTime(col string) *time.Time {
	if r.IsNull(col) {
		return nil
	}
	t := r.Time(col)
	return &t
}

// UUID devuelve una columna UUID con la representación del dialecto (el UUID cero si es NULL).
func (r *Row) UUID(col string) domain.UUID {
	u, err := r.d.ParseUUID(r.Raw(col))
	if err != nil {
		r.fail(col, err)
	}
	return u
}

// Bytes devuelve una columna binaria (nil si es NULL).
func (r *Row) Bytes(col string) []byte {
	switch x := r.Raw(col).(type) {
	case nil:
		return nil
	case []byte:
		return append([]byte(nil), x...)
	case string:
		return []byte(x)
	default:
		r.fail(col, fmt.Errorf("cannot convert %T to bytes", x))
		return nil
	}
}

func stringOf(v any) string {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	s, _ := v.(string)
	return s
}

// Decimal devuelve una columna numérica exacta (cero si es NULL). Los drivers entregan
// NUMERIC/DECIMAL como texto, bytes o, en algunos motores, float64/int64; el texto se interpreta
// de forma exacta.
func (r *Row) Decimal(col string) vocab.Decimal {
	switch x := r.Raw(col).(type) {
	case nil:
		return decimal.Zero
	case string, []byte:
		d, err := decimal.NewFromString(strings.TrimSpace(stringOf(x)))
		if err != nil {
			r.fail(col, err)
		}
		return d
	case int64:
		return decimal.NewFromInt(x)
	case float64:
		return decimal.NewFromFloat(x)
	case float32:
		return decimal.NewFromFloat32(x)
	default:
		r.fail(col, fmt.Errorf("cannot convert %T to decimal", x))
		return decimal.Zero
	}
}

// Date devuelve una columna de fecha civil (el Date cero si es NULL). Los valores DATE se leen en
// la zona que indica el driver, sin convertir a UTC, para que el día del calendario nunca se
// desplace.
func (r *Row) Date(col string) vocab.Date {
	switch x := r.Raw(col).(type) {
	case nil:
		return vocab.Date{}
	case time.Time:
		return vocab.DateOf(x)
	case []byte, string:
		s := strings.TrimSpace(stringOf(x))
		if len(s) >= 10 {
			if d, err := vocab.ParseDate(s[:10]); err == nil {
				return d
			}
		}
		r.fail(col, fmt.Errorf("cannot parse date %q", s))
		return vocab.Date{}
	default:
		r.fail(col, fmt.Errorf("cannot convert %T to date", x))
		return vocab.Date{}
	}
}

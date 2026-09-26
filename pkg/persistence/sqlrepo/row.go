package sqlrepo

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// Row gives dialect-independent, typed access to a scanned row. Conversions are lenient
// (e.g. booleans stored as NUMBER(1), UUIDs stored as RAW(16), CHAR(36) or UNIQUEIDENTIFIER),
// so one Hydrate function works on every engine. The first conversion error is kept and
// reported by Err; the repository checks it after Hydrate.
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

// Err returns the first conversion error.
func (r *Row) Err() error { return r.err }

// Raw returns the value as delivered by the driver.
func (r *Row) Raw(col string) any {
	i, ok := r.index[col]
	if !ok {
		r.fail(col, fmt.Errorf("not selected"))
		return nil
	}
	return r.vals[i]
}

// IsNull reports whether the column is NULL.
func (r *Row) IsNull(col string) bool { return r.Raw(col) == nil }

// String returns a text column ("" for NULL).
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

// NullString returns nil for NULL.
func (r *Row) NullString(col string) *string {
	if r.IsNull(col) {
		return nil
	}
	s := r.String(col)
	return &s
}

// Int64 returns an integer column (0 for NULL).
func (r *Row) Int64(col string) int64 {
	n, err := toInt64(r.Raw(col))
	if err != nil {
		r.fail(col, err)
	}
	return n
}

// NullInt64 returns nil for NULL.
func (r *Row) NullInt64(col string) *int64 {
	if r.IsNull(col) {
		return nil
	}
	n := r.Int64(col)
	return &n
}

// Float64 returns a numeric column (0 for NULL).
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

// Bool returns a boolean column; accepts native booleans, numbers and "1"/"true"/"Y" text.
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

// Time returns an instant column in UTC (zero time for NULL).
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

// NullTime returns nil for NULL.
func (r *Row) NullTime(col string) *time.Time {
	if r.IsNull(col) {
		return nil
	}
	t := r.Time(col)
	return &t
}

// UUID returns a UUID column using the dialect's representation (zero UUID for NULL).
func (r *Row) UUID(col string) domain.UUID {
	u, err := r.d.ParseUUID(r.Raw(col))
	if err != nil {
		r.fail(col, err)
	}
	return u
}

// Bytes returns a binary column (nil for NULL).
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

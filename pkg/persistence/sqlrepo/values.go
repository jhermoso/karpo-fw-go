package sqlrepo

import (
	"database/sql/driver"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
)

// toDriver converts a domain value (typed identifiers, named primitives, bools, times) into a
// value the driver of dialect d accepts.
func toDriver(d Dialect, v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case domain.UUIDBacked:
		return d.UUIDValue(x.BaseUUID()), nil
	case domain.LongBacked:
		return x.BaseLong(), nil
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

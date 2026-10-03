package vocab

import (
	"strings"

	"github.com/shopspring/decimal"
)

// Decimal is an arbitrary-precision decimal number (the Go counterpart of C# decimal).
// Compare with Equal/Cmp, never with ==.
type Decimal = decimal.Decimal

// ParseDecimal parses a decimal written with '.' as the decimal separator ("1234.56").
func ParseDecimal(s string) (Decimal, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return Decimal{}, invalid("", "decimal", "invalid decimal number "+quote(s))
	}
	return d, nil
}

// MustDecimal is like ParseDecimal but panics on error (constants and tests).
func MustDecimal(s string) Decimal {
	d, err := ParseDecimal(s)
	if err != nil {
		panic(err)
	}
	return d
}

// DecimalFromInt returns n as a Decimal.
func DecimalFromInt(n int64) Decimal { return decimal.NewFromInt(n) }

// CompareDecimal compares two decimals (for spec.OrderedBy).
func CompareDecimal(a, b Decimal) int { return a.Cmp(b) }

func quote(s string) string { return "\"" + s + "\"" }

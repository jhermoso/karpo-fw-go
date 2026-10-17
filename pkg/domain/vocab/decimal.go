package vocab

import (
	"strings"

	"github.com/shopspring/decimal"
)

// Decimal es un número decimal de precisión arbitraria (la contrapartida en Go del decimal de C#).
// Se compara con Equal/Cmp, nunca con ==.
type Decimal = decimal.Decimal

// ParseDecimal analiza un decimal escrito con '.' como separador decimal ("1234.56").
func ParseDecimal(s string) (Decimal, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return Decimal{}, invalid("", "decimal", "invalid decimal number "+quote(s))
	}
	return d, nil
}

// MustDecimal es como ParseDecimal pero entra en pánico si hay error (constantes y pruebas).
func MustDecimal(s string) Decimal {
	d, err := ParseDecimal(s)
	if err != nil {
		panic(err)
	}
	return d
}

// DecimalFromInt devuelve n como Decimal.
func DecimalFromInt(n int64) Decimal { return decimal.NewFromInt(n) }

// CompareDecimal compara dos decimales (para spec.OrderedBy).
func CompareDecimal(a, b Decimal) int { return a.Cmp(b) }

func quote(s string) string { return "\"" + s + "\"" }

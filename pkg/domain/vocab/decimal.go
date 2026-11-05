package vocab

import (
	"regexp"
	"strings"

	"github.com/shopspring/decimal"
)

// Decimal es un número decimal de precisión arbitraria (la contrapartida en Go del decimal de C#).
// Se compara con Equal/Cmp, nunca con ==.
type Decimal = decimal.Decimal

// plainDecimal es la única forma que se admite: sin exponente. Con exponente, "1e100000000" cabe
// en una petición y obliga a cada comparación a construir un entero de cien millones de cifras.
var plainDecimal = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)$`)

// ParseDecimal analiza un decimal escrito con '.' como separador decimal ("1234.56"), sin
// exponente.
func ParseDecimal(s string) (Decimal, error) {
	s = strings.TrimSpace(s)
	if !plainDecimal.MatchString(s) {
		return Decimal{}, invalid("", "decimal", "invalid decimal number "+quote(s))
	}
	d, err := decimal.NewFromString(s)
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

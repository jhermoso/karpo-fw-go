package vocab

import "github.com/shopspring/decimal"

// Percentage es un porcentaje expresado en puntos: 21 significa 21 %. A diferencia de la versión de
// C#, no se limita a 0–100 (recargos, variaciones y ajustes negativos son porcentajes válidos) y la
// aritmética nunca recorta en silencio; Within sirve para imponer un rango de negocio.
type Percentage struct{ points Decimal }

var hundred = decimal.NewFromInt(100)

// NewPercentage crea un porcentaje a partir de puntos (21 → 21 %).
func NewPercentage(points Decimal) Percentage { return Percentage{points: points} }

// PercentageFromFraction crea un porcentaje a partir de una fracción (0.21 → 21 %).
func PercentageFromFraction(f Decimal) Percentage { return Percentage{points: f.Mul(hundred)} }

// MustPercentage analiza points y entra en pánico si hay error.
func MustPercentage(points string) Percentage { return NewPercentage(MustDecimal(points)) }

// Points devuelve el valor en puntos (21 para 21 %).
func (p Percentage) Points() Decimal { return p.points }

// Fraction devuelve el valor como fracción (0.21 para 21 %).
func (p Percentage) Fraction() Decimal { return p.points.Div(hundred) }

// Within dice si lo <= p <= hi (en puntos).
func (p Percentage) Within(lo, hi Decimal) bool {
	return p.points.GreaterThanOrEqual(lo) && p.points.LessThanOrEqual(hi)
}

// Of devuelve el porcentaje de un importe (con toda la precisión; se redondea con Money.Round).
func (p Percentage) Of(m Money) Money { return m.Mul(p.Fraction()) }

// AddTo devuelve m aumentado en el porcentaje (neto → bruto).
func (p Percentage) AddTo(m Money) Money { return m.Mul(decimal.NewFromInt(1).Add(p.Fraction())) }

// Equal compara numéricamente.
func (p Percentage) Equal(o Percentage) bool { return p.points.Equal(o.points) }

// String devuelve "21%".
func (p Percentage) String() string { return p.points.String() + "%" }

// MarshalText escribe los puntos ("21.5").
func (p Percentage) MarshalText() ([]byte, error) { return []byte(p.points.String()), nil }

// UnmarshalText analiza los puntos.
func (p *Percentage) UnmarshalText(b []byte) error {
	d, err := ParseDecimal(string(b))
	if err == nil {
		p.points = d
	}
	return err
}

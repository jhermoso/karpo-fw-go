package vocab

import "github.com/shopspring/decimal"

// Percentage is a percentage expressed in points: 21 means 21 %. Unlike the C# version it is not
// clamped to 0–100 (surcharges, variations and negative adjustments are valid percentages) and
// arithmetic never clips silently; use Within to enforce a business range.
type Percentage struct{ points Decimal }

var hundred = decimal.NewFromInt(100)

// NewPercentage creates a percentage from points (21 → 21 %).
func NewPercentage(points Decimal) Percentage { return Percentage{points: points} }

// PercentageFromFraction creates a percentage from a fraction (0.21 → 21 %).
func PercentageFromFraction(f Decimal) Percentage { return Percentage{points: f.Mul(hundred)} }

// MustPercentage parses points, panicking on error.
func MustPercentage(points string) Percentage { return NewPercentage(MustDecimal(points)) }

// Points returns the value in points (21 for 21 %).
func (p Percentage) Points() Decimal { return p.points }

// Fraction returns the value as a fraction (0.21 for 21 %).
func (p Percentage) Fraction() Decimal { return p.points.Div(hundred) }

// Within reports whether lo <= p <= hi (points).
func (p Percentage) Within(lo, hi Decimal) bool {
	return p.points.GreaterThanOrEqual(lo) && p.points.LessThanOrEqual(hi)
}

// Of returns the percentage of an amount (full precision; round with Money.Round).
func (p Percentage) Of(m Money) Money { return m.Mul(p.Fraction()) }

// AddTo returns m increased by the percentage (net → gross).
func (p Percentage) AddTo(m Money) Money { return m.Mul(decimal.NewFromInt(1).Add(p.Fraction())) }

// Equal compares numerically.
func (p Percentage) Equal(o Percentage) bool { return p.points.Equal(o.points) }

// String returns "21%".
func (p Percentage) String() string { return p.points.String() + "%" }

// MarshalText renders the points ("21.5").
func (p Percentage) MarshalText() ([]byte, error) { return []byte(p.points.String()), nil }

// UnmarshalText parses the points.
func (p *Percentage) UnmarshalText(b []byte) error {
	d, err := ParseDecimal(string(b))
	if err == nil {
		p.points = d
	}
	return err
}

package vocab

import (
	"encoding/json"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/shopspring/decimal"
)

// Money es un importe en una moneda. La aritmética entre monedas distintas falla con una
// violación de regla en lugar de convertir en silencio. El importe conserva toda la precisión
// (precios unitarios, tipos); Round lo ajusta a las unidades menores de la moneda.
type Money struct {
	amount   Decimal
	currency CurrencyCode
}

// ErrCurrencyMismatch es el código de violación de regla que se devuelve al mezclar monedas.
const ErrCurrencyMismatch = "money.currency_mismatch"

// NewMoney crea un importe en currency.
func NewMoney(amount Decimal, currency CurrencyCode) (Money, error) {
	if currency.IsZero() {
		return Money{}, invalid("currency", "required", "currency is required")
	}
	return Money{amount: amount, currency: currency}, nil
}

// MustMoney analiza amount (p. ej. "12.50") y currency, y entra en pánico si hay error.
func MustMoney(amount, currency string) Money {
	m, err := NewMoney(MustDecimal(amount), MustCurrencyCode(currency))
	if err != nil {
		panic(err)
	}
	return m
}

// Zero devuelve cero en currency.
func Zero(currency CurrencyCode) Money { return Money{amount: decimal.Zero, currency: currency} }

// Amount devuelve el importe.
func (m Money) Amount() Decimal { return m.amount }

// Currency devuelve la moneda.
func (m Money) Currency() CurrencyCode { return m.currency }

// IsZero dice si falta el valor monetario (sin moneda).
func (m Money) IsZero() bool { return m.currency.IsZero() }

// IsZeroAmount dice si el importe es cero.
func (m Money) IsZeroAmount() bool { return m.amount.IsZero() }

// IsNegative dice si el importe es menor que cero.
func (m Money) IsNegative() bool { return m.amount.IsNegative() }

func (m Money) same(o Money) error {
	if m.currency != o.currency {
		return domain.Violation(ErrCurrencyMismatch, "cannot combine "+m.currency.String()+" and "+o.currency.String())
	}
	return nil
}

// Add devuelve m + o (misma moneda).
func (m Money) Add(o Money) (Money, error) {
	if err := m.same(o); err != nil {
		return Money{}, err
	}
	return Money{amount: m.amount.Add(o.amount), currency: m.currency}, nil
}

// Sub devuelve m - o (misma moneda).
func (m Money) Sub(o Money) (Money, error) {
	if err := m.same(o); err != nil {
		return Money{}, err
	}
	return Money{amount: m.amount.Sub(o.amount), currency: m.currency}, nil
}

// Mul devuelve m * factor (cantidades, tipos). El resultado conserva toda la precisión.
func (m Money) Mul(factor Decimal) Money {
	return Money{amount: m.amount.Mul(factor), currency: m.currency}
}

// Neg devuelve -m.
func (m Money) Neg() Money { return Money{amount: m.amount.Neg(), currency: m.currency} }

// Round redondea el importe a las unidades menores de la moneda, con el medio alejándose de cero.
func (m Money) Round() Money {
	return Money{amount: m.amount.Round(m.currency.MinorUnits()), currency: m.currency}
}

// Cmp compara dos importes de la misma moneda (-1, 0, 1).
func (m Money) Cmp(o Money) (int, error) {
	if err := m.same(o); err != nil {
		return 0, err
	}
	return m.amount.Cmp(o.amount), nil
}

// Equal dice si ambos tienen la misma moneda e importes numéricamente iguales.
func (m Money) Equal(o Money) bool { return m.currency == o.currency && m.amount.Equal(o.amount) }

// Allocate reparte el importe redondeado según ratios sin perder ni crear unidades menores:
// el resto se distribuye de una unidad menor en una, empezando por las primeras partes
// (plazos, reparto de costes...). Los ratios no pueden ser negativos ni todos cero.
func (m Money) Allocate(ratios ...int64) ([]Money, error) {
	var total int64
	for _, r := range ratios {
		if r < 0 {
			return nil, invalid("ratios", "range", "ratios must not be negative")
		}
		total += r
	}
	if len(ratios) == 0 || total == 0 {
		return nil, invalid("ratios", "required", "at least one positive ratio is required")
	}
	unit := decimal.New(1, -m.currency.MinorUnits())
	rounded := m.Round().amount
	units := rounded.Div(unit).IntPart() // importe en unidades menores
	shares := make([]Money, len(ratios))
	var allocated int64
	for i, r := range ratios {
		n := units * r / total
		allocated += n
		shares[i] = Money{amount: unit.Mul(decimal.NewFromInt(n)), currency: m.currency}
	}
	step := int64(1)
	if units < 0 {
		step = -1
	}
	for i := 0; allocated != units; i = (i + 1) % len(shares) {
		if ratios[i] == 0 {
			continue
		}
		shares[i].amount = shares[i].amount.Add(unit.Mul(decimal.NewFromInt(step)))
		allocated += step
	}
	return shares, nil
}

// String devuelve "12.50 EUR".
func (m Money) String() string {
	if m.IsZero() {
		return ""
	}
	return m.amount.StringFixed(m.currency.MinorUnits()) + " " + m.currency.String()
}

type moneyJSON struct {
	Amount   Decimal      `json:"amount"`
	Currency CurrencyCode `json:"currency"`
}

// MarshalJSON escribe {"amount":"12.50","currency":"EUR"}.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(moneyJSON{Amount: m.amount, Currency: m.currency})
}

// UnmarshalJSON analiza {"amount":"12.50","currency":"EUR"}.
func (m *Money) UnmarshalJSON(b []byte) error {
	var j moneyJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	v, err := NewMoney(j.Amount, j.Currency)
	if err == nil {
		*m = v
	}
	return err
}

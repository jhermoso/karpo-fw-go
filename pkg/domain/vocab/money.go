package vocab

import (
	"encoding/json"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/shopspring/decimal"
)

// Money is an amount in a currency. Arithmetic between different currencies fails with a
// rule violation instead of converting silently. The amount keeps full precision (unit prices,
// rates); use Round to settle it to the currency's minor units.
type Money struct {
	amount   Decimal
	currency CurrencyCode
}

// ErrCurrencyMismatch is the rule-violation code returned when mixing currencies.
const ErrCurrencyMismatch = "money.currency_mismatch"

// NewMoney creates an amount in currency.
func NewMoney(amount Decimal, currency CurrencyCode) (Money, error) {
	if currency.IsZero() {
		return Money{}, invalid("currency", "required", "currency is required")
	}
	return Money{amount: amount, currency: currency}, nil
}

// MustMoney parses amount (e.g. "12.50") and currency, panicking on error.
func MustMoney(amount, currency string) Money {
	m, err := NewMoney(MustDecimal(amount), MustCurrencyCode(currency))
	if err != nil {
		panic(err)
	}
	return m
}

// Zero returns zero in currency.
func Zero(currency CurrencyCode) Money { return Money{amount: decimal.Zero, currency: currency} }

// Amount returns the amount.
func (m Money) Amount() Decimal { return m.amount }

// Currency returns the currency.
func (m Money) Currency() CurrencyCode { return m.currency }

// IsZero reports whether the money value is absent (no currency).
func (m Money) IsZero() bool { return m.currency.IsZero() }

// IsZeroAmount reports whether the amount is zero.
func (m Money) IsZeroAmount() bool { return m.amount.IsZero() }

// IsNegative reports whether the amount is below zero.
func (m Money) IsNegative() bool { return m.amount.IsNegative() }

func (m Money) same(o Money) error {
	if m.currency != o.currency {
		return domain.Violation(ErrCurrencyMismatch, "cannot combine "+m.currency.String()+" and "+o.currency.String())
	}
	return nil
}

// Add returns m + o (same currency).
func (m Money) Add(o Money) (Money, error) {
	if err := m.same(o); err != nil {
		return Money{}, err
	}
	return Money{amount: m.amount.Add(o.amount), currency: m.currency}, nil
}

// Sub returns m - o (same currency).
func (m Money) Sub(o Money) (Money, error) {
	if err := m.same(o); err != nil {
		return Money{}, err
	}
	return Money{amount: m.amount.Sub(o.amount), currency: m.currency}, nil
}

// Mul returns m * factor (quantities, rates). The result keeps full precision.
func (m Money) Mul(factor Decimal) Money {
	return Money{amount: m.amount.Mul(factor), currency: m.currency}
}

// Neg returns -m.
func (m Money) Neg() Money { return Money{amount: m.amount.Neg(), currency: m.currency} }

// Round rounds the amount to the currency's minor units, half away from zero.
func (m Money) Round() Money {
	return Money{amount: m.amount.Round(m.currency.MinorUnits()), currency: m.currency}
}

// Cmp compares two amounts of the same currency (-1, 0, 1).
func (m Money) Cmp(o Money) (int, error) {
	if err := m.same(o); err != nil {
		return 0, err
	}
	return m.amount.Cmp(o.amount), nil
}

// Equal reports whether both have the same currency and numerically equal amounts.
func (m Money) Equal(o Money) bool { return m.currency == o.currency && m.amount.Equal(o.amount) }

// Allocate splits the rounded amount among ratios without losing or creating minor units:
// the remainder is distributed one minor unit at a time, starting with the first shares
// (installments, cost distribution...). Ratios must be non-negative and not all zero.
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
	units := rounded.Div(unit).IntPart() // amount in minor units
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

// String returns "12.50 EUR".
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

// MarshalJSON renders {"amount":"12.50","currency":"EUR"}.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(moneyJSON{Amount: m.amount, Currency: m.currency})
}

// UnmarshalJSON parses {"amount":"12.50","currency":"EUR"}.
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

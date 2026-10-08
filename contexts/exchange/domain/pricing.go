// Package domain is the Exchange model: the currency-exchange bureau of a company. What it sells
// foreign cash for (a reference rate, a margin per customer segment, and the smallest note it
// hands over), and the reservations customers make to collect that cash at an office. In C# the
// reservation trusted the rate the client sent, nothing ever made a reservation expire, and the
// rounding to notes was applied by the screen only.
package domain

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Aggregate type names.
const (
	CurrencyKind    = "exchange.currency"
	MarginKindName  = "exchange.margin"
	SettingsKind    = "exchange.settings"
	ReservationKind = "exchange.reservation"
)

// Identities of the context.
type (
	// CurrencyID identifies a currency a company deals in.
	CurrencyID struct{ fw.UUID }
	// MarginID identifies the margin of a currency for a segment.
	MarginID struct{ fw.UUID }
	// SettingsID identifies the settings of a company.
	SettingsID struct{ fw.UUID }
	// ReservationID identifies a reservation.
	ReservationID struct{ fw.UUID }
	// OrganizationID is the company, an internal organization of Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID is a customer or a collaborator.
	PartyID struct{ fw.UUID }
)

func zero() vocab.Decimal { return vocab.DecimalFromInt(0) }

func places(d vocab.Decimal, n int32) bool { return d.Equal(d.Round(n)) }

// NormalizeCode trims and uppercases a currency or segment code.
func NormalizeCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func validCode(s string, max int) bool {
	if s == "" || len(s) > max {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// CurrencyState is the persisted state of a currency of a company.
type CurrencyState struct {
	Company OrganizationID
	Code    string
	Name    string
	Rate    vocab.Decimal // reference rate: euros for one unit, six decimals; zero while unknown
	RateOn  vocab.Date    // the day of the reference rate
	Facial  vocab.Decimal // smallest note handed over; zero: any amount
	Crypto  bool
	Blocked bool
	Audit   traits.AuditStamp
}

// Currency is a currency a company exchanges, with its reference rate (in C# the rate came from an
// HTTP service that answered zero instead of failing).
type Currency struct {
	fw.BaseAggregateRoot[CurrencyID]
	traits.Audited
	s CurrencyState
}

func checkCurrency(v *fw.Validation, s *CurrencyState) {
	s.Name = strings.TrimSpace(s.Name)
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 120, "name", "length", "a name of 1 to 120 characters")
	v.Require(!s.Rate.IsNegative() && places(s.Rate, 6), "rate", "range", "a rate that is not negative, with six decimals")
	v.Require(s.Rate.IsZero() == s.RateOn.IsZero(), "rateOn", "required", "a rate has its day")
	v.Require(!s.Facial.IsNegative() && places(s.Facial, 2), "facial", "range", "a note that is not negative, in cents")
}

// ReconstituteCurrency rebuilds a currency.
func ReconstituteCurrency(id CurrencyID, s CurrencyState) (*Currency, error) {
	base, err := fw.NewBaseAggregateRoot(CurrencyKind, id)
	if err != nil {
		return nil, err
	}
	s.Code = NormalizeCode(s.Code)
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	v.Require(validCode(s.Code, 10) && s.Code != "EUR", "code", "format", "a currency code other than EUR")
	checkCurrency(&v, &s)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Currency{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (c *Currency) State() CurrencyState { return c.s }

// Change replaces the name, the note and whether the currency is crypto or blocked.
func (c *Currency) Change(name string, facial vocab.Decimal, crypto, blocked bool) error {
	s := c.s
	s.Name, s.Facial, s.Crypto, s.Blocked = name, facial, crypto, blocked
	var v fw.Validation
	checkCurrency(&v, &s)
	if err := v.Err(); err != nil {
		return err
	}
	c.s = s
	return nil
}

// SetRate fixes the reference rate of a day: positive, and not of a day before the current one.
func (c *Currency) SetRate(rate vocab.Decimal, on vocab.Date) error {
	if !rate.IsPositive() || !places(rate, 6) || on.IsZero() {
		return fw.Violation("exchange.rate", "a positive rate with six decimals, and its day")
	}
	if !c.s.RateOn.IsZero() && on.Before(c.s.RateOn) {
		return fw.Violation("exchange.rate_date", "the rate of "+c.s.RateOn.String()+" is more recent")
	}
	c.s.Rate, c.s.RateOn = rate, on
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (c *Currency) AuditSnapshot() map[string]any {
	return map[string]any{"code": c.s.Code, "rate": c.s.Rate.String(), "facial": c.s.Facial.String(), "blocked": c.s.Blocked}
}

// MarginKind is how a margin makes the rate dearer.
type MarginKind string

// Margin kinds.
const (
	Percent  MarginKind = "percent"  // rate × (1 + v / 100)
	Absolute MarginKind = "absolute" // rate + v
	Pip      MarginKind = "pip"      // rate + v × 0.0001
)

// MarginKinds lists the valid kinds.
var MarginKinds = []MarginKind{Percent, Absolute, Pip}

var (
	hundred = vocab.DecimalFromInt(100)
	pipUnit = vocab.MustDecimal("0.0001")
)

// Apply returns the rate offered to the customer: the reference rate plus the margin, with six
// decimals.
func (k MarginKind) Apply(rate, value vocab.Decimal) vocab.Decimal {
	switch k {
	case Absolute:
		rate = rate.Add(value)
	case Pip:
		rate = rate.Add(value.Mul(pipUnit))
	default:
		rate = rate.Mul(hundred.Add(value)).Div(hundred)
	}
	return rate.Round(6)
}

// AsPercent expresses a margin as a percentage of the reference rate, for reporting.
func (k MarginKind) AsPercent(rate, value vocab.Decimal) vocab.Decimal {
	switch {
	case k == Percent:
		return value
	case rate.IsZero():
		return zero()
	case k == Pip:
		return value.Mul(pipUnit).Div(rate).Mul(hundred).Round(6)
	}
	return value.Div(rate).Mul(hundred).Round(6)
}

// Levels of price: three margins per currency and segment, of which the company applies one.
const (
	MinLevel = 1
	MaxLevel = 3
)

// MarginState is the persisted state of a margin.
type MarginState struct {
	Company  OrganizationID
	Currency string
	Segment  string
	Kind     MarginKind
	Values   [MaxLevel]vocab.Decimal // one per level of price
	Audit    traits.AuditStamp
}

// Margin is what a company adds to the reference rate of a currency for a customer segment.
type Margin struct {
	fw.BaseAggregateRoot[MarginID]
	traits.Audited
	s MarginState
}

func checkMargin(v *fw.Validation, s *MarginState) {
	v.Require(slices.Contains(MarginKinds, s.Kind), "kind", "enum", "percent, absolute or pip")
	for _, x := range s.Values {
		v.Require(!x.IsNegative() && places(x, 6), "values", "range", "margins that are not negative, with six decimals")
	}
}

// ReconstituteMargin rebuilds a margin.
func ReconstituteMargin(id MarginID, s MarginState) (*Margin, error) {
	base, err := fw.NewBaseAggregateRoot(MarginKindName, id)
	if err != nil {
		return nil, err
	}
	s.Currency, s.Segment = NormalizeCode(s.Currency), NormalizeCode(s.Segment)
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	v.Require(validCode(s.Currency, 10), "currency", "format", "a currency code")
	v.Require(validCode(s.Segment, 20), "segment", "format", "a segment code")
	checkMargin(&v, &s)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Margin{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (m *Margin) State() MarginState { return m.s }

// Change replaces the kind and the three values.
func (m *Margin) Change(kind MarginKind, values [MaxLevel]vocab.Decimal) error {
	s := m.s
	s.Kind, s.Values = kind, values
	var v fw.Validation
	checkMargin(&v, &s)
	if err := v.Err(); err != nil {
		return err
	}
	m.s = s
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (m *Margin) AuditSnapshot() map[string]any {
	return map[string]any{"currency": m.s.Currency, "segment": m.s.Segment, "kind": string(m.s.Kind), "level1": m.s.Values[0].String(),
		"level2": m.s.Values[1].String(), "level3": m.s.Values[2].String()}
}

// Promotion code modes.
const (
	PromotionDisabled = "disabled" // codes are ignored
	PromotionOptional = "optional"
	PromotionRequired = "required"
)

// Defaults of the settings (those of the C#: second level of price, optional validated codes, a
// reservation waits four hours after the agreed pickup).
const (
	DefaultLevel       = 2
	DefaultExpiryHours = 4
	DefaultSegment     = "WEB"
)

// SettingsState is the persisted state of the settings of a company.
type SettingsState struct {
	Company           OrganizationID
	Level             int    // level of price applied, 1 to 3
	PromotionMode     string // disabled, optional or required
	ValidatePromotion bool   // a code must be that of a collaborator
	ExpiryHours       int    // hours a reservation waits after its pickup time
	CryptoEnabled     bool
	Audit             traits.AuditStamp
}

// Settings are how a company runs its reservations (in C# one global row for the promotion codes,
// and application settings nobody could change at run time for the rest).
type Settings struct {
	fw.BaseAggregateRoot[SettingsID]
	traits.Audited
	s SettingsState
}

// DefaultSettings returns the settings a company has until it changes them.
func DefaultSettings(company OrganizationID) SettingsState {
	return SettingsState{Company: company, Level: DefaultLevel, PromotionMode: PromotionOptional, ValidatePromotion: true, ExpiryHours: DefaultExpiryHours}
}

// ReconstituteSettings rebuilds the settings.
func ReconstituteSettings(id SettingsID, s SettingsState) (*Settings, error) {
	base, err := fw.NewBaseAggregateRoot(SettingsKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	v.Require(s.Level >= MinLevel && s.Level <= MaxLevel, "level", "range", "a level of price from 1 to 3")
	v.Require(slices.Contains([]string{PromotionDisabled, PromotionOptional, PromotionRequired}, s.PromotionMode), "promotionMode", "enum",
		"disabled, optional or required")
	v.Require(s.ExpiryHours >= 1 && s.ExpiryHours <= 720, "expiryHours", "range", "from 1 to 720 hours")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Settings{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (s *Settings) State() SettingsState { return s.s }

// Change replaces the settings.
func (s *Settings) Change(n SettingsState) error {
	n.Company, n.Audit = s.s.Company, s.s.Audit
	checked, err := ReconstituteSettings(s.ID(), n)
	if err != nil {
		return err
	}
	s.s = checked.s
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (s *Settings) AuditSnapshot() map[string]any {
	return map[string]any{"level": s.s.Level, "promotionMode": s.s.PromotionMode, "validatePromotion": s.s.ValidatePromotion,
		"expiryHours": s.s.ExpiryHours, "cryptoEnabled": s.s.CryptoEnabled}
}

// Quotation is what a customer is offered for an amount of a currency.
type Quotation struct {
	Currency      string
	Segment       string
	Amount        vocab.Decimal // what was asked for
	BaseRate      vocab.Decimal // reference rate, euros for one unit
	RateOn        vocab.Date
	OfferedRate   vocab.Decimal // with the margin
	MarginKind    MarginKind
	Level         int
	MarginValue   vocab.Decimal
	MarginPercent vocab.Decimal
	Facial        vocab.Decimal
	Delivered     vocab.Decimal // what is handed over: the amount in whole notes
	Eur           vocab.Decimal // what the customer pays
	NoMargin      bool          // the currency has no margin for the segment: the reference rate is offered
}

// RoundDownToFacial returns the amount in whole notes. An amount under one note is left as asked,
// and so is any amount when the currency has no note.
func RoundDownToFacial(amount, facial vocab.Decimal) vocab.Decimal {
	if !facial.IsPositive() || !amount.IsPositive() {
		return amount
	}
	if r := amount.Div(facial).Floor().Mul(facial); r.IsPositive() {
		return r
	}
	return amount
}

// Quote prices an amount of a currency for a segment at the level of price of the company:
// reference rate plus margin (six decimals), the amount in whole notes, and its value in euros
// (cents, half away from zero). margin may be nil.
func Quote(c *Currency, m *Margin, set SettingsState, segment string, amount vocab.Decimal) (Quotation, error) {
	if !amount.IsPositive() || !places(amount, 2) {
		return Quotation{}, fw.Violation("exchange.amount", "a positive amount in cents")
	}
	s := c.s
	switch {
	case s.Blocked:
		return Quotation{}, fw.Violation("exchange.currency_blocked", "the currency "+s.Code+" is not exchanged")
	case s.Crypto && !set.CryptoEnabled:
		return Quotation{}, fw.Violation("exchange.crypto_disabled", "cryptocurrencies are not exchanged")
	case !s.Rate.IsPositive():
		return Quotation{}, fw.Violation("exchange.no_rate", "the currency "+s.Code+" has no rate")
	}
	q := Quotation{Currency: s.Code, Segment: NormalizeCode(segment), Amount: amount, BaseRate: s.Rate, RateOn: s.RateOn, MarginKind: Percent,
		Level: set.Level, MarginValue: zero(), Facial: s.Facial, NoMargin: m == nil}
	if m != nil {
		q.MarginKind, q.MarginValue = m.s.Kind, m.s.Values[set.Level-1]
	}
	q.OfferedRate = q.MarginKind.Apply(s.Rate, q.MarginValue)
	q.MarginPercent = q.MarginKind.AsPercent(s.Rate, q.MarginValue)
	q.Delivered = RoundDownToFacial(amount, s.Facial)
	q.Eur = q.Delivered.Mul(q.OfferedRate).Round(2)
	return q, nil
}

// Pricing fields.
var (
	CurFieldCompany = spec.Comparable("company", func(c *Currency) OrganizationID { return c.s.Company })
	CurFieldCode    = spec.Ordered("code", func(c *Currency) string { return c.s.Code })
	MrgFieldCompany = spec.Comparable("company", func(m *Margin) OrganizationID { return m.s.Company })
	MrgFieldCur     = spec.Ordered("currency", func(m *Margin) string { return m.s.Currency })
	MrgFieldSegment = spec.Ordered("segment_code", func(m *Margin) string { return m.s.Segment })
	SetFieldCompany = spec.Comparable("company", func(s *Settings) OrganizationID { return s.s.Company })
)

// Identities.
func NewCurrencyID() CurrencyID       { return CurrencyID{fw.NewUUID()} }
func NewMarginID() MarginID           { return MarginID{fw.NewUUID()} }
func NewSettingsID() SettingsID       { return SettingsID{fw.NewUUID()} }
func NewReservationID() ReservationID { return ReservationID{fw.NewUUID()} }

// ParseCurrencyID parses a textual identity.
func ParseCurrencyID(s string) (CurrencyID, error) {
	u, err := fw.ParseUUID(s)
	return CurrencyID{u}, err
}

// ParseMarginID parses a textual identity.
func ParseMarginID(s string) (MarginID, error) { u, err := fw.ParseUUID(s); return MarginID{u}, err }

// Repositories.
type (
	CurrencyRepository    = fw.Repository[CurrencyID, *Currency]
	MarginRepository      = fw.Repository[MarginID, *Margin]
	SettingsRepository    = fw.Repository[SettingsID, *Settings]
	ReservationRepository = fw.Repository[ReservationID, *Reservation]
)

// Collaborators resolves the promotion code of a collaborator (a port Exchange owns; without it no
// code can be validated).
type Collaborators interface {
	// ByPromotionCode returns the collaborator of a code and whether there is an active one.
	ByPromotionCode(ctx context.Context, company OrganizationID, code string) (PartyID, bool, error)
}

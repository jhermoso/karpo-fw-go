// Package domain es el modelo de Exchange: la casa de cambio de una empresa. Por cuánto vende el
// efectivo en divisa (un cambio de referencia, un margen por segmento de clientes, y el billete más
// pequeño que entrega), y las reservas que hacen los clientes para recoger ese efectivo en una
// oficina. En C# la reserva se fiaba del cambio que enviaba el cliente, nada hacía caducar nunca una
// reserva, y el redondeo a billetes sólo lo aplicaba la pantalla.
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

// Nombres de los tipos de agregado.
const (
	CurrencyKind    = "exchange.currency"
	MarginKindName  = "exchange.margin"
	SettingsKind    = "exchange.settings"
	ReservationKind = "exchange.reservation"
)

// Identidades del contexto.
type (
	// CurrencyID identifica una moneda con la que opera una empresa.
	CurrencyID struct{ fw.UUID }
	// MarginID identifica el margen de una moneda para un segmento.
	MarginID struct{ fw.UUID }
	// SettingsID identifica la configuración de una empresa.
	SettingsID struct{ fw.UUID }
	// ReservationID identifica una reserva.
	ReservationID struct{ fw.UUID }
	// OrganizationID es la empresa, una organización interna de Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es un cliente o un colaborador.
	PartyID struct{ fw.UUID }
)

func zero() vocab.Decimal { return vocab.DecimalFromInt(0) }

func places(d vocab.Decimal, n int32) bool { return d.Equal(d.Round(n)) }

// NormalizeCode recorta y pasa a mayúsculas un código de moneda o de segmento.
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

// CurrencyState es el estado persistido de una moneda de una empresa.
type CurrencyState struct {
	Company OrganizationID
	Code    string
	Name    string
	Rate    vocab.Decimal // cambio de referencia: euros por una unidad, seis decimales; cero mientras no se sabe
	RateOn  vocab.Date    // el día del cambio de referencia
	Facial  vocab.Decimal // billete más pequeño que se entrega; cero: cualquier importe
	Crypto  bool
	Blocked bool
	Audit   traits.AuditStamp
}

// Currency es una moneda que cambia una empresa, con su cambio de referencia (en C# el cambio venía
// de un servicio HTTP que respondía cero en vez de fallar).
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

// ReconstituteCurrency reconstruye una moneda.
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

// State devuelve el estado.
func (c *Currency) State() CurrencyState { return c.s }

// Change sustituye el nombre, el billete y si la moneda es cripto o está bloqueada.
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

// SetRate fija el cambio de referencia de un día: positivo, y no de un día anterior al vigente.
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

// AuditSnapshot implementa traits.Snapshotter.
func (c *Currency) AuditSnapshot() map[string]any {
	return map[string]any{"code": c.s.Code, "rate": c.s.Rate.String(), "facial": c.s.Facial.String(), "blocked": c.s.Blocked}
}

// MarginKind es cómo encarece un margen el cambio.
type MarginKind string

// Tipos de margen.
const (
	Percent  MarginKind = "percent"  // rate × (1 + v / 100)
	Absolute MarginKind = "absolute" // rate + v
	Pip      MarginKind = "pip"      // rate + v × 0.0001
)

// MarginKinds lista los tipos válidos.
var MarginKinds = []MarginKind{Percent, Absolute, Pip}

var (
	hundred = vocab.DecimalFromInt(100)
	pipUnit = vocab.MustDecimal("0.0001")
)

// Apply devuelve el cambio que se ofrece al cliente: el cambio de referencia más el margen, con seis
// decimales.
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

// AsPercent expresa un margen como porcentaje del cambio de referencia, para los informes.
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

// Niveles de precio: tres márgenes por moneda y segmento, de los que la empresa aplica uno.
const (
	MinLevel = 1
	MaxLevel = 3
)

// MarginState es el estado persistido de un margen.
type MarginState struct {
	Company  OrganizationID
	Currency string
	Segment  string
	Kind     MarginKind
	Values   [MaxLevel]vocab.Decimal // uno por nivel de precio
	Audit    traits.AuditStamp
}

// Margin es lo que una empresa añade al cambio de referencia de una moneda para un segmento de clientes.
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

// ReconstituteMargin reconstruye un margen.
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

// State devuelve el estado.
func (m *Margin) State() MarginState { return m.s }

// Change sustituye el tipo y los tres valores.
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

// AuditSnapshot implementa traits.Snapshotter.
func (m *Margin) AuditSnapshot() map[string]any {
	return map[string]any{"currency": m.s.Currency, "segment": m.s.Segment, "kind": string(m.s.Kind), "level1": m.s.Values[0].String(),
		"level2": m.s.Values[1].String(), "level3": m.s.Values[2].String()}
}

// Modos del código de promoción.
const (
	PromotionDisabled = "disabled" // los códigos se ignoran
	PromotionOptional = "optional"
	PromotionRequired = "required"
)

// Valores por omisión de la configuración (los del C#: segundo nivel de precio, códigos opcionales
// validados, una reserva espera cuatro horas tras la recogida acordada).
const (
	DefaultLevel       = 2
	DefaultExpiryHours = 4
	DefaultSegment     = "WEB"
)

// SettingsState es el estado persistido de la configuración de una empresa.
type SettingsState struct {
	Company           OrganizationID
	Level             int    // nivel de precio aplicado, de 1 a 3
	PromotionMode     string // disabled, optional o required
	ValidatePromotion bool   // un código debe ser el de un colaborador
	ExpiryHours       int    // horas que espera una reserva tras su hora de recogida
	CryptoEnabled     bool
	Audit             traits.AuditStamp
}

// Settings es cómo lleva una empresa sus reservas (en C#, una fila global para los códigos de
// promoción, y para el resto ajustes de aplicación que nadie podía cambiar en ejecución).
type Settings struct {
	fw.BaseAggregateRoot[SettingsID]
	traits.Audited
	s SettingsState
}

// DefaultSettings devuelve la configuración que tiene una empresa hasta que la cambia.
func DefaultSettings(company OrganizationID) SettingsState {
	return SettingsState{Company: company, Level: DefaultLevel, PromotionMode: PromotionOptional, ValidatePromotion: true, ExpiryHours: DefaultExpiryHours}
}

// ReconstituteSettings reconstruye la configuración.
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

// State devuelve el estado.
func (s *Settings) State() SettingsState { return s.s }

// Change sustituye la configuración.
func (s *Settings) Change(n SettingsState) error {
	n.Company, n.Audit = s.s.Company, s.s.Audit
	checked, err := ReconstituteSettings(s.ID(), n)
	if err != nil {
		return err
	}
	s.s = checked.s
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (s *Settings) AuditSnapshot() map[string]any {
	return map[string]any{"level": s.s.Level, "promotionMode": s.s.PromotionMode, "validatePromotion": s.s.ValidatePromotion,
		"expiryHours": s.s.ExpiryHours, "cryptoEnabled": s.s.CryptoEnabled}
}

// Quotation es lo que se ofrece a un cliente por un importe de una moneda.
type Quotation struct {
	Currency      string
	Segment       string
	Amount        vocab.Decimal // lo que se pidió
	BaseRate      vocab.Decimal // cambio de referencia, euros por una unidad
	RateOn        vocab.Date
	OfferedRate   vocab.Decimal // con el margen
	MarginKind    MarginKind
	Level         int
	MarginValue   vocab.Decimal
	MarginPercent vocab.Decimal
	Facial        vocab.Decimal
	Delivered     vocab.Decimal // lo que se entrega: el importe en billetes enteros
	Eur           vocab.Decimal // lo que paga el cliente
	NoMargin      bool          // la moneda no tiene margen para el segmento: se ofrece el cambio de referencia
}

// RoundDownToFacial devuelve el importe en billetes enteros. Un importe inferior a un billete se
// deja como se pidió, y también cualquier importe cuando la moneda no tiene billete.
func RoundDownToFacial(amount, facial vocab.Decimal) vocab.Decimal {
	if !facial.IsPositive() || !amount.IsPositive() {
		return amount
	}
	if r := amount.Div(facial).Floor().Mul(facial); r.IsPositive() {
		return r
	}
	return amount
}

// Quote pone precio a un importe de una moneda para un segmento al nivel de precio de la empresa:
// cambio de referencia más margen (seis decimales), el importe en billetes enteros, y su valor en
// euros (céntimos, redondeando el medio lejos de cero). margin puede ser nil.
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

// Campos de precios.
var (
	CurFieldCompany = spec.Comparable("company", func(c *Currency) OrganizationID { return c.s.Company })
	CurFieldCode    = spec.Ordered("code", func(c *Currency) string { return c.s.Code })
	MrgFieldCompany = spec.Comparable("company", func(m *Margin) OrganizationID { return m.s.Company })
	MrgFieldCur     = spec.Ordered("currency", func(m *Margin) string { return m.s.Currency })
	MrgFieldSegment = spec.Ordered("segment_code", func(m *Margin) string { return m.s.Segment })
	SetFieldCompany = spec.Comparable("company", func(s *Settings) OrganizationID { return s.s.Company })
)

// Identidades.
func NewCurrencyID() CurrencyID       { return CurrencyID{fw.NewUUID()} }
func NewMarginID() MarginID           { return MarginID{fw.NewUUID()} }
func NewSettingsID() SettingsID       { return SettingsID{fw.NewUUID()} }
func NewReservationID() ReservationID { return ReservationID{fw.NewUUID()} }

// ParseCurrencyID interpreta una identidad en texto.
func ParseCurrencyID(s string) (CurrencyID, error) {
	u, err := fw.ParseUUID(s)
	return CurrencyID{u}, err
}

// ParseMarginID interpreta una identidad en texto.
func ParseMarginID(s string) (MarginID, error) { u, err := fw.ParseUUID(s); return MarginID{u}, err }

// Repositorios.
type (
	CurrencyRepository    = fw.Repository[CurrencyID, *Currency]
	MarginRepository      = fw.Repository[MarginID, *Margin]
	SettingsRepository    = fw.Repository[SettingsID, *Settings]
	ReservationRepository = fw.Repository[ReservationID, *Reservation]
)

// Collaborators resuelve el código de promoción de un colaborador (un puerto propio de Exchange; sin
// él no se puede validar ningún código).
type Collaborators interface {
	// ByPromotionCode devuelve el colaborador de un código y si hay uno activo.
	ByPromotionCode(ctx context.Context, company OrganizationID, code string) (PartyID, bool, error)
}

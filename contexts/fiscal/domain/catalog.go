// Package domain is the model of the Fiscal bounded context: the tax rate and tax treatment
// catalogs, the fiscal profile of each taxpayer (territory, prorrata, activities and filing
// obligations), the withholdings reported by Payroll and the tax filings (Modelo 111 and 190 in
// this phase) with their numbering. Invoices belong to Billing, payslips to Payroll, tax numbers
// to Parties: this context references them by identity.
package domain

import (
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

type (
	// TaxRateID identifies a tax rate.
	TaxRateID struct{ fw.UUID }
	// TreatmentID identifies a tax treatment.
	TreatmentID struct{ fw.UUID }
	// TaxpayerID identifies a taxpayer profile.
	TaxpayerID struct{ fw.UUID }
	// FilingID identifies a tax filing.
	FilingID struct{ fw.UUID }
	// CounterID identifies a filing counter.
	CounterID struct{ fw.UUID }
	// WithholdingID identifies a withholding record (the id of the payslip that reported it).
	WithholdingID struct{ fw.UUID }
	// OrganizationID is an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
	// PartyID is a party of the Parties context.
	PartyID struct{ fw.UUID }
)

var hundred = vocab.DecimalFromInt(100)

// Territory is a fiscal territory (the C# FiscalTerritory enum; OfficialTaxFormSubscription used
// a free string instead).
type Territory int

// Territories.
const (
	Common Territory = iota + 1
	BasqueCountry
	Navarre
	Canaries
	CeutaMelilla
)

var territories = map[Territory]string{Common: "common", BasqueCountry: "basque-country", Navarre: "navarre", Canaries: "canaries",
	CeutaMelilla: "ceuta-melilla"}

// String returns the stable name.
func (t Territory) String() string { return territories[t] }

// ParseTerritory parses a territory name.
func ParseTerritory(s string) (Territory, bool) { return parseEnum(territories, s) }

// TaxType is an indirect tax: VAT (IVA) in the peninsula and the Balearic Islands and the foral
// territories, IGIC in the Canaries, IPSI in Ceuta and Melilla.
type TaxType int

// Tax types.
const (
	VAT TaxType = iota + 1
	IGIC
	IPSI
)

var taxTypes = map[TaxType]string{VAT: "vat", IGIC: "igic", IPSI: "ipsi"}

// String returns the stable name.
func (t TaxType) String() string { return taxTypes[t] }

// ParseTaxType parses a tax type name.
func ParseTaxType(s string) (TaxType, bool) { return parseEnum(taxTypes, s) }

// Applies reports whether the tax type is levied in the territory.
func (t TaxType) Applies(in Territory) bool {
	switch t {
	case IGIC:
		return in == Canaries
	case IPSI:
		return in == CeutaMelilla
	case VAT:
		return in == Common || in == BasqueCountry || in == Navarre
	}
	return false
}

func parseEnum[K comparable](m map[K]string, s string) (K, bool) {
	for k, n := range m {
		if n == s {
			return k, true
		}
	}
	var zero K
	return zero, false
}

// NormalizeCode returns a catalog code in its stored form.
func NormalizeCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func code(v *fw.Validation, s string) string {
	c := NormalizeCode(s)
	v.Require(c != "" && utf8.RuneCountInString(c) <= 5, "code", "length", "a code of 1 to 5 characters is required")
	return c
}

func text(v *fw.Validation, field, s string, min, max int) string {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	v.Require(n >= min && n <= max, field, "length", "invalid length")
	return s
}

func percent(v *fw.Validation, field string, d vocab.Decimal) {
	v.Require(!d.IsNegative() && d.LessThanOrEqual(hundred) && d.Equal(d.Round(2)), field, "range", "a percentage from 0 to 100 with two decimals")
}

// TaxRateKind is the stable aggregate type name.
const TaxRateKind = "fiscal.tax_rate"

// TaxRate is a rate of an indirect tax in a territory, in force for a period (the C# VatGroup had
// neither territory nor validity, so the Sage import collided on territorial codes). The
// equivalence surcharge (recargo de equivalencia) exists only in VAT.
type TaxRate struct {
	fw.BaseAggregateRoot[TaxRateID]
	traits.Audited
	s TaxRateState
}

// TaxRateState is the persisted state of a tax rate.
type TaxRateState struct {
	Type        TaxType
	Territory   Territory
	Code        string
	Description string
	Rate        vocab.Decimal
	Surcharge   vocab.Decimal // equivalence surcharge; zero when none
	From        vocab.Date
	Until       vocab.Date // zero: open
	Audit       traits.AuditStamp
}

func (s *TaxRateState) check(v *fw.Validation) {
	_, okType := taxTypes[s.Type]
	_, okTerr := territories[s.Territory]
	v.Require(okType, "type", "enum", "vat, igic or ipsi")
	v.Require(okTerr, "territory", "enum", "unknown territory")
	v.Require(!okType || !okTerr || s.Type.Applies(s.Territory), "territory", "tax", "the tax is not levied in that territory")
	s.Code = code(v, s.Code)
	s.Description = text(v, "description", s.Description, 1, 100)
	percent(v, "rate", s.Rate)
	percent(v, "surcharge", s.Surcharge)
	v.Require(s.Surcharge.IsZero() || s.Type == VAT, "surcharge", "tax", "the equivalence surcharge exists only in VAT")
	v.Require(!s.From.IsZero() && (s.Until.IsZero() || !s.Until.Before(s.From)), "from", "order", "a validity whose end does not precede its start")
}

// ReconstituteTaxRate rebuilds a tax rate.
func ReconstituteTaxRate(id TaxRateID, s TaxRateState) (*TaxRate, error) {
	base, err := fw.NewBaseAggregateRoot(TaxRateKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.check(&v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &TaxRate{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state of the rate.
func (r *TaxRate) State() TaxRateState { return r.s }

// InForceOn reports whether the rate applies on a date.
func (r *TaxRate) InForceOn(d vocab.Date) bool {
	return !d.Before(r.s.From) && (r.s.Until.IsZero() || !d.After(r.s.Until))
}

// Overlaps reports whether two rates share type, territory and code in overlapping periods.
func (r *TaxRate) Overlaps(o TaxRateState) bool {
	return r.s.Type == o.Type && r.s.Territory == o.Territory && r.s.Code == o.Code &&
		(r.s.Until.IsZero() || !o.From.After(r.s.Until)) && (o.Until.IsZero() || !r.s.From.After(o.Until))
}

// End closes the validity of the rate on a date (a new rate replaces it).
func (r *TaxRate) End(on vocab.Date) error {
	if on.Before(r.s.From) {
		return fw.Violation("fiscal.rate_end_before_start", "a rate cannot end before it starts")
	}
	r.s.Until = on
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (r *TaxRate) AuditSnapshot() map[string]any {
	return map[string]any{"code": r.s.Code, "rate": r.s.Rate.String(), "until": r.s.Until.String()}
}

// TreatmentKind says whether an operation is subject to the tax, exempt or not subject (the C#
// VatIndicator allowed both flags at once).
type TreatmentKind int

// Treatment kinds.
const (
	Subject TreatmentKind = iota + 1
	Exempt
	NotSubject
)

var treatmentKinds = map[TreatmentKind]string{Subject: "subject", Exempt: "exempt", NotSubject: "not-subject"}

// String returns the stable name.
func (k TreatmentKind) String() string { return treatmentKinds[k] }

// ParseTreatmentKind parses a treatment kind name.
func ParseTreatmentKind(s string) (TreatmentKind, bool) { return parseEnum(treatmentKinds, s) }

// TreatmentKindName is the stable aggregate type name.
const TreatmentKindName = "fiscal.tax_treatment"

// Treatment is a tax treatment (regime or exemption indicator) of a territory.
type Treatment struct {
	fw.BaseAggregateRoot[TreatmentID]
	traits.Audited
	s TreatmentState
}

// TreatmentState is the persisted state of a treatment.
type TreatmentState struct {
	Territory   Territory
	Code        string
	Description string
	Kind        TreatmentKind
	Active      bool
	Audit       traits.AuditStamp
}

// ReconstituteTreatment rebuilds a treatment.
func ReconstituteTreatment(id TreatmentID, s TreatmentState) (*Treatment, error) {
	base, err := fw.NewBaseAggregateRoot(TreatmentKindName, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	_, okTerr := territories[s.Territory]
	v.Require(okTerr, "territory", "enum", "unknown territory")
	_, okKind := treatmentKinds[s.Kind]
	v.Require(okKind, "kind", "enum", "subject, exempt or not-subject")
	s.Code = code(&v, s.Code)
	s.Description = text(&v, "description", s.Description, 1, 100)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Treatment{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state of the treatment.
func (t *Treatment) State() TreatmentState { return t.s }

// Deactivate retires the treatment.
func (t *Treatment) Deactivate() { t.s.Active = false }

// AuditSnapshot implements traits.Snapshotter.
func (t *Treatment) AuditSnapshot() map[string]any {
	return map[string]any{"code": t.s.Code, "kind": t.s.Kind.String(), "active": t.s.Active}
}

// Catalog fields.
var (
	RateFieldType      = spec.Comparable("tax_type", func(r *TaxRate) int { return int(r.s.Type) })
	RateFieldTerritory = spec.Comparable("territory", func(r *TaxRate) int { return int(r.s.Territory) })
	RateFieldCode      = spec.Ordered("code", func(r *TaxRate) string { return r.s.Code })
	TreatFieldTerr     = spec.Comparable("territory", func(t *Treatment) int { return int(t.s.Territory) })
	TreatFieldCode     = spec.Ordered("code", func(t *Treatment) string { return t.s.Code })
	TreatFieldActive   = spec.Comparable("active", func(t *Treatment) bool { return t.s.Active })
)

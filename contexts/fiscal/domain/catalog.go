// Package domain es el modelo del contexto delimitado Fiscal: los catálogos de tipos impositivos y de
// tratamientos fiscales, el perfil fiscal de cada contribuyente (territorio, prorrata, actividades
// y obligaciones de presentación), las retenciones que comunica Payroll y las declaraciones
// (Modelos 111 y 190 en esta fase) con su numeración. Las facturas son de Billing, las nóminas de
// Payroll, los NIF de Parties: este contexto los referencia por identidad.
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
	// TaxRateID identifica un tipo impositivo.
	TaxRateID struct{ fw.UUID }
	// TreatmentID identifica un tratamiento fiscal.
	TreatmentID struct{ fw.UUID }
	// TaxpayerID identifica un perfil de contribuyente.
	TaxpayerID struct{ fw.UUID }
	// FilingID identifica una declaración.
	FilingID struct{ fw.UUID }
	// CounterID identifica un contador de declaraciones.
	CounterID struct{ fw.UUID }
	// WithholdingID identifica un registro de retención (el id de la nómina que la comunicó).
	WithholdingID struct{ fw.UUID }
	// OrganizationID es una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID es una party del contexto Parties.
	PartyID struct{ fw.UUID }
)

var hundred = vocab.DecimalFromInt(100)

// Territory es un territorio fiscal (el enum FiscalTerritory del C#; OfficialTaxFormSubscription
// usaba en su lugar un texto libre).
type Territory int

// Territorios.
const (
	Common Territory = iota + 1
	BasqueCountry
	Navarre
	Canaries
	CeutaMelilla
)

var territories = map[Territory]string{Common: "common", BasqueCountry: "basque-country", Navarre: "navarre", Canaries: "canaries",
	CeutaMelilla: "ceuta-melilla"}

// String devuelve el nombre estable.
func (t Territory) String() string { return territories[t] }

// ParseTerritory interpreta el nombre de un territorio.
func ParseTerritory(s string) (Territory, bool) { return parseEnum(territories, s) }

// TaxType es un impuesto indirecto: el IVA en la península, Baleares y los territorios forales, el
// IGIC en Canarias, el IPSI en Ceuta y Melilla.
type TaxType int

// Impuestos.
const (
	VAT TaxType = iota + 1
	IGIC
	IPSI
)

var taxTypes = map[TaxType]string{VAT: "vat", IGIC: "igic", IPSI: "ipsi"}

// String devuelve el nombre estable.
func (t TaxType) String() string { return taxTypes[t] }

// ParseTaxType interpreta el nombre de un impuesto.
func ParseTaxType(s string) (TaxType, bool) { return parseEnum(taxTypes, s) }

// Applies dice si el impuesto se aplica en el territorio.
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

// NormalizeCode devuelve un código de catálogo en la forma en que se guarda.
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

// TaxRateKind es el nombre estable del tipo de agregado.
const TaxRateKind = "fiscal.tax_rate"

// TaxRate es un tipo de un impuesto indirecto en un territorio, vigente durante un periodo (el
// VatGroup del C# no tenía ni territorio ni vigencia, así que la importación de Sage chocaba con
// los códigos territoriales). El recargo de equivalencia sólo existe en el IVA.
type TaxRate struct {
	fw.BaseAggregateRoot[TaxRateID]
	traits.Audited
	s TaxRateState
}

// TaxRateState es el estado persistido de un tipo impositivo.
type TaxRateState struct {
	Type        TaxType
	Territory   Territory
	Code        string
	Description string
	Rate        vocab.Decimal
	Surcharge   vocab.Decimal // recargo de equivalencia; cero si no hay
	From        vocab.Date
	Until       vocab.Date // cero: abierto
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

// ReconstituteTaxRate reconstruye un tipo impositivo.
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

// State devuelve el estado del tipo.
func (r *TaxRate) State() TaxRateState { return r.s }

// InForceOn dice si el tipo se aplica en una fecha.
func (r *TaxRate) InForceOn(d vocab.Date) bool {
	return !d.Before(r.s.From) && (r.s.Until.IsZero() || !d.After(r.s.Until))
}

// Overlaps dice si dos tipos comparten impuesto, territorio y código en periodos que se solapan.
func (r *TaxRate) Overlaps(o TaxRateState) bool {
	return r.s.Type == o.Type && r.s.Territory == o.Territory && r.s.Code == o.Code &&
		(r.s.Until.IsZero() || !o.From.After(r.s.Until)) && (o.Until.IsZero() || !r.s.From.After(o.Until))
}

// End cierra la vigencia del tipo en una fecha (lo sustituye un tipo nuevo).
func (r *TaxRate) End(on vocab.Date) error {
	if on.Before(r.s.From) {
		return fw.Violation("fiscal.rate_end_before_start", "a rate cannot end before it starts")
	}
	r.s.Until = on
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (r *TaxRate) AuditSnapshot() map[string]any {
	return map[string]any{"code": r.s.Code, "rate": r.s.Rate.String(), "until": r.s.Until.String()}
}

// TreatmentKind dice si una operación está sujeta al impuesto, exenta o no sujeta (el
// VatIndicator del C# permitía los dos indicadores a la vez).
type TreatmentKind int

// Clases de tratamiento.
const (
	Subject TreatmentKind = iota + 1
	Exempt
	NotSubject
)

var treatmentKinds = map[TreatmentKind]string{Subject: "subject", Exempt: "exempt", NotSubject: "not-subject"}

// String devuelve el nombre estable.
func (k TreatmentKind) String() string { return treatmentKinds[k] }

// ParseTreatmentKind interpreta el nombre de una clase de tratamiento.
func ParseTreatmentKind(s string) (TreatmentKind, bool) { return parseEnum(treatmentKinds, s) }

// TreatmentKindName es el nombre estable del tipo de agregado.
const TreatmentKindName = "fiscal.tax_treatment"

// Treatment es un tratamiento fiscal (régimen o indicador de exención) de un territorio.
type Treatment struct {
	fw.BaseAggregateRoot[TreatmentID]
	traits.Audited
	s TreatmentState
}

// TreatmentState es el estado persistido de un tratamiento.
type TreatmentState struct {
	Territory   Territory
	Code        string
	Description string
	Kind        TreatmentKind
	Active      bool
	Audit       traits.AuditStamp
}

// ReconstituteTreatment reconstruye un tratamiento.
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

// State devuelve el estado del tratamiento.
func (t *Treatment) State() TreatmentState { return t.s }

// Deactivate retira el tratamiento.
func (t *Treatment) Deactivate() { t.s.Active = false }

// AuditSnapshot implementa traits.Snapshotter.
func (t *Treatment) AuditSnapshot() map[string]any {
	return map[string]any{"code": t.s.Code, "kind": t.s.Kind.String(), "active": t.s.Active}
}

// Campos de los catálogos.
var (
	RateFieldType      = spec.Comparable("tax_type", func(r *TaxRate) int { return int(r.s.Type) })
	RateFieldTerritory = spec.Comparable("territory", func(r *TaxRate) int { return int(r.s.Territory) })
	RateFieldCode      = spec.Ordered("code", func(r *TaxRate) string { return r.s.Code })
	TreatFieldTerr     = spec.Comparable("territory", func(t *Treatment) int { return int(t.s.Territory) })
	TreatFieldCode     = spec.Ordered("code", func(t *Treatment) string { return t.s.Code })
	TreatFieldActive   = spec.Comparable("active", func(t *Treatment) bool { return t.s.Active })
)

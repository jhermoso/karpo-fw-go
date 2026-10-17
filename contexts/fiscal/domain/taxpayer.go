package domain

import (
	"slices"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// TaxpayerKind es el nombre estable del tipo de agregado.
const TaxpayerKind = "fiscal.taxpayer"

// Form es un modelo de la AEAT que este contexto conoce (el C# enumeraba los códigos en un
// comentario y los guardaba como texto libre).
type Form string

// Periodicity es la periodicidad de una declaración.
type Periodicity int

// Periodicidades.
const (
	Monthly Periodicity = iota + 1
	Quarterly
	Annual
)

var periodicities = map[Periodicity]string{Monthly: "monthly", Quarterly: "quarterly", Annual: "annual"}

// String devuelve el nombre estable.
func (p Periodicity) String() string { return periodicities[p] }

// ParsePeriodicity interpreta el nombre de una periodicidad.
func ParsePeriodicity(s string) (Periodicity, bool) { return parseEnum(periodicities, s) }

// Forms son los modelos y las periodicidades con que se presentan. En esta fase sólo se generan el 111 y el 190;
// los demás se pueden registrar como obligaciones.
var Forms = map[Form][]Periodicity{
	"111": {Monthly, Quarterly}, "115": {Monthly, Quarterly}, "180": {Annual}, "190": {Annual}, "216": {Monthly, Quarterly},
	"296": {Annual}, "303": {Monthly, Quarterly}, "347": {Annual}, "349": {Monthly, Quarterly, Annual}, "390": {Annual},
}

// ActivityCategory es la categoría de una actividad económica.
type ActivityCategory int

// Categorías de actividad (ActivityCategoryType del C#).
const (
	Trade ActivityCategory = iota + 1
	Industry
	Services
	Professional
	OtherActivity
)

var categories = map[ActivityCategory]string{Trade: "trade", Industry: "industry", Services: "services", Professional: "professional",
	OtherActivity: "other"}

// String devuelve el nombre estable.
func (c ActivityCategory) String() string { return categories[c] }

// ParseActivityCategory interpreta el nombre de una categoría.
func ParseActivityCategory(s string) (ActivityCategory, bool) { return parseEnum(categories, s) }

// ActivityID identifica una actividad.
type ActivityID struct{ fw.UUID }

// Activity es una actividad económica del contribuyente (el FiscalActivity del C#, ahora con
// from ≤ until comprobado).
type Activity struct {
	ID               ActivityID
	Code             string
	IAE              string
	Category         ActivityCategory
	Description      string
	From             vocab.Date
	Until            vocab.Date
	Primary          bool
	VATDeductible    bool
	EstimationRegime string
}

func (a Activity) activeOn(d vocab.Date) bool {
	return !d.Before(a.From) && (a.Until.IsZero() || !d.After(a.Until))
}

// ObligationID identifica una obligación.
type ObligationID struct{ fw.UUID }

// Obligation es un modelo que el contribuyente debe presentar, con su periodicidad, desde un año
// (el OfficialTaxFormSubscription del C#, con códigos y periodicidades comprobados).
type Obligation struct {
	ID          ObligationID
	Form        Form
	Periodicity Periodicity
	FromYear    int
	UntilYear   int // cero: abierta
}

// Covers dice si la obligación está vigente en un año.
func (o Obligation) Covers(year int) bool {
	return year >= o.FromYear && (o.UntilYear == 0 || year <= o.UntilYear)
}

// Taxpayer es el perfil fiscal de una organización interna: territorio, inicio del ejercicio,
// prorrata, actividades y obligaciones de presentación (la parte fiscal de
// InternalOrganizationProfile, FiscalActivity y OfficialTaxFormSubscription del C#).
type Taxpayer struct {
	fw.BaseAggregateRoot[TaxpayerID]
	traits.Audited
	organization OrganizationID
	terms        TaxpayerTerms
	activities   []Activity
	obligations  []Obligation
}

// TaxpayerTerms son las condiciones editables de un contribuyente.
type TaxpayerTerms struct {
	Territory             Territory
	FiscalYearStartMonth  int
	GeneralProrata        vocab.Decimal // cero: sin prorrata
	DifferentiatedSectors bool
}

func (t TaxpayerTerms) check(v *fw.Validation) {
	_, ok := territories[t.Territory]
	v.Require(ok, "territory", "enum", "unknown territory")
	v.Require(t.FiscalYearStartMonth >= 1 && t.FiscalYearStartMonth <= 12, "fiscalYearStartMonth", "range", "a month from 1 to 12")
	percent(v, "generalProrata", t.GeneralProrata)
}

// TaxpayerState es el estado persistido de un contribuyente.
type TaxpayerState struct {
	Organization OrganizationID
	Terms        TaxpayerTerms
	Activities   []Activity
	Obligations  []Obligation
	Audit        traits.AuditStamp
}

// ReconstituteTaxpayer reconstruye un contribuyente.
func ReconstituteTaxpayer(id TaxpayerID, s TaxpayerState) (*Taxpayer, error) {
	base, err := fw.NewBaseAggregateRoot(TaxpayerKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Organization.IsZero(), "organization", "required", "a taxpayer is an organization")
	s.Terms.check(&v)
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Taxpayer{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), organization: s.Organization, terms: s.Terms,
		activities: slices.Clone(s.Activities), obligations: slices.Clone(s.Obligations)}, nil
}

// RegisterTaxpayer crea el perfil fiscal de una organización.
func RegisterTaxpayer(id TaxpayerID, org OrganizationID, t TaxpayerTerms) (*Taxpayer, error) {
	return ReconstituteTaxpayer(id, TaxpayerState{Organization: org, Terms: t})
}

// Organization devuelve la organización interna.
func (t *Taxpayer) Organization() OrganizationID { return t.organization }

// Terms devuelve las condiciones editables.
func (t *Taxpayer) Terms() TaxpayerTerms { return t.terms }

// Activities devuelve una copia de las actividades.
func (t *Taxpayer) Activities() []Activity { return slices.Clone(t.activities) }

// Obligations devuelve una copia de las obligaciones.
func (t *Taxpayer) Obligations() []Obligation { return slices.Clone(t.obligations) }

// SetTerms sustituye las condiciones.
func (t *Taxpayer) SetTerms(terms TaxpayerTerms) error {
	var v fw.Validation
	terms.check(&v)
	if err := v.Err(); err != nil {
		return err
	}
	t.terms = terms
	return nil
}

// FiscalYearOf devuelve el ejercicio al que pertenece una fecha (con el nombre del año natural en
// que empieza).
func (t *Taxpayer) FiscalYearOf(d vocab.Date) int {
	if int(d.Month()) < t.terms.FiscalYearStartMonth {
		return d.Year() - 1
	}
	return d.Year()
}

// AddActivity añade una actividad. Invariantes: from ≤ until, y una sola actividad principal a la
// vez (una principal nueva hace secundaria a la que se solapa).
func (t *Taxpayer) AddActivity(a Activity) (ActivityID, error) {
	var v fw.Validation
	a.Code = text(&v, "code", a.Code, 1, 20)
	a.IAE = text(&v, "iae", a.IAE, 0, 10)
	_, ok := categories[a.Category]
	v.Require(ok, "category", "enum", "unknown activity category")
	a.Description = text(&v, "description", a.Description, 1, 200)
	a.EstimationRegime = text(&v, "estimationRegime", a.EstimationRegime, 0, 30)
	v.Require(!a.From.IsZero() && (a.Until.IsZero() || !a.Until.Before(a.From)), "from", "order", "a validity whose end does not precede its start")
	if err := v.Err(); err != nil {
		return ActivityID{}, err
	}
	acts := slices.Clone(t.activities)
	if a.Primary {
		for i, o := range acts {
			if o.Primary && (o.Until.IsZero() || !a.From.After(o.Until)) && (a.Until.IsZero() || !o.From.After(a.Until)) {
				acts[i].Primary = false
			}
		}
	}
	a.ID = ActivityID{fw.NewUUID()}
	t.activities = append(acts, a)
	return a.ID, nil
}

// EndActivity termina una actividad en una fecha.
func (t *Taxpayer) EndActivity(id ActivityID, on vocab.Date) error {
	i := slices.IndexFunc(t.activities, func(a Activity) bool { return a.ID == id })
	if i < 0 {
		return fw.NotFound("fiscal.activity", id)
	}
	if on.Before(t.activities[i].From) {
		return fw.Violation("fiscal.activity_end_before_start", "an activity cannot end before it starts")
	}
	t.activities = slices.Clone(t.activities)
	t.activities[i].Until = on
	return nil
}

// PrimaryActivityOn devuelve la actividad principal en una fecha.
func (t *Taxpayer) PrimaryActivityOn(d vocab.Date) (Activity, bool) {
	for _, a := range t.activities {
		if a.Primary && a.activeOn(d) {
			return a, true
		}
	}
	return Activity{}, false
}

// AddObligation registra un modelo que hay que presentar. Invariantes: un modelo conocido con una
// de sus periodicidades, y una obligación por modelo en cada año.
func (t *Taxpayer) AddObligation(o Obligation) (ObligationID, error) {
	var v fw.Validation
	allowed, known := Forms[o.Form]
	v.Require(known, "form", "unknown", "unknown tax form")
	v.Require(!known || slices.Contains(allowed, o.Periodicity), "periodicity", "form", "the form is not filed with that periodicity")
	v.Require(o.FromYear >= 1990 && (o.UntilYear == 0 || o.UntilYear >= o.FromYear), "fromYear", "range", "valid years")
	if err := v.Err(); err != nil {
		return ObligationID{}, err
	}
	for _, x := range t.obligations {
		if x.Form == o.Form && (x.UntilYear == 0 || o.FromYear <= x.UntilYear) && (o.UntilYear == 0 || x.FromYear <= o.UntilYear) {
			return ObligationID{}, fw.Violation("fiscal.obligation_overlaps", "the form is already an obligation in those years")
		}
	}
	o.ID = ObligationID{fw.NewUUID()}
	t.obligations = append(slices.Clone(t.obligations), o)
	return o.ID, nil
}

// EndObligation termina una obligación después de un año.
func (t *Taxpayer) EndObligation(id ObligationID, lastYear int) error {
	i := slices.IndexFunc(t.obligations, func(o Obligation) bool { return o.ID == id })
	if i < 0 {
		return fw.NotFound("fiscal.obligation", id)
	}
	if lastYear < t.obligations[i].FromYear {
		return fw.Violation("fiscal.obligation_end_before_start", "an obligation cannot end before it starts")
	}
	t.obligations = slices.Clone(t.obligations)
	t.obligations[i].UntilYear = lastYear
	return nil
}

// ObligationFor devuelve la obligación de un modelo vigente en un año.
func (t *Taxpayer) ObligationFor(f Form, year int) (Obligation, bool) {
	for _, o := range t.obligations {
		if o.Form == f && o.Covers(year) {
			return o, true
		}
	}
	return Obligation{}, false
}

// AuditSnapshot implementa traits.Snapshotter.
func (t *Taxpayer) AuditSnapshot() map[string]any {
	return map[string]any{"territory": t.terms.Territory.String(), "prorrata": t.terms.GeneralProrata.String(),
		"activities": len(t.activities), "obligations": len(t.obligations)}
}

// Campos del contribuyente.
var (
	TaxpayerFieldID  = spec.Comparable("id", func(t *Taxpayer) TaxpayerID { return t.ID() })
	TaxpayerFieldOrg = spec.Comparable("organization", (*Taxpayer).Organization)
)

package domain

import (
	"slices"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// TaxpayerKind is the stable aggregate type name.
const TaxpayerKind = "fiscal.taxpayer"

// Form is an AEAT tax form this context knows (the C# listed the codes in a comment and stored
// them as free strings).
type Form string

// Periodicity of a filing.
type Periodicity int

// Periodicities.
const (
	Monthly Periodicity = iota + 1
	Quarterly
	Annual
)

var periodicities = map[Periodicity]string{Monthly: "monthly", Quarterly: "quarterly", Annual: "annual"}

// String returns the stable name.
func (p Periodicity) String() string { return periodicities[p] }

// ParsePeriodicity parses a periodicity name.
func ParsePeriodicity(s string) (Periodicity, bool) { return parseEnum(periodicities, s) }

// Forms and the periodicities they are filed with. Only 111 and 190 are generated in this phase;
// the others can be registered as obligations.
var Forms = map[Form][]Periodicity{
	"111": {Monthly, Quarterly}, "115": {Monthly, Quarterly}, "180": {Annual}, "190": {Annual}, "216": {Monthly, Quarterly},
	"296": {Annual}, "303": {Monthly, Quarterly}, "347": {Annual}, "349": {Monthly, Quarterly, Annual}, "390": {Annual},
}

// ActivityCategory is the category of an economic activity.
type ActivityCategory int

// Activity categories (the C# ActivityCategoryType).
const (
	Trade ActivityCategory = iota + 1
	Industry
	Services
	Professional
	OtherActivity
)

var categories = map[ActivityCategory]string{Trade: "trade", Industry: "industry", Services: "services", Professional: "professional",
	OtherActivity: "other"}

// String returns the stable name.
func (c ActivityCategory) String() string { return categories[c] }

// ParseActivityCategory parses a category name.
func ParseActivityCategory(s string) (ActivityCategory, bool) { return parseEnum(categories, s) }

// ActivityID identifies an activity.
type ActivityID struct{ fw.UUID }

// Activity is an economic activity of the taxpayer (the C# FiscalActivity, with from ≤ until now
// checked).
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

// ObligationID identifies an obligation.
type ObligationID struct{ fw.UUID }

// Obligation is a form the taxpayer must file, with its periodicity, from a year on (the C#
// OfficialTaxFormSubscription, with codes and periodicities checked).
type Obligation struct {
	ID          ObligationID
	Form        Form
	Periodicity Periodicity
	FromYear    int
	UntilYear   int // zero: open
}

// Covers reports whether the obligation is in force in a year.
func (o Obligation) Covers(year int) bool {
	return year >= o.FromYear && (o.UntilYear == 0 || year <= o.UntilYear)
}

// Taxpayer is the fiscal profile of an internal organization: territory, fiscal year start,
// prorrata, activities and filing obligations (the fiscal slice of the C#
// InternalOrganizationProfile, FiscalActivity and OfficialTaxFormSubscription).
type Taxpayer struct {
	fw.BaseAggregateRoot[TaxpayerID]
	traits.Audited
	organization OrganizationID
	terms        TaxpayerTerms
	activities   []Activity
	obligations  []Obligation
}

// TaxpayerTerms are the editable terms of a taxpayer.
type TaxpayerTerms struct {
	Territory             Territory
	FiscalYearStartMonth  int
	GeneralProrata        vocab.Decimal // zero: no prorrata
	DifferentiatedSectors bool
}

func (t TaxpayerTerms) check(v *fw.Validation) {
	_, ok := territories[t.Territory]
	v.Require(ok, "territory", "enum", "unknown territory")
	v.Require(t.FiscalYearStartMonth >= 1 && t.FiscalYearStartMonth <= 12, "fiscalYearStartMonth", "range", "a month from 1 to 12")
	percent(v, "generalProrata", t.GeneralProrata)
}

// TaxpayerState is the persisted state of a taxpayer.
type TaxpayerState struct {
	Organization OrganizationID
	Terms        TaxpayerTerms
	Activities   []Activity
	Obligations  []Obligation
	Audit        traits.AuditStamp
}

// ReconstituteTaxpayer rebuilds a taxpayer.
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

// RegisterTaxpayer creates the fiscal profile of an organization.
func RegisterTaxpayer(id TaxpayerID, org OrganizationID, t TaxpayerTerms) (*Taxpayer, error) {
	return ReconstituteTaxpayer(id, TaxpayerState{Organization: org, Terms: t})
}

// Organization returns the internal organization.
func (t *Taxpayer) Organization() OrganizationID { return t.organization }

// Terms returns the editable terms.
func (t *Taxpayer) Terms() TaxpayerTerms { return t.terms }

// Activities returns a copy of the activities.
func (t *Taxpayer) Activities() []Activity { return slices.Clone(t.activities) }

// Obligations returns a copy of the obligations.
func (t *Taxpayer) Obligations() []Obligation { return slices.Clone(t.obligations) }

// SetTerms replaces the terms.
func (t *Taxpayer) SetTerms(terms TaxpayerTerms) error {
	var v fw.Validation
	terms.check(&v)
	if err := v.Err(); err != nil {
		return err
	}
	t.terms = terms
	return nil
}

// FiscalYearOf returns the fiscal year a date belongs to (named by the calendar year it starts in).
func (t *Taxpayer) FiscalYearOf(d vocab.Date) int {
	if int(d.Month()) < t.terms.FiscalYearStartMonth {
		return d.Year() - 1
	}
	return d.Year()
}

// AddActivity adds an activity. Invariants: from ≤ until, and one primary activity at a time (a
// new primary makes the overlapping one secondary).
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

// EndActivity ends an activity on a date.
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

// PrimaryActivityOn returns the primary activity on a date.
func (t *Taxpayer) PrimaryActivityOn(d vocab.Date) (Activity, bool) {
	for _, a := range t.activities {
		if a.Primary && a.activeOn(d) {
			return a, true
		}
	}
	return Activity{}, false
}

// AddObligation registers a form to file. Invariants: a known form with one of its
// periodicities, and one obligation per form in each year.
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

// EndObligation ends an obligation after a year.
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

// ObligationFor returns the obligation of a form in force in a year.
func (t *Taxpayer) ObligationFor(f Form, year int) (Obligation, bool) {
	for _, o := range t.obligations {
		if o.Form == f && o.Covers(year) {
			return o, true
		}
	}
	return Obligation{}, false
}

// AuditSnapshot implements traits.Snapshotter.
func (t *Taxpayer) AuditSnapshot() map[string]any {
	return map[string]any{"territory": t.terms.Territory.String(), "prorrata": t.terms.GeneralProrata.String(),
		"activities": len(t.activities), "obligations": len(t.obligations)}
}

// Taxpayer fields.
var (
	TaxpayerFieldID  = spec.Comparable("id", func(t *Taxpayer) TaxpayerID { return t.ID() })
	TaxpayerFieldOrg = spec.Comparable("organization", (*Taxpayer).Organization)
)

package domain

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Indirect taxes are calculated by jurisdiction (approved decision, BACKLOG §6): the core of Fiscal
// is country-agnostic; each country contributes a Jurisdiction with its taxes, rules, rounding and
// regimes, and each regime or sector is a strategy inside it. A new country is a new
// Jurisdiction, registered without touching the core.

// Assessment is what a jurisdiction needs to calculate the taxes of a document.
type Assessment struct {
	Taxpayer *Taxpayer // the seller: its territory and regime
	Date     vocab.Date
	// EquivalenceSurcharge: the customer is in the equivalence surcharge regime (Spanish retail).
	EquivalenceSurcharge bool
	Lines                []TaxableLine
}

// TaxableLine is a taxable base with its tax code or treatment. Bases arrive rounded to cents.
type TaxableLine struct {
	Ref       string
	Base      vocab.Decimal
	TaxCode   string // rate code of the catalog (e.g. G21); empty when the treatment is exempt or not subject
	Treatment string // treatment code of the catalog; empty: subject to the tax
}

// Breakdown is the tax breakdown of a document: one line per tax code and treatment.
type Breakdown struct {
	Lines     []BreakdownLine
	Net       vocab.Decimal
	Tax       vocab.Decimal
	Surcharge vocab.Decimal
}

// BreakdownLine is the base and quota of a rate (or of an exempt or not subject treatment).
type BreakdownLine struct {
	TaxType         TaxType // zero for exempt and not subject bases
	TaxCode         string
	Treatment       string
	TreatmentKind   TreatmentKind
	Rate            vocab.Decimal
	Base            vocab.Decimal
	Amount          vocab.Decimal
	SurchargeRate   vocab.Decimal
	SurchargeAmount vocab.Decimal
}

// RateBook gives a jurisdiction the catalog entries in force.
type RateBook interface {
	RateOn(ctx context.Context, t TaxType, in Territory, code string, on vocab.Date) (*TaxRate, bool, error)
	Treatment(ctx context.Context, in Territory, code string) (*Treatment, bool, error)
}

// Jurisdiction calculates the indirect taxes of a country.
type Jurisdiction interface {
	Country() string
	Calculate(ctx context.Context, a Assessment, book RateBook) (Breakdown, error)
}

// Country of a taxpayer. All the territories modelled today are Spanish; when a second country
// arrives, the territory moves into its jurisdiction (BACKLOG §6).
func (t *Taxpayer) Country() string { return "ES" }

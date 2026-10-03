package application

import (
	"context"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Engine implements contracts.TaxEngine: it resolves the fiscal profile of the seller, picks the
// jurisdiction of its country and lets it calculate with the catalog in force. It serves
// contexts, not users.
type Engine struct {
	Taxpayers     domain.TaxpayerRepository
	Rates         domain.TaxRateRepository
	Treatments    domain.TreatmentRepository
	Jurisdictions map[string]domain.Jurisdiction
}

var _ contracts.TaxEngine = Engine{}

// NewEngine registers the jurisdictions by country.
func NewEngine(taxpayers domain.TaxpayerRepository, rates domain.TaxRateRepository, treatments domain.TreatmentRepository,
	jurisdictions ...domain.Jurisdiction) Engine {
	e := Engine{Taxpayers: taxpayers, Rates: rates, Treatments: treatments, Jurisdictions: map[string]domain.Jurisdiction{}}
	for _, j := range jurisdictions {
		e.Jurisdictions[j.Country()] = j
	}
	return e
}

// RateOn implements domain.RateBook.
func (e Engine) RateOn(ctx context.Context, t domain.TaxType, in domain.Territory, code string, on vocab.Date) (*domain.TaxRate, bool, error) {
	rs, err := e.Rates.Find(ctx, spec.And(domain.RateFieldType.Eq(int(t)), domain.RateFieldTerritory.Eq(int(in)), domain.RateFieldCode.Eq(code)))
	if err != nil {
		return nil, false, err
	}
	for _, r := range rs {
		if r.InForceOn(on) {
			return r, true, nil
		}
	}
	return nil, false, nil
}

// Treatment implements domain.RateBook.
func (e Engine) Treatment(ctx context.Context, in domain.Territory, code string) (*domain.Treatment, bool, error) {
	ts, err := e.Treatments.Find(ctx, spec.And(domain.TreatFieldTerr.Eq(int(in)), domain.TreatFieldCode.Eq(code), domain.TreatFieldActive.Eq(true)))
	if err != nil || len(ts) == 0 {
		return nil, false, err
	}
	return ts[0], true, nil
}

// Calculate implements contracts.TaxEngine.
func (e Engine) Calculate(ctx context.Context, doc contracts.TaxableDocument) (contracts.TaxBreakdown, error) {
	var v fw.Validation
	seller := domain.OrganizationID{UUID: parseID(&v, "seller", doc.Seller)}
	date, err := vocab.ParseDate(doc.Date)
	v.Require(err == nil, "date", "format", "a date YYYY-MM-DD is required")
	a := domain.Assessment{Date: date, EquivalenceSurcharge: doc.EquivalenceSurcharge}
	for i, l := range doc.Lines {
		a.Lines = append(a.Lines, domain.TaxableLine{Ref: l.Ref, Base: parseDecimal(&v, fmt.Sprintf("lines[%d].base", i), l.Base),
			TaxCode: l.TaxCode, Treatment: l.Treatment})
	}
	if err := v.Err(); err != nil {
		return contracts.TaxBreakdown{}, err
	}
	tps, err := e.Taxpayers.Find(ctx, domain.TaxpayerFieldOrg.Eq(seller))
	if err != nil {
		return contracts.TaxBreakdown{}, err
	}
	if len(tps) == 0 {
		return contracts.TaxBreakdown{}, fw.Violation("fiscal.no_taxpayer", "the seller has no fiscal profile")
	}
	a.Taxpayer = tps[0]
	j, ok := e.Jurisdictions[a.Taxpayer.Country()]
	if !ok {
		return contracts.TaxBreakdown{}, fw.Violation("fiscal.no_jurisdiction", "no tax jurisdiction for "+a.Taxpayer.Country())
	}
	b, err := j.Calculate(ctx, a, e)
	if err != nil {
		return contracts.TaxBreakdown{}, err
	}
	out := contracts.TaxBreakdown{Country: j.Country(), Lines: []contracts.TaxLine{}, Net: b.Net.StringFixed(2), Tax: b.Tax.StringFixed(2),
		Surcharge: b.Surcharge.StringFixed(2)}
	for _, l := range b.Lines {
		tl := contracts.TaxLine{TaxCode: l.TaxCode, Treatment: l.Treatment, TreatmentKind: l.TreatmentKind.String(), Rate: l.Rate.StringFixed(2),
			Base: l.Base.StringFixed(2), Amount: l.Amount.StringFixed(2), SurchargeRate: decimalText(l.SurchargeRate),
			SurchargeAmount: decimalText(l.SurchargeAmount)}
		if l.TaxType != 0 {
			tl.TaxType = l.TaxType.String()
		}
		out.Lines = append(out.Lines, tl)
	}
	return out, nil
}

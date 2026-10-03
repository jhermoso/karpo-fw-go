package es_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/jurisdictions/es"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

type book struct {
	rates      []*domain.TaxRate
	treatments []*domain.Treatment
}

func (b book) RateOn(_ context.Context, t domain.TaxType, in domain.Territory, code string, on vocab.Date) (*domain.TaxRate, bool, error) {
	for _, r := range b.rates {
		s := r.State()
		if s.Type == t && s.Territory == in && s.Code == code && r.InForceOn(on) {
			return r, true, nil
		}
	}
	return nil, false, nil
}

func (b book) Treatment(_ context.Context, in domain.Territory, code string) (*domain.Treatment, bool, error) {
	for _, t := range b.treatments {
		if t.State().Territory == in && t.State().Code == code {
			return t, true, nil
		}
	}
	return nil, false, nil
}

func dec(s string) vocab.Decimal { return vocab.MustDecimal(s) }

func rate(t domain.TaxType, in domain.Territory, code, pct, surcharge string) *domain.TaxRate {
	r, err := domain.ReconstituteTaxRate(domain.NewTaxRateID(), domain.TaxRateState{Type: t, Territory: in, Code: code, Description: code,
		Rate: dec(pct), Surcharge: dec(surcharge), From: vocab.MustDate(2012, 9, 1)})
	if err != nil {
		panic(err)
	}
	return r
}

func taxpayer(in domain.Territory) *domain.Taxpayer {
	t, _ := domain.RegisterTaxpayer(domain.NewTaxpayerID(), domain.OrganizationID{UUID: fw.NewUUID()},
		domain.TaxpayerTerms{Territory: in, FiscalYearStartMonth: 1})
	return t
}

func TestSpain_GeneralRegime(t *testing.T) {
	exempt, _ := domain.ReconstituteTreatment(domain.NewTreatmentID(), domain.TreatmentState{Territory: domain.Common, Code: "E1",
		Description: "Exenta art. 20", Kind: domain.Exempt, Active: true})
	b := book{rates: []*domain.TaxRate{rate(domain.VAT, domain.Common, "G21", "21", "5.2"), rate(domain.VAT, domain.Common, "R10", "10", "1.4"),
		rate(domain.IGIC, domain.Canaries, "G7", "7", "0")}, treatments: []*domain.Treatment{exempt}}
	ctx := context.Background()
	on := vocab.MustDate(2026, 9, 28)
	a := domain.Assessment{Taxpayer: taxpayer(domain.Common), Date: on, Lines: []domain.TaxableLine{
		{Base: dec("10.03"), TaxCode: "G21"}, {Base: dec("10.03"), TaxCode: "g21"}, {Base: dec("10.03"), TaxCode: "G21"},
		{Base: dec("100"), TaxCode: "R10"}, {Base: dec("50"), Treatment: "E1"},
	}}
	got, err := es.Spain{}.Calculate(ctx, a, b)
	if err != nil {
		t.Fatal(err)
	}
	// 30.09 × 21 % = 6.3189 → 6.32 on the aggregated base (line by line it would be 3 × 2.11 = 6.33).
	if len(got.Lines) != 3 || !got.Lines[0].Base.Equal(dec("30.09")) || !got.Lines[0].Amount.Equal(dec("6.32")) ||
		!got.Lines[1].Amount.Equal(dec("10")) || got.Lines[2].TreatmentKind != domain.Exempt || !got.Lines[2].Amount.IsZero() ||
		!got.Net.Equal(dec("180.09")) || !got.Tax.Equal(dec("16.32")) || !got.Surcharge.IsZero() {
		t.Fatalf("breakdown: %+v", got)
	}

	a.EquivalenceSurcharge = true
	got, _ = es.Spain{}.Calculate(ctx, a, b)
	// 30.09 × 5.2 % = 1.56468 → 1.56; 100 × 1.4 % = 1.40.
	if !got.Surcharge.Equal(dec("2.96")) || !got.Lines[0].SurchargeAmount.Equal(dec("1.56")) {
		t.Fatalf("surcharge: %+v", got)
	}

	canary := domain.Assessment{Taxpayer: taxpayer(domain.Canaries), Date: on, Lines: []domain.TaxableLine{{Base: dec("100"), TaxCode: "G7"}}}
	if got, err := (es.Spain{}).Calculate(ctx, canary, b); err != nil || got.Lines[0].TaxType != domain.IGIC || !got.Tax.Equal(dec("7")) {
		t.Fatalf("IGIC in the Canaries: %+v %v", got, err)
	}
	canary.Lines[0].TaxCode = "G21"
	if _, err := (es.Spain{}).Calculate(ctx, canary, b); !isViolation(err, "fiscal.unknown_rate") {
		t.Fatalf("no VAT in the Canaries: %v", err)
	}
	bad := []domain.TaxableLine{{Base: dec("1.005"), TaxCode: "G21"}}
	if _, err := (es.Spain{}).Calculate(ctx, domain.Assessment{Taxpayer: a.Taxpayer, Date: on, Lines: bad}, b); !isViolation(err, "fiscal.base_not_rounded") {
		t.Fatal(err)
	}
	for code, line := range map[string]domain.TaxableLine{
		"fiscal.missing_tax_code":   {Base: dec("1")},
		"fiscal.unknown_treatment":  {Base: dec("1"), Treatment: "E9"},
		"fiscal.tax_code_on_exempt": {Base: dec("1"), TaxCode: "G21", Treatment: "E1"},
		"fiscal.unknown_rate":       {Base: dec("1"), TaxCode: "X"},
	} {
		if _, err := (es.Spain{}).Calculate(ctx, domain.Assessment{Taxpayer: a.Taxpayer, Date: on, Lines: []domain.TaxableLine{line}}, b); !isViolation(err, code) {
			t.Fatalf("%s: %v", code, err)
		}
	}
	past := domain.Assessment{Taxpayer: a.Taxpayer, Date: vocab.MustDate(2012, 8, 31), Lines: []domain.TaxableLine{{Base: dec("1"), TaxCode: "G21"}}}
	if _, err := (es.Spain{}).Calculate(ctx, past, b); !isViolation(err, "fiscal.unknown_rate") {
		t.Fatal("the rate was not in force yet")
	}
	// Credit notes (by differences) have negative bases.
	neg := domain.Assessment{Taxpayer: a.Taxpayer, Date: on, Lines: []domain.TaxableLine{{Base: dec("-10.03"), TaxCode: "G21"}}}
	if got, err := (es.Spain{}).Calculate(ctx, neg, b); err != nil || !got.Tax.Equal(dec("-2.11")) {
		t.Fatalf("negative base: %+v %v", got, err)
	}
}

func isViolation(err error, code string) bool {
	var rv *fw.RuleViolationError
	return errors.As(err, &rv) && rv.Code == code
}

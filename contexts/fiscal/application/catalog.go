package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CreateRate adds a tax rate.
type CreateRate struct {
	Type        string     `json:"type"`
	Territory   string     `json:"territory"`
	Code        string     `json:"code"`
	Description string     `json:"description"`
	Rate        string     `json:"rate"`
	Surcharge   string     `json:"surcharge,omitempty"`
	From        vocab.Date `json:"from"`
	Until       vocab.Date `json:"until,omitzero"`
}

// EndRate closes the validity of a rate.
type EndRate struct {
	ID domain.TaxRateID `json:"-"`
	On vocab.Date       `json:"on"`
}

// SearchRates lists rates, optionally of a type and territory and in force on a date.
type SearchRates struct{ Type, Territory, On string }

// RateDTO is the transport form of a rate.
type RateDTO struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Territory   string `json:"territory"`
	Code        string `json:"code"`
	Description string `json:"description"`
	Rate        string `json:"rate"`
	Surcharge   string `json:"surcharge,omitempty"`
	From        string `json:"from"`
	Until       string `json:"until,omitempty"`
}

func rateDTO(r *domain.TaxRate) RateDTO {
	s := r.State()
	return RateDTO{ID: r.ID().String(), Type: s.Type.String(), Territory: s.Territory.String(), Code: s.Code, Description: s.Description,
		Rate: s.Rate.StringFixed(2), Surcharge: decimalText(s.Surcharge), From: s.From.String(), Until: dateText(s.Until)}
}

// CreateTreatment adds a tax treatment.
type CreateTreatment struct {
	Territory   string `json:"territory"`
	Code        string `json:"code"`
	Description string `json:"description"`
	Kind        string `json:"kind"` // subject | exempt | not-subject
}

// RetireTreatment deactivates a treatment.
type RetireTreatment struct {
	ID domain.TreatmentID `json:"-"`
}

// SearchTreatments lists the treatments of a territory.
type SearchTreatments struct {
	Territory  string
	ActiveOnly bool
}

// TreatmentDTO is the transport form of a treatment.
type TreatmentDTO struct {
	ID          string `json:"id"`
	Territory   string `json:"territory"`
	Code        string `json:"code"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Active      bool   `json:"active"`
}

func treatmentDTO(t *domain.Treatment) TreatmentDTO {
	s := t.State()
	return TreatmentDTO{ID: t.ID().String(), Territory: s.Territory.String(), Code: s.Code, Description: s.Description, Kind: s.Kind.String(), Active: s.Active}
}

func (s service) catalogUseCases(svc *Service) {
	svc.CreateRate = guard(PermCatalogUpdate, func(ctx context.Context, c CreateRate) (RateDTO, error) {
		var v fw.Validation
		typ, ok1 := domain.ParseTaxType(c.Type)
		terr, ok2 := domain.ParseTerritory(c.Territory)
		v.Require(ok1, "type", "enum", "vat, igic or ipsi")
		v.Require(ok2, "territory", "enum", "unknown territory")
		st := domain.TaxRateState{Type: typ, Territory: terr, Code: c.Code, Description: c.Description, Rate: parseDecimal(&v, "rate", c.Rate),
			Surcharge: parseDecimal(&v, "surcharge", c.Surcharge), From: c.From, Until: c.Until}
		if err := v.Err(); err != nil {
			return RateDTO{}, err
		}
		r, err := domain.ReconstituteTaxRate(domain.NewTaxRateID(), st)
		if err != nil {
			return RateDTO{}, err
		}
		same, err := s.Rates.Find(ctx, spec.And(domain.RateFieldType.Eq(int(typ)), domain.RateFieldTerritory.Eq(int(terr)), domain.RateFieldCode.Eq(r.State().Code)))
		if err != nil {
			return RateDTO{}, err
		}
		for _, o := range same {
			if o.Overlaps(r.State()) {
				return RateDTO{}, fw.Violation("fiscal.rate_overlaps", "the code already has a rate in force in that period: end it first")
			}
		}
		if err := s.rates.Create(ctx, r); err != nil {
			return RateDTO{}, err
		}
		return rateDTO(r), nil
	}, pipeline.Transactional[CreateRate, RateDTO](s.UoW))

	svc.EndRate = guard(PermCatalogUpdate, func(ctx context.Context, c EndRate) (RateDTO, error) {
		r, err := s.rates.Update(ctx, c.ID, func(_ context.Context, r *domain.TaxRate) error { return r.End(c.On) })
		if err != nil {
			return RateDTO{}, err
		}
		return rateDTO(r), nil
	}, retry[EndRate, RateDTO]())

	svc.SearchRates = guard(PermCatalogRead, func(ctx context.Context, q SearchRates) ([]RateDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.TaxRate]{spec.All[*domain.TaxRate]()}
		if q.Type != "" {
			t, ok := domain.ParseTaxType(q.Type)
			v.Require(ok, "type", "enum", "vat, igic or ipsi")
			parts = append(parts, domain.RateFieldType.Eq(int(t)))
		}
		if q.Territory != "" {
			t, ok := domain.ParseTerritory(q.Territory)
			v.Require(ok, "territory", "enum", "unknown territory")
			parts = append(parts, domain.RateFieldTerritory.Eq(int(t)))
		}
		var on vocab.Date
		if q.On != "" {
			d, err := vocab.ParseDate(q.On)
			v.Require(err == nil, "on", "format", "a date YYYY-MM-DD is required")
			on = d
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		rs, err := s.Rates.Find(ctx, spec.And(parts...), domain.RateFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []RateDTO{}
		for _, r := range rs {
			if on.IsZero() || r.InForceOn(on) {
				out = append(out, rateDTO(r))
			}
		}
		return out, nil
	})

	svc.CreateTreatment = guard(PermCatalogUpdate, func(ctx context.Context, c CreateTreatment) (TreatmentDTO, error) {
		var v fw.Validation
		terr, ok1 := domain.ParseTerritory(c.Territory)
		kind, ok2 := domain.ParseTreatmentKind(c.Kind)
		v.Require(ok1, "territory", "enum", "unknown territory")
		v.Require(ok2, "kind", "enum", "subject, exempt or not-subject")
		if err := v.Err(); err != nil {
			return TreatmentDTO{}, err
		}
		t, err := domain.ReconstituteTreatment(domain.NewTreatmentID(), domain.TreatmentState{Territory: terr, Code: c.Code,
			Description: c.Description, Kind: kind, Active: true})
		if err != nil {
			return TreatmentDTO{}, err
		}
		dup, err := s.Treatments.Exists(ctx, spec.And(domain.TreatFieldTerr.Eq(int(terr)), domain.TreatFieldCode.Eq(t.State().Code),
			domain.TreatFieldActive.Eq(true)))
		if err != nil {
			return TreatmentDTO{}, err
		}
		if dup {
			return TreatmentDTO{}, fw.Violation("fiscal.duplicate_treatment", "the territory already has an active treatment with that code")
		}
		if err := s.treatments.Create(ctx, t); err != nil {
			return TreatmentDTO{}, err
		}
		return treatmentDTO(t), nil
	}, pipeline.Transactional[CreateTreatment, TreatmentDTO](s.UoW))

	svc.RetireTreatment = guard(PermCatalogUpdate, func(ctx context.Context, c RetireTreatment) (TreatmentDTO, error) {
		t, err := s.treatments.Update(ctx, c.ID, func(_ context.Context, t *domain.Treatment) error { t.Deactivate(); return nil })
		if err != nil {
			return TreatmentDTO{}, err
		}
		return treatmentDTO(t), nil
	}, retry[RetireTreatment, TreatmentDTO]())

	svc.SearchTreatments = guard(PermCatalogRead, func(ctx context.Context, q SearchTreatments) ([]TreatmentDTO, error) {
		parts := []spec.Specification[*domain.Treatment]{spec.All[*domain.Treatment]()}
		if q.Territory != "" {
			t, ok := domain.ParseTerritory(q.Territory)
			if !ok {
				var v fw.Validation
				v.Add("territory", "enum", "unknown territory")
				return nil, v.Err()
			}
			parts = append(parts, domain.TreatFieldTerr.Eq(int(t)))
		}
		if q.ActiveOnly {
			parts = append(parts, domain.TreatFieldActive.Eq(true))
		}
		ts, err := s.Treatments.Find(ctx, spec.And(parts...), domain.TreatFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []TreatmentDTO{}
		for _, t := range ts {
			out = append(out, treatmentDTO(t))
		}
		return out, nil
	})
}

// RateLookup implements contracts.Rates.
type RateLookup struct{ Rates domain.TaxRateRepository }

var _ contracts.Rates = RateLookup{}

// RateOn implements contracts.Rates.
func (l RateLookup) RateOn(ctx context.Context, taxType, territory, code, date string) (contracts.RateRef, bool, error) {
	typ, ok1 := domain.ParseTaxType(taxType)
	terr, ok2 := domain.ParseTerritory(territory)
	on, err := vocab.ParseDate(date)
	if !ok1 || !ok2 || err != nil {
		var v fw.Validation
		v.Add("rate", "query", "a tax type, a territory and a date YYYY-MM-DD are required")
		return contracts.RateRef{}, false, v.Err()
	}
	c := domain.NormalizeCode(code)
	rs, err := l.Rates.Find(ctx, spec.And(domain.RateFieldType.Eq(int(typ)), domain.RateFieldTerritory.Eq(int(terr)), domain.RateFieldCode.Eq(c)))
	if err != nil {
		return contracts.RateRef{}, false, err
	}
	for _, r := range rs {
		if r.InForceOn(on) {
			d := rateDTO(r)
			return contracts.RateRef{ID: d.ID, Type: d.Type, Territory: d.Territory, Code: d.Code, Rate: d.Rate, Surcharge: d.Surcharge}, true, nil
		}
	}
	return contracts.RateRef{}, false, nil
}

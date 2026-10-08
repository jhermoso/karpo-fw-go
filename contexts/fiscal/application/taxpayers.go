package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// TermsInput are the editable terms of a taxpayer.
type TermsInput struct {
	Territory             string `json:"territory"`
	FiscalYearStartMonth  int    `json:"fiscalYearStartMonth"`
	GeneralProrata        string `json:"generalProrata,omitempty"`
	DifferentiatedSectors bool   `json:"differentiatedSectors,omitempty"`
}

// RegisterTaxpayer creates the fiscal profile of an internal organization.
type RegisterTaxpayer struct {
	Organization string `json:"organization"`
	TermsInput
}

// SetTaxpayerTerms replaces the terms of a taxpayer.
type SetTaxpayerTerms struct {
	ID domain.TaxpayerID `json:"-"`
	TermsInput
}

// AddActivity adds an economic activity.
type AddActivity struct {
	ID               domain.TaxpayerID `json:"-"`
	Code             string            `json:"code"`
	IAE              string            `json:"iae,omitempty"`
	Category         string            `json:"category"`
	Description      string            `json:"description"`
	From             vocab.Date        `json:"from"`
	Until            vocab.Date        `json:"until,omitzero"`
	Primary          bool              `json:"primary,omitempty"`
	VATDeductible    bool              `json:"vatDeductible,omitempty"`
	EstimationRegime string            `json:"estimationRegime,omitempty"`
}

// EndActivity ends an activity.
type EndActivity struct {
	ID       domain.TaxpayerID `json:"-"`
	Activity string            `json:"activity"`
	On       vocab.Date        `json:"on"`
}

// AddObligation registers a form to file.
type AddObligation struct {
	ID          domain.TaxpayerID `json:"-"`
	Form        string            `json:"form"`
	Periodicity string            `json:"periodicity"`
	FromYear    int               `json:"fromYear"`
	UntilYear   int               `json:"untilYear,omitempty"`
}

// EndObligation ends an obligation after a year.
type EndObligation struct {
	ID         domain.TaxpayerID `json:"-"`
	Obligation string            `json:"obligation"`
	LastYear   int               `json:"lastYear"`
}

// GetTaxpayer loads a taxpayer by id or by organization.
type GetTaxpayer struct {
	ID           domain.TaxpayerID
	Organization string
}

// ActivityDTO is the transport form of an activity.
type ActivityDTO struct {
	ID               string `json:"id"`
	Code             string `json:"code"`
	IAE              string `json:"iae,omitempty"`
	Category         string `json:"category"`
	Description      string `json:"description"`
	From             string `json:"from"`
	Until            string `json:"until,omitempty"`
	Primary          bool   `json:"primary"`
	VATDeductible    bool   `json:"vatDeductible"`
	EstimationRegime string `json:"estimationRegime,omitempty"`
}

// ObligationDTO is the transport form of an obligation.
type ObligationDTO struct {
	ID          string `json:"id"`
	Form        string `json:"form"`
	Periodicity string `json:"periodicity"`
	FromYear    int    `json:"fromYear"`
	UntilYear   int    `json:"untilYear,omitempty"`
}

// TaxpayerDTO is the transport form of a taxpayer.
type TaxpayerDTO struct {
	ID                    string          `json:"id"`
	Organization          string          `json:"organization"`
	Territory             string          `json:"territory"`
	FiscalYearStartMonth  int             `json:"fiscalYearStartMonth"`
	GeneralProrata        string          `json:"generalProrata,omitempty"`
	DifferentiatedSectors bool            `json:"differentiatedSectors"`
	Activities            []ActivityDTO   `json:"activities"`
	Obligations           []ObligationDTO `json:"obligations"`
	Version               int64           `json:"version"`
}

func taxpayerDTO(t *domain.Taxpayer) TaxpayerDTO {
	tm := t.Terms()
	d := TaxpayerDTO{ID: t.ID().String(), Organization: t.Organization().String(), Territory: tm.Territory.String(),
		FiscalYearStartMonth: tm.FiscalYearStartMonth, GeneralProrata: decimalText(tm.GeneralProrata), DifferentiatedSectors: tm.DifferentiatedSectors,
		Activities: []ActivityDTO{}, Obligations: []ObligationDTO{}, Version: t.Version()}
	for _, a := range t.Activities() {
		d.Activities = append(d.Activities, ActivityDTO{ID: a.ID.String(), Code: a.Code, IAE: a.IAE, Category: a.Category.String(),
			Description: a.Description, From: a.From.String(), Until: dateText(a.Until), Primary: a.Primary, VATDeductible: a.VATDeductible,
			EstimationRegime: a.EstimationRegime})
	}
	for _, o := range t.Obligations() {
		d.Obligations = append(d.Obligations, ObligationDTO{ID: o.ID.String(), Form: string(o.Form), Periodicity: o.Periodicity.String(),
			FromYear: o.FromYear, UntilYear: o.UntilYear})
	}
	return d
}

func parseTerms(in TermsInput) (domain.TaxpayerTerms, error) {
	var v fw.Validation
	terr, ok := domain.ParseTerritory(in.Territory)
	v.Require(ok, "territory", "enum", "unknown territory")
	t := domain.TaxpayerTerms{Territory: terr, FiscalYearStartMonth: in.FiscalYearStartMonth,
		GeneralProrata: parseDecimal(&v, "generalProrata", in.GeneralProrata), DifferentiatedSectors: in.DifferentiatedSectors}
	return t, v.Err()
}

func (s service) taxpayerUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.TaxpayerID, fn func(*domain.Taxpayer) error) (TaxpayerDTO, error) {
		sc := scopeOf(ctx)
		t, err := s.taxpayers.Update(ctx, id, func(_ context.Context, t *domain.Taxpayer) error {
			if err := sc.check(domain.TaxpayerKind, t.ID(), t.Organization(), true); err != nil {
				return err
			}
			return fn(t)
		})
		if err != nil {
			return TaxpayerDTO{}, err
		}
		return taxpayerDTO(t), nil
	}

	svc.RegisterTaxpayer = guard(PermTaxpayerWrite, func(ctx context.Context, c RegisterTaxpayer) (TaxpayerDTO, error) {
		var v fw.Validation
		org := domain.OrganizationID{UUID: parseID(&v, "organization", c.Organization)}
		if err := v.Err(); err != nil {
			return TaxpayerDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", org, org, true); err != nil {
			return TaxpayerDTO{}, err
		}
		terms, err := parseTerms(c.TermsInput)
		if err != nil {
			return TaxpayerDTO{}, err
		}
		exists, err := s.Taxpayers.Exists(ctx, domain.TaxpayerFieldOrg.Eq(org))
		if err != nil {
			return TaxpayerDTO{}, err
		}
		if exists {
			return TaxpayerDTO{}, fw.Violation("fiscal.taxpayer_exists", "the organization already has a fiscal profile")
		}
		t, err := domain.RegisterTaxpayer(domain.NewTaxpayerID(), org, terms)
		if err != nil {
			return TaxpayerDTO{}, err
		}
		if err := s.taxpayers.Create(ctx, t); err != nil {
			return TaxpayerDTO{}, err
		}
		return taxpayerDTO(t), nil
	}, pipeline.Transactional[RegisterTaxpayer, TaxpayerDTO](s.UoW))

	svc.SetTaxpayerTerms = guard(PermTaxpayerWrite, func(ctx context.Context, c SetTaxpayerTerms) (TaxpayerDTO, error) {
		terms, err := parseTerms(c.TermsInput)
		if err != nil {
			return TaxpayerDTO{}, err
		}
		return update(ctx, c.ID, func(t *domain.Taxpayer) error { return t.SetTerms(terms) })
	}, retry[SetTaxpayerTerms, TaxpayerDTO]())

	svc.AddActivity = guard(PermTaxpayerWrite, func(ctx context.Context, c AddActivity) (TaxpayerDTO, error) {
		cat, ok := domain.ParseActivityCategory(c.Category)
		if !ok {
			var v fw.Validation
			v.Add("category", "enum", "trade, industry, services, professional or other")
			return TaxpayerDTO{}, v.Err()
		}
		return update(ctx, c.ID, func(t *domain.Taxpayer) error {
			_, err := t.AddActivity(domain.Activity{Code: c.Code, IAE: c.IAE, Category: cat, Description: c.Description, From: c.From,
				Until: c.Until, Primary: c.Primary, VATDeductible: c.VATDeductible, EstimationRegime: c.EstimationRegime})
			return err
		})
	}, retry[AddActivity, TaxpayerDTO]())

	svc.EndActivity = guard(PermTaxpayerWrite, func(ctx context.Context, c EndActivity) (TaxpayerDTO, error) {
		var v fw.Validation
		id := domain.ActivityID{UUID: parseID(&v, "activity", c.Activity)}
		if err := v.Err(); err != nil {
			return TaxpayerDTO{}, err
		}
		return update(ctx, c.ID, func(t *domain.Taxpayer) error { return t.EndActivity(id, c.On) })
	}, retry[EndActivity, TaxpayerDTO]())

	svc.AddObligation = guard(PermTaxpayerWrite, func(ctx context.Context, c AddObligation) (TaxpayerDTO, error) {
		per, ok := domain.ParsePeriodicity(c.Periodicity)
		if !ok {
			var v fw.Validation
			v.Add("periodicity", "enum", "monthly, quarterly or annual")
			return TaxpayerDTO{}, v.Err()
		}
		return update(ctx, c.ID, func(t *domain.Taxpayer) error {
			_, err := t.AddObligation(domain.Obligation{Form: domain.Form(c.Form), Periodicity: per, FromYear: c.FromYear, UntilYear: c.UntilYear})
			return err
		})
	}, retry[AddObligation, TaxpayerDTO]())

	svc.EndObligation = guard(PermTaxpayerWrite, func(ctx context.Context, c EndObligation) (TaxpayerDTO, error) {
		var v fw.Validation
		id := domain.ObligationID{UUID: parseID(&v, "obligation", c.Obligation)}
		if err := v.Err(); err != nil {
			return TaxpayerDTO{}, err
		}
		return update(ctx, c.ID, func(t *domain.Taxpayer) error { return t.EndObligation(id, c.LastYear) })
	}, retry[EndObligation, TaxpayerDTO]())

	svc.GetTaxpayer = guard(PermTaxpayerRead, func(ctx context.Context, q GetTaxpayer) (TaxpayerDTO, error) {
		t, err := s.taxpayer(ctx, q)
		if err != nil {
			return TaxpayerDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.TaxpayerKind, t.ID(), t.Organization(), false); err != nil {
			return TaxpayerDTO{}, err
		}
		return taxpayerDTO(t), nil
	})
}

func (s service) taxpayer(ctx context.Context, q GetTaxpayer) (*domain.Taxpayer, error) {
	if q.Organization == "" {
		return s.Taxpayers.Get(ctx, q.ID)
	}
	var v fw.Validation
	org := domain.OrganizationID{UUID: parseID(&v, "organization", q.Organization)}
	if err := v.Err(); err != nil {
		return nil, err
	}
	ts, err := s.Taxpayers.Find(ctx, domain.TaxpayerFieldOrg.Eq(org))
	if err != nil {
		return nil, err
	}
	if len(ts) == 0 {
		return nil, fw.NotFound(domain.TaxpayerKind, org)
	}
	return ts[0], nil
}

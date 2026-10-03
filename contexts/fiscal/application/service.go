// Package application holds the Fiscal use cases (with permissions and organization scope), the
// Payroll subscription that feeds the withholding forms, the rate lookup for other contexts and
// the translation to its Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# fiscal endpoints required authentication only). The rate catalog is legal
// data shared by every company: changing it is a permission of its own.
var (
	PermCatalogRead   = authz.MustPermission("Fiscal.Catalog.Read")
	PermCatalogUpdate = authz.MustPermission("Fiscal.Catalog.Update")
	PermTaxpayerRead  = authz.MustPermission("Fiscal.Taxpayer.Read")
	PermTaxpayerWrite = authz.MustPermission("Fiscal.Taxpayer.Update")
	PermFilingRead    = authz.MustPermission("Fiscal.Filing.Read")
	PermFilingCreate  = authz.MustPermission("Fiscal.Filing.Create")
	PermFilingSubmit  = authz.MustPermission("Fiscal.Filing.Submit")
)

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Rates        domain.TaxRateRepository
	Treatments   domain.TreatmentRepository
	Taxpayers    domain.TaxpayerRepository
	Filings      domain.FilingRepository
	Counters     domain.CounterRepository
	Withholdings domain.WithholdingRepository
	Identities   domain.Identities
	UoW          fw.UnitOfWork
	Recorder     app.EventRecorder
	Audit        app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	CreateRate       app.CommandHandler[CreateRate, RateDTO]
	EndRate          app.CommandHandler[EndRate, RateDTO]
	SearchRates      app.QueryHandler[SearchRates, []RateDTO]
	CreateTreatment  app.CommandHandler[CreateTreatment, TreatmentDTO]
	RetireTreatment  app.CommandHandler[RetireTreatment, TreatmentDTO]
	SearchTreatments app.QueryHandler[SearchTreatments, []TreatmentDTO]

	RegisterTaxpayer app.CommandHandler[RegisterTaxpayer, TaxpayerDTO]
	SetTaxpayerTerms app.CommandHandler[SetTaxpayerTerms, TaxpayerDTO]
	AddActivity      app.CommandHandler[AddActivity, TaxpayerDTO]
	EndActivity      app.CommandHandler[EndActivity, TaxpayerDTO]
	AddObligation    app.CommandHandler[AddObligation, TaxpayerDTO]
	EndObligation    app.CommandHandler[EndObligation, TaxpayerDTO]
	GetTaxpayer      app.QueryHandler[GetTaxpayer, TaxpayerDTO]

	GenerateFiling app.CommandHandler[GenerateFiling, FilingDTO]
	SubmitFiling   app.CommandHandler[SubmitFiling, FilingDTO]
	RevertFiling   app.CommandHandler[RevertFiling, FilingDTO]
	DiscardFiling  app.CommandHandler[DiscardFiling, struct{}]
	GetFiling      app.QueryHandler[GetFiling, FilingDTO]
	SearchFilings  app.QueryHandler[SearchFilings, fw.Page[FilingDTO]]
}

// scope: taxpayers and filings belong to their organization; visible inside its scope (uniform
// 404 elsewhere) and writable with a Full grant on it.
type scope struct {
	global bool
	ac     *authz.Context
	orgs   []domain.OrganizationID
}

func scopeOf(ctx context.Context) scope {
	ac, ok := authz.FromContext(ctx)
	if !ok {
		return scope{}
	}
	s := scope{global: ac.GlobalAdmin, ac: ac}
	for _, u := range ac.EffectiveOrganizations {
		s.orgs = append(s.orgs, domain.OrganizationID{UUID: u})
	}
	return s
}

func (s scope) check(kind string, id fmt.Stringer, org domain.OrganizationID, write bool) error {
	if !s.global && !slices.Contains(s.orgs, org) {
		return fw.NotFound(kind, id)
	}
	if write && !s.global && (s.ac == nil || !s.ac.CanWrite(org.UUID)) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

func (s scope) within() spec.Spec[*domain.Filing] {
	switch {
	case s.global:
		return spec.All[*domain.Filing]()
	case len(s.orgs) == 0:
		return spec.None[*domain.Filing]()
	}
	return domain.FilFieldDeclarant.In(s.orgs...)
}

type service struct {
	Deps
	rates      *orchestration.Orchestrator[domain.TaxRateID, *domain.TaxRate]
	treatments *orchestration.Orchestrator[domain.TreatmentID, *domain.Treatment]
	taxpayers  *orchestration.Orchestrator[domain.TaxpayerID, *domain.Taxpayer]
	filings    *orchestration.Orchestrator[domain.FilingID, *domain.Filing]
}

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

func parseDecimal(v *fw.Validation, field, s string) vocab.Decimal {
	if s == "" {
		return vocab.DecimalFromInt(0)
	}
	d, err := vocab.ParseDecimal(s)
	v.Require(err == nil, field, "format", field+" must be a decimal number")
	return d
}

func decimalText(d vocab.Decimal) string {
	if d.IsZero() {
		return ""
	}
	return d.StringFixed(2)
}

func dateText(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	s := service{Deps: d,
		rates:      orchestration.New[domain.TaxRateID, *domain.TaxRate](d.Rates, d.UoW, opts...),
		treatments: orchestration.New[domain.TreatmentID, *domain.Treatment](d.Treatments, d.UoW, opts...),
		taxpayers:  orchestration.New[domain.TaxpayerID, *domain.Taxpayer](d.Taxpayers, d.UoW, opts...),
		filings:    orchestration.New[domain.FilingID, *domain.Filing](d.Filings, d.UoW, opts...),
	}
	svc := &Service{}
	s.catalogUseCases(svc)
	s.taxpayerUseCases(svc)
	s.filingUseCases(svc)
	return svc
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond)
}

// Package application contiene los casos de uso de Receivables (con permisos y ámbito de vendedor),
// la suscripción a Billing que abre los cobros pendientes, el puerto Credit y la traducción a su
// Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permisos (los endpoints de pagos del C# sólo pedían autenticación).
var (
	PermTermsRead       = authz.MustPermission("Receivables.Terms.Read")
	PermTermsUpdate     = authz.MustPermission("Receivables.Terms.Update")
	PermCreditRead      = authz.MustPermission("Receivables.Credit.Read")
	PermCreditUpdate    = authz.MustPermission("Receivables.Credit.Update")
	PermReceivableRead  = authz.MustPermission("Receivables.Receivable.Read")
	PermCollectionRead  = authz.MustPermission("Receivables.Collection.Read")
	PermCollectionWrite = authz.MustPermission("Receivables.Collection.Update")
)

// Deps son los puertos que necesitan los casos de uso; Recorder, Audit y Calendar son opcionales.
type Deps struct {
	Terms       domain.TermsRepository
	Receivables domain.ReceivableRepository
	Collections domain.CollectionRepository
	Credit      domain.CreditProfileRepository
	Calendar    domain.Calendar
	Merged      app.MergedParties // opcional: las parties fusionadas, que los listados dejan fuera (D-100)
	UoW         fw.UnitOfWork
	Recorder    app.EventRecorder
	Audit       app.AuditLog
}

// withoutMerged deja fuera de un listado los registros cuya party (cliente o pagador) es una party fusionada,
// salvo que se pidan con includeMergedParties (docs/DECISIONES-COMUNES.md, D-100).
func withoutMerged[T any](ctx context.Context, m app.MergedParties, include bool, f spec.Field[T, domain.PartyID]) ([]spec.Specification[T], error) {
	return app.WithoutMerged(ctx, m, include, f, func(u fw.UUID) domain.PartyID { return domain.PartyID{UUID: u} })
}

// Service expone los casos de uso.
type Service struct {
	CreateTerms app.CommandHandler[CreateTerms, TermsDTO]
	RetireTerms app.CommandHandler[RetireTerms, TermsDTO]
	SearchTerms app.QueryHandler[SearchTerms, []TermsDTO]
	Preview     app.QueryHandler[PreviewSchedule, []DueDTO]

	SetCredit    app.CommandHandler[SetCredit, CreditDTO]
	SearchCredit app.QueryHandler[SearchCredit, []CreditDTO]
	GetExposure  app.QueryHandler[GetExposure, contracts.Exposure]

	GetReceivable     app.QueryHandler[GetReceivable, ReceivableDTO]
	SearchReceivables app.QueryHandler[SearchReceivables, fw.Page[ReceivableDTO]]

	RegisterCollection app.CommandHandler[RegisterCollection, CollectionDTO]
	Allocate           app.CommandHandler[Allocate, CollectionDTO]
	Deallocate         app.CommandHandler[Deallocate, CollectionDTO]
	CancelCollection   app.CommandHandler[CancelCollection, CollectionDTO]
	Offset             app.CommandHandler[OffsetCredit, CollectionDTO]
	GetCollection      app.QueryHandler[GetCollection, CollectionDTO]
	SearchCollections  app.QueryHandler[SearchCollections, fw.Page[CollectionDTO]]
}

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

// check devuelve un 404 uniforme fuera del ámbito del vendedor y 403 cuando es de sólo lectura.
func (s scope) check(kind string, id fmt.Stringer, org domain.OrganizationID, write bool) error {
	if !s.global && !slices.Contains(s.orgs, org) {
		return fw.NotFound(kind, id)
	}
	if write && !s.global && (s.ac == nil || !s.ac.CanWrite(org.UUID)) {
		return fmt.Errorf("%w: read-only in your organization scope", fw.ErrForbidden)
	}
	return nil
}

func within[T any](s scope, field spec.Field[T, domain.OrganizationID]) spec.Spec[T] {
	switch {
	case s.global:
		return spec.All[T]()
	case len(s.orgs) == 0:
		return spec.None[T]()
	}
	return field.In(s.orgs...)
}

type service struct {
	Deps
	terms       *orchestration.Orchestrator[domain.TermsID, *domain.Terms]
	receivables *orchestration.Orchestrator[domain.ReceivableID, *domain.Receivable]
	collections *orchestration.Orchestrator[domain.CollectionID, *domain.Collection]
	credit      *orchestration.Orchestrator[domain.CreditProfileID, *domain.CreditProfile]
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

func money(d vocab.Decimal) string { return d.StringFixed(2) }

func newOrchestrators(d Deps) service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	if d.Calendar == nil {
		d.Calendar = domain.NoHolidays{}
	}
	return service{Deps: d,
		terms:       orchestration.New[domain.TermsID, *domain.Terms](d.Terms, d.UoW, opts...),
		receivables: orchestration.New[domain.ReceivableID, *domain.Receivable](d.Receivables, d.UoW, opts...),
		collections: orchestration.New[domain.CollectionID, *domain.Collection](d.Collections, d.UoW, opts...),
		credit:      orchestration.New[domain.CreditProfileID, *domain.CreditProfile](d.Credit, d.UoW, opts...),
	}
}

// NewService conecta los casos de uso.
func NewService(d Deps) *Service {
	s := newOrchestrators(d)
	svc := &Service{}
	s.termsUseCases(svc)
	s.receivableUseCases(svc)
	s.collectionUseCases(svc)
	return svc
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond)
}

// Publications traduce los eventos de dominio al Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.CollectionAllocated) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.CollectionAllocatedV1{CollectionID: e.AggregateID, Seller: e.Seller, Payer: e.Payer, Method: e.Method,
			InvoiceID: e.Receivable, Installment: e.Installment, Amount: e.Amount, On: e.On}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.AllocationReversed) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.AllocationReversedV1{CollectionID: e.AggregateID, Seller: e.Seller, Payer: e.Payer,
			InvoiceID: e.Receivable, Installment: e.Installment, Amount: e.Amount}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.ReceivableSettled) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.ReceivableSettledV1{InvoiceID: e.AggregateID, Number: e.Number, Customer: e.Customer}}, nil
	})
	return r
}

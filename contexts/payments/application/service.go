// Package application holds the Payments use cases (with permissions and company scope), the
// subscriptions that turn payslips and tax forms into payables and executed transfers into
// payments, the port Treasury reads and the translation to the Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/payments/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# generated 12 Payments.* codes and enforced none). Maintaining what is owed
// and paying it are separate permissions.
var (
	PermPayableRead  = authz.MustPermission("Payments.Payable.Read")
	PermPayableWrite = authz.MustPermission("Payments.Payable.Update")
	PermPaymentRead  = authz.MustPermission("Payments.Payment.Read")
	PermPaymentWrite = authz.MustPermission("Payments.Payment.Update")
)

// Deps are the ports the use cases need; Recorder, Audit and NetPay are optional.
type Deps struct {
	Payables domain.PayableRepository
	Payments domain.PaymentRepository
	NetPay   domain.NetPaySplits
	UoW      fw.UnitOfWork
	Recorder app.EventRecorder
	Audit    app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	SetPayTo       app.CommandHandler[SetPayTo, PayableDTO]
	CancelPayable  app.CommandHandler[CancelPayable, PayableDTO]
	GetPayable     app.QueryHandler[GetPayable, PayableDTO]
	SearchPayables app.QueryHandler[SearchPayables, fw.Page[PayableDTO]]

	RegisterPayment app.CommandHandler[RegisterPayment, PaymentDTO]
	Allocate        app.CommandHandler[Allocate, PaymentDTO]
	Deallocate      app.CommandHandler[Deallocate, PaymentDTO]
	CancelPayment   app.CommandHandler[CancelPayment, PaymentDTO]
	GetPayment      app.QueryHandler[GetPayment, PaymentDTO]
	SearchPayments  app.QueryHandler[SearchPayments, fw.Page[PaymentDTO]]
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

// check returns a uniform 404 outside the company's scope and 403 when it is read-only.
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
	payables *orchestration.Orchestrator[domain.PayableID, *domain.Payable]
	payments *orchestration.Orchestrator[domain.PaymentID, *domain.Payment]
}

func newService(d Deps) service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	return service{Deps: d,
		payables: orchestration.New[domain.PayableID, *domain.Payable](d.Payables, d.UoW, opts...),
		payments: orchestration.New[domain.PaymentID, *domain.Payment](d.Payments, d.UoW, opts...),
	}
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	s := newService(d)
	svc := &Service{}
	s.payableUseCases(svc)
	s.paymentUseCases(svc)
	return svc
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

var euro = vocab.MustCurrencyCode("EUR")

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond)
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.PaymentAllocated) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.PaymentAllocatedV1{PaymentID: e.AggregateID, Company: e.Company, Payee: e.Payee, Method: e.Method,
			PayableID: e.Payable, Kind: e.Kind, Amount: e.Amount, On: e.On}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.AllocationReversed) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.AllocationReversedV1{PaymentID: e.AggregateID, Company: e.Company, Payee: e.Payee,
			PayableID: e.Payable, Kind: e.Kind, Amount: e.Amount}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.PayableSettled) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.PayableSettledV1{PayableID: e.AggregateID, Company: e.Company, Kind: e.Kind, Document: e.Document}}, nil
	})
	return r
}

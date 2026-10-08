// Package application holds the Purchases use cases (with permissions and company scope) and the
// translation to its Published Language.
package application

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/purchases/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# defined no purchase permission at all). Booking an invoice and annulling it
// are separate.
var (
	PermInvoiceRead     = authz.MustPermission("Purchases.Invoice.Read")
	PermInvoiceRegister = authz.MustPermission("Purchases.Invoice.Register")
	PermInvoiceCancel   = authz.MustPermission("Purchases.Invoice.Cancel")
	PermSupplierRead    = authz.MustPermission("Purchases.Supplier.Read")
	PermSupplierUpdate  = authz.MustPermission("Purchases.Supplier.Update")
)

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Invoices  domain.InvoiceRepository
	Suppliers domain.SupplierRepository
	Counters  domain.CounterRepository
	Taxes     domain.Taxes
	UoW       fw.UnitOfWork
	Recorder  app.EventRecorder
	Audit     app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	RegisterInvoice app.CommandHandler[RegisterInvoice, InvoiceDTO]
	CancelInvoice   app.CommandHandler[CancelInvoice, InvoiceDTO]
	GetInvoice      app.QueryHandler[GetInvoice, InvoiceDTO]
	SearchInvoices  app.QueryHandler[SearchInvoices, fw.Page[InvoiceDTO]]

	SetSupplier     app.CommandHandler[SetSupplier, SupplierDTO]
	SearchSuppliers app.QueryHandler[SearchSuppliers, []SupplierDTO]
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
	invoices  *orchestration.Orchestrator[domain.InvoiceID, *domain.Invoice]
	suppliers *orchestration.Orchestrator[domain.SupplierID, *domain.SupplierProfile]
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
		invoices:  orchestration.New[domain.InvoiceID, *domain.Invoice](d.Invoices, d.UoW, opts...),
		suppliers: orchestration.New[domain.SupplierID, *domain.SupplierProfile](d.Suppliers, d.UoW, opts...),
	}
	svc := &Service{}
	s.invoiceUseCases(svc)
	s.supplierUseCases(svc)
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

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func retry[In, Out any]() app.Middleware[In, Out] {
	return pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond)
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.InvoiceRegistered) ([]app.IntegrationEvent, error) {
		id, err := domain.ParseInvoiceID(e.AggregateID)
		if err != nil {
			return nil, err
		}
		inv, err := domain.ReconstituteInvoice(id, e.Snapshot)
		if err != nil {
			return nil, err
		}
		return []app.IntegrationEvent{registered(inv)}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.InvoiceCancelled) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.InvoiceCancelledV1{InvoiceID: e.AggregateID, Company: e.Company, Register: e.Register, Reason: e.Reason}}, nil
	})
	return r
}

func registered(inv *domain.Invoice) contracts.InvoiceRegisteredV1 {
	s := inv.State()
	expenses, deductible := inv.Expenses()
	out := contracts.InvoiceRegisteredV1{InvoiceID: inv.ID().String(), Company: s.Company.String(), Supplier: s.Supplier.String(),
		SupplierNumber: s.SupplierNumber, Register: s.Register, Issued: s.Issued.String(), Received: s.Received.String(), Due: s.Due.String(),
		Net: money(s.Breakdown.Net), Tax: money(s.Breakdown.Tax), DeductibleTax: money(deductible), Total: money(inv.Total()),
		WithholdingRate: money(s.WithholdingRate), Withholding: money(s.Withholding), Payable: money(inv.Payable()),
		Expenses: []contracts.Expense{}, Taxes: []contracts.Tax{}}
	if !s.Corrects.IsZero() {
		out.Corrects = s.Corrects.String()
	}
	if !s.Withholding.IsZero() {
		out.WithholdingKey = domain.ProfessionalKey
	}
	for _, e := range expenses {
		out.Expenses = append(out.Expenses, contracts.Expense{Category: string(e.Category), Amount: money(e.Amount)})
	}
	for _, t := range s.Breakdown.Lines {
		out.Taxes = append(out.Taxes, contracts.Tax{TaxCode: t.TaxCode, Treatment: t.Treatment, TreatmentKind: t.TreatmentKind, Rate: money(t.Rate),
			Base: money(t.Base), Amount: money(t.Amount)})
	}
	for _, p := range s.PayTo {
		out.PayTo = append(out.PayTo, contracts.PayTo{IBAN: p.IBAN.String(), Amount: money(p.Amount)})
	}
	return out
}

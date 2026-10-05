// Package application holds the Orders use cases (with permissions and company scope), the
// subscription to the stock Inventory holds and the translation to the Published Language.
package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permissions (the C# order, shipment and invoice endpoints required authentication only).
// Taking an order, committing it, serving it and withdrawing it are separate.
var (
	PermOrderRead    = authz.MustPermission("Orders.Order.Read")
	PermOrderUpdate  = authz.MustPermission("Orders.Order.Update")
	PermOrderConfirm = authz.MustPermission("Orders.Order.Confirm")
	PermOrderDeliver = authz.MustPermission("Orders.Order.Deliver")
	PermOrderCancel  = authz.MustPermission("Orders.Order.Cancel")
	PermTermsRead    = authz.MustPermission("Orders.Terms.Read")
	PermTermsUpdate  = authz.MustPermission("Orders.Terms.Update")
)

// Deps are the ports the use cases need; Credit, Recorder and Audit are optional.
type Deps struct {
	Orders     domain.OrderRepository
	Deliveries domain.DeliveryRepository
	Terms      domain.TermsRepository
	Counters   domain.CounterRepository
	Catalog    domain.Catalog
	Credit     domain.CreditCheck
	UoW        fw.UnitOfWork
	Recorder   app.EventRecorder
	Audit      app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	SetTerms    app.CommandHandler[SetTerms, TermsDTO]
	SearchTerms app.QueryHandler[SearchTerms, []TermsDTO]

	DraftOrder   app.CommandHandler[DraftOrder, OrderDTO]
	AddLine      app.CommandHandler[AddLine, OrderDTO]
	RemoveLine   app.CommandHandler[RemoveLine, OrderDTO]
	Confirm      app.CommandHandler[ConfirmOrder, OrderDTO]
	RequestStock app.CommandHandler[RequestStock, OrderDTO]
	Deliver      app.CommandHandler[Deliver, DeliveryDTO]
	Cancel       app.CommandHandler[EndOrder, OrderDTO]
	Close        app.CommandHandler[EndOrder, OrderDTO]
	GetOrder     app.QueryHandler[GetOrder, OrderDTO]
	SearchOrders app.QueryHandler[SearchOrders, fw.Page[OrderDTO]]

	GetDelivery      app.QueryHandler[GetDelivery, DeliveryDTO]
	SearchDeliveries app.QueryHandler[SearchDeliveries, fw.Page[DeliveryDTO]]
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
	orders     *orchestration.Orchestrator[domain.OrderID, *domain.Order]
	deliveries *orchestration.Orchestrator[domain.DeliveryID, *domain.Delivery]
	terms      *orchestration.Orchestrator[domain.TermsID, *domain.Terms]
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
		orders:     orchestration.New[domain.OrderID, *domain.Order](d.Orders, d.UoW, opts...),
		deliveries: orchestration.New[domain.DeliveryID, *domain.Delivery](d.Deliveries, d.UoW, opts...),
		terms:      orchestration.New[domain.TermsID, *domain.Terms](d.Terms, d.UoW, opts...),
	}
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	s := newService(d)
	svc := &Service{}
	s.termsUseCases(svc)
	s.orderUseCases(svc)
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

func optID(u fw.UUID) string {
	if u.IsZero() {
		return ""
	}
	return u.String()
}

func money(d vocab.Decimal) string { return d.StringFixed(2) }

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

// changing wraps a use case that changes aggregates: retried on a version conflict, in one unit of
// work.
func changing[In, Out any](s service, p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return guard(p, fn, pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond), pipeline.Transactional[In, Out](s.UoW))
}

// number takes the next number of a series of a company and year (same unit of work).
func (s service) number(ctx context.Context, company domain.OrganizationID, series string, year int) (string, error) {
	cs, err := s.Counters.Find(ctx, spec.And(domain.CntFieldCompany.Eq(company), domain.CntFieldSeries.Eq(series), domain.CntFieldYear.Eq(year)))
	if err != nil {
		return "", err
	}
	var c *domain.Counter
	if len(cs) > 0 {
		c = cs[0]
	} else if c, err = domain.ReconstituteCounter(domain.NewCounterID(), company, series, year, 0); err != nil {
		return "", err
	}
	n := c.Next()
	return n, s.Counters.Save(ctx, c)
}

// termsOf returns the terms of a customer (the zero terms when it has none).
func (s service) termsOf(ctx context.Context, company domain.OrganizationID, customer domain.PartyID) (domain.TermsState, error) {
	ts, err := s.Terms.Find(ctx, spec.And(domain.TrmFieldCompany.Eq(company), domain.TrmFieldCustomer.Eq(customer)))
	if err != nil || len(ts) == 0 {
		return domain.TermsState{Company: company, Customer: customer, Discount: vocab.DecimalFromInt(0)}, err
	}
	return ts[0].State(), nil
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.OrderConfirmed) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.OrderConfirmedV1{OrderID: e.AggregateID, Company: e.Company, Customer: e.Customer, Number: e.Number,
			Total: e.Total}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.StockRequested) ([]app.IntegrationEvent, error) {
		out := contracts.StockRequestedV1{OrderID: e.AggregateID, Company: e.Company, Warehouse: e.Warehouse}
		for _, l := range e.Lines {
			out.Lines = append(out.Lines, contracts.StockLine{Line: l.Line, Product: l.Product, Quantity: l.Quantity})
		}
		return []app.IntegrationEvent{out}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.OrderClosed) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.OrderClosedV1{OrderID: e.AggregateID, Company: e.Company, Status: e.Status, Reason: e.Reason,
			Lines: slices.Clone(e.Lines)}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.DeliveryIssued) ([]app.IntegrationEvent, error) {
		d := e.Snapshot
		out := contracts.DeliveryIssuedV1{DeliveryID: e.AggregateID, OrderID: d.Order.String(), OrderNumber: d.OrderNumber, Company: d.Company.String(),
			Customer: d.Customer.String(), Warehouse: optID(d.Warehouse.UUID), Number: d.Number, Date: d.Date.String()}
		total := vocab.DecimalFromInt(0)
		for _, l := range d.Lines {
			total = total.Add(l.Amount)
			out.Lines = append(out.Lines, contracts.DeliveryLine{Line: l.Line, Product: l.Product.String(), SKU: l.SKU, Description: l.Description, UoM: l.UoM,
				TaxCode: l.TaxCode, Stocked: l.Stocked, Quantity: l.Quantity.String(), NetPrice: l.NetPrice.StringFixed(4), Amount: money(l.Amount)})
		}
		out.Total = money(total)
		return []app.IntegrationEvent{out}, nil
	})
	return r
}

// OrderSource is the source type Inventory uses for the stock of sales orders; the source id is
// "order|line".
const OrderSource = "orders.sales-order"

// StockReserved is the Orders copy of inventory.stock-reserved.v1.
type StockReserved struct {
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
	Added      string `json:"added"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (StockReserved) IntegrationEventType() string { return "inventory.stock-reserved.v1" }

// DeliverySource is the source type Billing gives the invoices drafted from delivery notes.
const DeliverySource = "orders.delivery"

// InvoiceIssued is the Orders copy of billing.invoice-issued.v1.
type InvoiceIssued struct {
	InvoiceID  string `json:"invoiceId"`
	Number     string `json:"number"`
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (InvoiceIssued) IntegrationEventType() string { return "billing.invoice-issued.v1" }

// Subscribe records on each order line the stock Inventory holds for it (reservations of other
// sources are ignored). A line of an order that no longer waits ignores it too: its closure
// already asked Inventory to release what it holds. It also records on each delivery note the
// invoice Billing issued for it.
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	messaging.Handle(c, func(ctx context.Context, e InvoiceIssued, _ app.Envelope) error {
		if e.SourceType != DeliverySource {
			return nil
		}
		id, err := domain.ParseDeliveryID(e.SourceID)
		if err != nil {
			return fw.Violation("orders.invalid_event", "billing.invoice-issued.v1: "+e.SourceID)
		}
		_, err = s.deliveries.Update(ctx, id, func(_ context.Context, d *domain.Delivery) error { d.MarkInvoiced(e.InvoiceID, e.Number); return nil })
		if errors.Is(err, fw.ErrNotFound) {
			return nil
		}
		return err
	})
	messaging.Handle(c, func(ctx context.Context, e StockReserved, _ app.Envelope) error {
		if e.SourceType != OrderSource {
			return nil
		}
		order, line, ok := strings.Cut(e.SourceID, "|")
		id, err1 := domain.ParseOrderID(order)
		no, err2 := strconv.Atoi(line)
		added, err3 := vocab.ParseDecimal(e.Added)
		if err := errors.Join(err1, err2, err3); err != nil || !ok {
			return fw.Violation("orders.invalid_event", "inventory.stock-reserved.v1: "+e.SourceID)
		}
		_, err := s.orders.Update(ctx, id, func(_ context.Context, o *domain.Order) error { o.Hold(no, added); return nil })
		if errors.Is(err, fw.ErrNotFound) {
			return nil
		}
		return err
	})
}

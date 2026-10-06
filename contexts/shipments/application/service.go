// Package application holds the Shipments use cases (with permissions and company scope), the
// subscription to the delivery notes of Orders and the translation to the Published Language.
package application

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/shipments/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/shipments/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	"github.com/jhermoso/karpo-fw-go/pkg/application/orchestration"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// DeliverySource is the source type of the shipments planned from delivery notes of Orders.
const DeliverySource = "orders.delivery"

// Deps are the ports the use cases need; Recorder and Audit are optional.
type Deps struct {
	Shipments domain.ShipmentRepository
	Carriers  domain.CarrierRepository
	UoW       fw.UnitOfWork
	Recorder  app.EventRecorder
	Audit     app.AuditLog
}

// Service exposes the use cases.
type Service struct {
	Schedule        app.CommandHandler[ScheduleShipment, ShipmentDTO]
	Change          app.CommandHandler[ChangeShipment, ShipmentDTO]
	Move            app.CommandHandler[MoveShipment, ShipmentDTO]
	GetShipment     app.QueryHandler[GetShipment, ShipmentDTO]
	SearchShipments app.QueryHandler[SearchShipments, fw.Page[ShipmentDTO]]

	RegisterCarrier app.CommandHandler[RegisterCarrier, CarrierDTO]
	ChangeCarrier   app.CommandHandler[ChangeCarrier, CarrierDTO]
	SearchCarriers  app.QueryHandler[SearchCarriers, []CarrierDTO]
}

type service struct {
	Deps
	shipments *orchestration.Orchestrator[domain.ShipmentID, *domain.Shipment]
	carriers  *orchestration.Orchestrator[domain.CarrierID, *domain.Carrier]
}

func newService(d Deps) service {
	var opts []orchestration.Option
	if d.Recorder != nil {
		opts = append(opts, orchestration.WithOutbox(d.Recorder))
	}
	if d.Audit != nil {
		opts = append(opts, orchestration.WithAuditLog(d.Audit))
	}
	return service{Deps: d, shipments: orchestration.New[domain.ShipmentID, *domain.Shipment](d.Shipments, d.UoW, opts...),
		carriers: orchestration.New[domain.CarrierID, *domain.Carrier](d.Carriers, d.UoW, opts...)}
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

func parseID(v *fw.Validation, field, s string) fw.UUID {
	u, err := fw.ParseUUID(s)
	v.Require(err == nil && !u.IsZero(), field, "format", field+" must be an id")
	return u
}

func optionalID(v *fw.Validation, field, s string) fw.UUID {
	if s == "" {
		return fw.UUID{}
	}
	return parseID(v, field, s)
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

func dateText(d vocab.Date) string {
	if d.IsZero() {
		return ""
	}
	return d.String()
}

func orToday(d vocab.Date) vocab.Date {
	if d.IsZero() {
		return vocab.DateOf(fw.Now())
	}
	return d
}

func guard[In, Out any](p authz.Permission, fn func(context.Context, In) (Out, error), mw ...app.Middleware[In, Out]) app.Handler[In, Out] {
	return app.Chain[In, Out](app.HandlerFunc[In, Out](fn), append([]app.Middleware[In, Out]{pipeline.RequirePermission[In, Out](p)}, mw...)...)
}

func changing[In, Out any](uow fw.UnitOfWork, p authz.Permission, fn func(context.Context, In) (Out, error)) app.Handler[In, Out] {
	return guard(p, fn, pipeline.RetryOnConflict[In, Out](5, 10*time.Millisecond), pipeline.Transactional[In, Out](uow))
}

// NewService wires the use cases.
func NewService(d Deps) *Service {
	s := newService(d)
	svc := &Service{}
	s.wireShipments(svc)
	s.wireCarriers(svc)
	return svc
}

// DeliveryIssued is the Shipments copy of orders.delivery-issued.v1 (only the fields it needs).
type DeliveryIssued struct {
	DeliveryID string `json:"deliveryId"`
	Company    string `json:"company"`
	Customer   string `json:"customer"`
	Number     string `json:"number"`
	Date       string `json:"date"`
	Lines      []struct {
		SKU         string `json:"sku"`
		Description string `json:"description"`
		UoM         string `json:"uom"`
		Stocked     bool   `json:"stocked"`
		Quantity    string `json:"quantity"`
	} `json:"lines"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DeliveryIssued) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// Subscribe plans the shipment of each delivery note (Orders publishes, Shipments dispatches):
// the goods of the note, on its date, with nothing decided yet about how they travel. Services
// are not shipped: a delivery note without goods plans nothing. One shipment per delivery note.
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	messaging.Handle(c, func(ctx context.Context, e DeliveryIssued, _ app.Envelope) error {
		company, err1 := fw.ParseUUID(e.Company)
		customer, err2 := fw.ParseUUID(e.Customer)
		date, err3 := vocab.ParseDate(e.Date)
		if err := errors.Join(err1, err2, err3); err != nil || e.DeliveryID == "" {
			return fw.Violation("shipments.invalid_event", "orders.delivery-issued.v1: "+e.DeliveryID)
		}
		var lines []domain.Line
		for _, l := range e.Lines {
			if !l.Stocked {
				continue
			}
			q, err := vocab.ParseDecimal(l.Quantity)
			if err != nil {
				return fw.Violation("shipments.invalid_event", "orders.delivery-issued.v1: quantity "+l.Quantity)
			}
			lines = append(lines, domain.Line{SKU: l.SKU, Description: l.Description, UoM: l.UoM, Quantity: q})
		}
		if len(lines) == 0 {
			return nil
		}
		cid := domain.OrganizationID{UUID: company}
		done, err := s.Shipments.Exists(ctx, spec.And(domain.ShpFieldCompany.Eq(cid), domain.ShpFieldSrcType.Eq(DeliverySource),
			domain.ShpFieldSrcID.Eq(e.DeliveryID)))
		if err != nil || done {
			return err
		}
		shp, err := domain.ScheduleShipment(domain.NewShipmentID(), cid, domain.PartyID{UUID: customer},
			domain.Source{Type: DeliverySource, ID: e.DeliveryID, Ref: strings.TrimSpace(e.Number)}, domain.Plan{EstimatedShip: date}, lines, date)
		if err != nil {
			return fw.Violation("shipments.invalid_event", "orders.delivery-issued.v1: "+err.Error())
		}
		return s.shipments.Create(ctx, shp)
	})
}

// Publications translates the domain events into the Published Language.
func Publications(r *messaging.Recorder) *messaging.Recorder {
	messaging.On(r, func(_ context.Context, e domain.ShipmentDispatched) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.ShipmentDispatchedV1{ShipmentID: e.AggregateID, Company: e.Company, Customer: e.Customer,
			SourceType: e.SourceType, SourceID: e.SourceID, SourceRef: e.SourceRef, Method: e.Method, Carrier: e.Carrier, Tracking: e.Tracking,
			Packages: e.Packages, Date: e.Date}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.ShipmentDelivered) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.ShipmentDeliveredV1{ShipmentID: e.AggregateID, Company: e.Company, Customer: e.Customer,
			SourceType: e.SourceType, SourceID: e.SourceID, SourceRef: e.SourceRef, Date: e.Date, ReceivedBy: e.ReceivedBy}}, nil
	})
	messaging.On(r, func(_ context.Context, e domain.ShipmentReturned) ([]app.IntegrationEvent, error) {
		return []app.IntegrationEvent{contracts.ShipmentReturnedV1{ShipmentID: e.AggregateID, Company: e.Company, Customer: e.Customer,
			SourceType: e.SourceType, SourceID: e.SourceID, SourceRef: e.SourceRef, Date: e.Date, Reason: e.Reason}}, nil
	})
	return r
}

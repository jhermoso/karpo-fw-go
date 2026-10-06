package application

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/shipments/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// PlanInput is how a shipment travels.
type PlanInput struct {
	Method           string     `json:"method,omitempty"`
	Carrier          string     `json:"carrier,omitempty"`
	Tracking         string     `json:"tracking,omitempty"`
	Recipient        string     `json:"recipient,omitempty"`
	Destination      string     `json:"destination,omitempty"`
	Packages         int        `json:"packages,omitempty"`
	WeightKg         string     `json:"weightKg,omitempty"`
	Cost             string     `json:"cost,omitempty"`
	Instructions     string     `json:"instructions,omitempty"`
	EstimatedShip    vocab.Date `json:"estimatedShip,omitzero"`
	EstimatedArrival vocab.Date `json:"estimatedArrival,omitzero"`
}

func (p PlanInput) plan(v *fw.Validation) domain.Plan {
	return domain.Plan{Method: domain.Method(strings.TrimSpace(p.Method)), Carrier: domain.CarrierID{UUID: optionalID(v, "carrier", p.Carrier)},
		Tracking: p.Tracking, Recipient: p.Recipient, Destination: p.Destination, Packages: p.Packages, WeightKg: parseDecimal(v, "weightKg", p.WeightKg),
		Cost: parseDecimal(v, "cost", p.Cost), Instructions: p.Instructions, EstimatedShip: p.EstimatedShip, EstimatedArrival: p.EstimatedArrival}
}

// LineInput is something that travels in a shipment planned by hand.
type LineInput struct {
	SKU         string `json:"sku,omitempty"`
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UoM         string `json:"uom,omitempty"`
}

// ScheduleShipment plans by hand a shipment that no delivery note produced (samples, returns to
// a supplier, material lent).
type ScheduleShipment struct {
	Company  string `json:"company"`
	Customer string `json:"customer"`
	PlanInput
	Lines []LineInput `json:"lines"`
	On    vocab.Date  `json:"on,omitzero"` // today by default
}

// ChangeShipment replaces how a shipment travels.
type ChangeShipment struct {
	ID domain.ShipmentID `json:"-"`
	PlanInput
}

// MoveShipment moves the status: in-transit (dispatch), delivered, returned or cancelled.
type MoveShipment struct {
	ID         domain.ShipmentID `json:"-"`
	To         string            `json:"to"`
	On         vocab.Date        `json:"on,omitzero"` // today by default
	ReceivedBy string            `json:"receivedBy,omitempty"`
	Reason     string            `json:"reason,omitempty"`
}

// GetShipment loads a shipment with its lines and history.
type GetShipment struct{ ID domain.ShipmentID }

// SearchShipments searches the shipments of the caller's scope.
type SearchShipments struct {
	Company, Customer, Status, Carrier, Tracking, Delivery string
	Page, Size                                             int
}

// ShipmentLineDTO is the transport form of a line.
type ShipmentLineDTO struct {
	Line        int    `json:"line"`
	SKU         string `json:"sku,omitempty"`
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UoM         string `json:"uom,omitempty"`
}

// StepDTO is the transport form of a change of status.
type StepDTO struct {
	Status string `json:"status"`
	On     string `json:"on"`
}

// ShipmentDTO is the transport form of a shipment.
type ShipmentDTO struct {
	ID               string            `json:"id"`
	Company          string            `json:"company"`
	Customer         string            `json:"customer"`
	SourceType       string            `json:"sourceType,omitempty"`
	SourceID         string            `json:"sourceId,omitempty"`
	SourceRef        string            `json:"sourceRef,omitempty"`
	Method           string            `json:"method,omitempty"`
	Carrier          string            `json:"carrier,omitempty"`
	CarrierName      string            `json:"carrierName,omitempty"`
	Tracking         string            `json:"tracking,omitempty"`
	TrackingLink     string            `json:"trackingLink,omitempty"`
	Recipient        string            `json:"recipient,omitempty"`
	Destination      string            `json:"destination,omitempty"`
	Packages         int               `json:"packages"`
	WeightKg         string            `json:"weightKg"`
	Cost             string            `json:"cost"`
	Instructions     string            `json:"instructions,omitempty"`
	EstimatedShip    string            `json:"estimatedShip,omitempty"`
	EstimatedArrival string            `json:"estimatedArrival,omitempty"`
	Status           string            `json:"status"`
	Dispatched       string            `json:"dispatched,omitempty"`
	Closed           string            `json:"closed,omitempty"`
	ReceivedBy       string            `json:"receivedBy,omitempty"`
	Reason           string            `json:"reason,omitempty"`
	Lines            []ShipmentLineDTO `json:"lines,omitempty"`
	History          []StepDTO         `json:"history,omitempty"`
	Version          int64             `json:"version"`
}

func shipmentDTO(s *domain.Shipment, detail bool) ShipmentDTO {
	st := s.State()
	d := ShipmentDTO{ID: s.ID().String(), Company: st.Company.String(), Customer: st.Customer.String(), SourceType: st.Source.Type,
		SourceID: st.Source.ID, SourceRef: st.Source.Ref, Method: string(st.Method), Carrier: optID(st.Carrier.UUID), Tracking: st.Tracking,
		Recipient: st.Recipient, Destination: st.Destination, Packages: st.Packages, WeightKg: st.WeightKg.StringFixed(3), Cost: st.Cost.StringFixed(2),
		Instructions: st.Instructions, EstimatedShip: dateText(st.EstimatedShip), EstimatedArrival: dateText(st.EstimatedArrival),
		Status: string(st.Status), Dispatched: dateText(st.Dispatched), Closed: dateText(st.Closed), ReceivedBy: st.ReceivedBy, Reason: st.Reason,
		Version: s.Version()}
	if detail {
		for _, l := range st.Lines {
			d.Lines = append(d.Lines, ShipmentLineDTO{Line: l.No, SKU: l.SKU, Description: l.Description, Quantity: l.Quantity.String(), UoM: l.UoM})
		}
		for _, h := range st.History {
			d.History = append(d.History, StepDTO{Status: string(h.Status), On: h.On.String()})
		}
	}
	return d
}

// carrierOf loads the carrier of a plan and checks it may be used by the company.
func (s service) carrierOf(ctx context.Context, company domain.OrganizationID, id domain.CarrierID, usable bool) (*domain.Carrier, error) {
	if id.IsZero() {
		return nil, nil
	}
	c, err := s.Carriers.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if c.State().Company != company {
		return nil, fw.NotFound(domain.CarrierKind, id)
	}
	if usable && c.State().Blocked {
		return nil, fw.Violation("shipments.carrier_blocked", "the carrier is blocked")
	}
	return c, nil
}

func (s service) detail(ctx context.Context, shp *domain.Shipment) (ShipmentDTO, error) {
	d := shipmentDTO(shp, true)
	c, err := s.carrierOf(ctx, shp.State().Company, shp.State().Carrier, false)
	if err != nil {
		return ShipmentDTO{}, err
	}
	if c != nil {
		d.CarrierName, d.TrackingLink = c.State().Name, c.TrackingLink(shp.State().Tracking)
	}
	return d, nil
}

func (s service) wireShipments(svc *Service) {
	d := s.Deps
	svc.Schedule = changing(d.UoW, PermShipmentUpdate, func(ctx context.Context, c ScheduleShipment) (ShipmentDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		customer := domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}
		plan := c.plan(&v)
		var lines []domain.Line
		for _, l := range c.Lines {
			lines = append(lines, domain.Line{SKU: l.SKU, Description: l.Description, UoM: l.UoM, Quantity: parseDecimal(&v, "lines.quantity", l.Quantity)})
		}
		if err := v.Err(); err != nil {
			return ShipmentDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return ShipmentDTO{}, err
		}
		if _, err := s.carrierOf(ctx, company, plan.Carrier, true); err != nil {
			return ShipmentDTO{}, err
		}
		shp, err := domain.ScheduleShipment(domain.NewShipmentID(), company, customer, domain.Source{}, plan, lines, orToday(c.On))
		if err != nil {
			return ShipmentDTO{}, err
		}
		if err := s.shipments.Create(ctx, shp); err != nil {
			return ShipmentDTO{}, err
		}
		return s.detail(ctx, shp)
	})

	update := func(ctx context.Context, id domain.ShipmentID, fn func(context.Context, *domain.Shipment) error) (ShipmentDTO, error) {
		sc := scopeOf(ctx)
		shp, err := s.shipments.Update(ctx, id, func(ctx context.Context, shp *domain.Shipment) error {
			if err := sc.check(domain.ShipmentKind, shp.ID(), shp.State().Company, true); err != nil {
				return err
			}
			return fn(ctx, shp)
		})
		if err != nil {
			return ShipmentDTO{}, err
		}
		return s.detail(ctx, shp)
	}
	svc.Change = changing(d.UoW, PermShipmentUpdate, func(ctx context.Context, c ChangeShipment) (ShipmentDTO, error) {
		var v fw.Validation
		plan := c.plan(&v)
		if err := v.Err(); err != nil {
			return ShipmentDTO{}, err
		}
		return update(ctx, c.ID, func(ctx context.Context, shp *domain.Shipment) error {
			// A blocked carrier stays on the shipments that already had it.
			if _, err := s.carrierOf(ctx, shp.State().Company, plan.Carrier, plan.Carrier != shp.State().Carrier); err != nil {
				return err
			}
			return shp.Change(plan)
		})
	})
	svc.Move = changing(d.UoW, PermShipmentDispatch, func(ctx context.Context, c MoveShipment) (ShipmentDTO, error) {
		to := domain.Status(strings.TrimSpace(c.To))
		var v fw.Validation
		v.Require(slices.Contains(domain.Statuses, to) && to != domain.Scheduled, "to", "enum", "in-transit, delivered, returned or cancelled")
		if err := v.Err(); err != nil {
			return ShipmentDTO{}, err
		}
		on := orToday(c.On)
		return update(ctx, c.ID, func(ctx context.Context, shp *domain.Shipment) error {
			switch to {
			case domain.Delivered:
				return shp.Deliver(on, c.ReceivedBy)
			case domain.Returned:
				return shp.Return(on, c.Reason)
			case domain.Cancelled:
				return shp.Cancel(on, c.Reason)
			}
			carrier, err := s.carrierOf(ctx, shp.State().Company, shp.State().Carrier, true)
			if err != nil {
				return err
			}
			name := ""
			if carrier != nil {
				name = carrier.State().Name
			}
			return shp.Dispatch(on, name)
		})
	})

	svc.GetShipment = guard(PermShipmentRead, func(ctx context.Context, q GetShipment) (ShipmentDTO, error) {
		shp, err := d.Shipments.Get(ctx, q.ID)
		if err != nil {
			return ShipmentDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.ShipmentKind, shp.ID(), shp.State().Company, false); err != nil {
			return ShipmentDTO{}, err
		}
		return s.detail(ctx, shp)
	})
	svc.SearchShipments = guard(PermShipmentRead, func(ctx context.Context, q SearchShipments) (fw.Page[ShipmentDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Shipment]{within(scopeOf(ctx), domain.ShpFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.ShpFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Customer != "" {
			parts = append(parts, domain.ShpFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", q.Customer)}))
		}
		if q.Status != "" {
			v.Require(slices.Contains(domain.Statuses, domain.Status(q.Status)), "status", "enum", "a status")
			parts = append(parts, domain.ShpFieldStatus.Eq(q.Status))
		}
		if q.Carrier != "" {
			parts = append(parts, domain.ShpFieldCarrier.Eq(domain.CarrierID{UUID: parseID(&v, "carrier", q.Carrier)}))
		}
		if q.Tracking != "" {
			parts = append(parts, domain.ShpFieldTracking.ContainsFold(q.Tracking))
		}
		if q.Delivery != "" {
			parts = append(parts, domain.ShpFieldSrcType.Eq(DeliverySource), domain.ShpFieldSrcID.Eq(q.Delivery))
		}
		if err := v.Err(); err != nil {
			return fw.Page[ShipmentDTO]{}, err
		}
		page, err := d.Shipments.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.ShpFieldPlanned.Asc()))
		if err != nil {
			return fw.Page[ShipmentDTO]{}, err
		}
		return fw.MapPage(page, func(shp *domain.Shipment) ShipmentDTO { return shipmentDTO(shp, false) }), nil
	})
}

// RegisterCarrier adds a carrier to a company.
type RegisterCarrier struct {
	Company     string `json:"company"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Party       string `json:"party,omitempty"`
	TrackingURL string `json:"trackingUrl,omitempty"`
}

// ChangeCarrier replaces the data of a carrier.
type ChangeCarrier struct {
	ID          domain.CarrierID `json:"-"`
	Name        string           `json:"name"`
	Party       string           `json:"party,omitempty"`
	TrackingURL string           `json:"trackingUrl,omitempty"`
	Blocked     bool             `json:"blocked,omitempty"`
}

// SearchCarriers lists the carriers of a company.
type SearchCarriers struct{ Company string }

// CarrierDTO is the transport form of a carrier.
type CarrierDTO struct {
	ID          string `json:"id"`
	Company     string `json:"company"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Party       string `json:"party,omitempty"`
	TrackingURL string `json:"trackingUrl,omitempty"`
	Blocked     bool   `json:"blocked"`
	Version     int64  `json:"version"`
}

func carrierDTO(c *domain.Carrier) CarrierDTO {
	s := c.State()
	return CarrierDTO{ID: c.ID().String(), Company: s.Company.String(), Code: s.Code, Name: s.Name, Party: optID(s.Party.UUID),
		TrackingURL: s.TrackingURL, Blocked: s.Blocked, Version: c.Version()}
}

func (s service) wireCarriers(svc *Service) {
	d := s.Deps
	svc.RegisterCarrier = changing(d.UoW, PermCarrierUpdate, func(ctx context.Context, c RegisterCarrier) (CarrierDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		party := domain.PartyID{UUID: optionalID(&v, "party", c.Party)}
		if err := v.Err(); err != nil {
			return CarrierDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return CarrierDTO{}, err
		}
		car, err := domain.ReconstituteCarrier(domain.NewCarrierID(), domain.CarrierState{Company: company, Code: c.Code, Name: c.Name, Party: party,
			TrackingURL: c.TrackingURL})
		if err != nil {
			return CarrierDTO{}, err
		}
		dup, err := d.Carriers.Exists(ctx, spec.And(domain.CarFieldCompany.Eq(company), domain.CarFieldCode.Eq(car.State().Code)))
		if err != nil {
			return CarrierDTO{}, err
		}
		if dup {
			return CarrierDTO{}, fw.Violation("shipments.duplicate_carrier", "the company already has a carrier with that code")
		}
		if err := s.carriers.Create(ctx, car); err != nil {
			return CarrierDTO{}, err
		}
		return carrierDTO(car), nil
	})
	svc.ChangeCarrier = changing(d.UoW, PermCarrierUpdate, func(ctx context.Context, c ChangeCarrier) (CarrierDTO, error) {
		var v fw.Validation
		party := domain.PartyID{UUID: optionalID(&v, "party", c.Party)}
		if err := v.Err(); err != nil {
			return CarrierDTO{}, err
		}
		sc := scopeOf(ctx)
		car, err := s.carriers.Update(ctx, c.ID, func(_ context.Context, car *domain.Carrier) error {
			if err := sc.check(domain.CarrierKind, car.ID(), car.State().Company, true); err != nil {
				return err
			}
			return car.Change(c.Name, party, c.TrackingURL, c.Blocked)
		})
		if err != nil {
			return CarrierDTO{}, err
		}
		return carrierDTO(car), nil
	})
	svc.SearchCarriers = guard(PermCarrierRead, func(ctx context.Context, q SearchCarriers) ([]CarrierDTO, error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Carrier]{within(scopeOf(ctx), domain.CarFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.CarFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if err := v.Err(); err != nil {
			return nil, err
		}
		cs, err := d.Carriers.Find(ctx, spec.And(parts...), domain.CarFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []CarrierDTO{}
		for _, c := range cs {
			out = append(out, carrierDTO(c))
		}
		return out, nil
	})
}

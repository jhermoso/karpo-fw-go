// Package domain is the Shipments model: the physical dispatch of what a delivery note says was
// delivered (who carries it, how, where to, in how many packages, under which tracking number,
// when it left and when it arrived) and the carriers of a company. In C# a shipment had no
// direction, no carrier and no tracking number, its status was a free field disconnected from its
// history, and packages and route segments were classes without a table.
package domain

import (
	"slices"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Aggregate type names.
const (
	ShipmentKind = "shipments.shipment"
	CarrierKind  = "shipments.carrier"
)

// Identities of the context.
type (
	// ShipmentID identifies a shipment.
	ShipmentID struct{ fw.UUID }
	// CarrierID identifies a carrier of a company.
	CarrierID struct{ fw.UUID }
	// OrganizationID is the company that ships, an internal organization of Parties.
	OrganizationID struct{ fw.UUID }
	// PartyID is the recipient, or the party a carrier is.
	PartyID struct{ fw.UUID }
)

// Method is how a shipment travels (the seeded C# method types, plus the customer collecting it).
type Method string

// Methods.
const (
	Truck   Method = "truck"
	Courier Method = "courier"
	Rail    Method = "rail"
	Air     Method = "air"
	Ocean   Method = "ocean"
	Pickup  Method = "pickup" // the customer collects it: no carrier
)

// Methods lists the valid methods.
var Methods = []Method{Truck, Courier, Rail, Air, Ocean, Pickup}

// Status is where a shipment is.
type Status string

// Statuses (the seeded C# status types; "picked" is left to the warehouse).
const (
	Scheduled Status = "scheduled"
	InTransit Status = "in-transit"
	Delivered Status = "delivered"
	Returned  Status = "returned" // it could not be delivered and came back
	Cancelled Status = "cancelled"
)

// Statuses lists the valid statuses.
var Statuses = []Status{Scheduled, InTransit, Delivered, Returned, Cancelled}

// Step is a change of status.
type Step struct {
	Status Status
	On     vocab.Date
}

// Line is something that travels in the shipment.
type Line struct {
	No          int
	SKU         string
	Description string
	Quantity    vocab.Decimal
	UoM         string
}

// Source is the document a shipment dispatches (a delivery note of Orders).
type Source struct {
	Type string
	ID   string
	Ref  string // its number
}

// Plan is what may be edited while the shipment has not left.
type Plan struct {
	Method           Method
	Carrier          CarrierID
	Tracking         string
	Recipient        string // who receives it
	Destination      string // where: the address as written on the label
	Packages         int
	WeightKg         vocab.Decimal
	Cost             vocab.Decimal // what the carrier charges
	Instructions     string
	EstimatedShip    vocab.Date
	EstimatedArrival vocab.Date
}

// ShipmentState is the persisted state of a shipment.
type ShipmentState struct {
	Company  OrganizationID
	Customer PartyID
	Source   Source
	Plan
	Lines      []Line
	Status     Status
	Dispatched vocab.Date
	Closed     vocab.Date // delivered, returned or cancelled
	ReceivedBy string
	Reason     string // of the return or the cancellation
	History    []Step
	Audit      traits.AuditStamp
}

// Shipment is the dispatch of goods to a customer.
type Shipment struct {
	fw.BaseAggregateRoot[ShipmentID]
	traits.Audited
	s ShipmentState
}

func cents(d vocab.Decimal) bool { return d.Equal(d.Round(2)) }

func checkPlan(v *fw.Validation, p *Plan) {
	p.Tracking, p.Recipient, p.Destination, p.Instructions = strings.TrimSpace(p.Tracking), strings.TrimSpace(p.Recipient),
		strings.TrimSpace(p.Destination), strings.TrimSpace(p.Instructions)
	v.Require(p.Method == "" || slices.Contains(Methods, p.Method), "method", "enum", "truck, courier, rail, air, ocean or pickup")
	v.Require(p.Method != Pickup || p.Carrier.IsZero(), "carrier", "pickup", "a collection by the customer has no carrier")
	v.Require(utf8.RuneCountInString(p.Tracking) <= 60, "tracking", "length", "at most 60 characters")
	v.Require(utf8.RuneCountInString(p.Recipient) <= 200, "recipient", "length", "at most 200 characters")
	v.Require(utf8.RuneCountInString(p.Destination) <= 500, "destination", "length", "at most 500 characters")
	v.Require(utf8.RuneCountInString(p.Instructions) <= 500, "instructions", "length", "at most 500 characters")
	v.Require(p.Packages >= 0 && p.Packages <= 9999, "packages", "range", "from 0 to 9999 packages")
	v.Require(!p.WeightKg.IsNegative() && p.WeightKg.Equal(p.WeightKg.Round(3)), "weightKg", "range", "a weight in kilograms with three decimals")
	v.Require(!p.Cost.IsNegative() && cents(p.Cost), "cost", "range", "a cost in cents")
	v.Require(p.EstimatedArrival.IsZero() || p.EstimatedShip.IsZero() || !p.EstimatedArrival.Before(p.EstimatedShip), "estimatedArrival", "range",
		"an arrival not before the departure")
}

// ReconstituteShipment rebuilds a shipment.
func ReconstituteShipment(id ShipmentID, s ShipmentState) (*Shipment, error) {
	base, err := fw.NewBaseAggregateRoot(ShipmentKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	v.Require(!s.Customer.IsZero(), "customer", "required", "the customer is required")
	v.Require((s.Source.Type == "") == (s.Source.ID == "") && len(s.Source.Type) <= 40 && len(s.Source.ID) <= 64 && utf8.RuneCountInString(s.Source.Ref) <= 60,
		"source", "format", "a source has a type and an identity")
	v.Require(slices.Contains(Statuses, s.Status), "status", "enum", "a status")
	v.Require(len(s.History) >= 1, "history", "required", "a shipment has the day it was planned")
	checkPlan(&v, &s.Plan)
	v.Require(len(s.Lines) >= 1 && len(s.Lines) <= 500, "lines", "range", "from 1 to 500 lines")
	for i := range s.Lines {
		l := &s.Lines[i]
		l.SKU, l.Description, l.UoM = strings.TrimSpace(l.SKU), strings.TrimSpace(l.Description), strings.TrimSpace(l.UoM)
		v.Require(l.No == i+1, "lines", "order", "lines are numbered from 1")
		v.Require(l.Description != "" && utf8.RuneCountInString(l.Description) <= 200 && len(l.SKU) <= 40 && len(l.UoM) <= 10, "lines", "format",
			"a description of 1 to 200 characters")
		v.Require(l.Quantity.IsPositive(), "lines", "quantity", "a positive quantity")
	}
	if err := v.Err(); err != nil {
		return nil, err
	}
	s.Lines, s.History = slices.Clone(s.Lines), slices.Clone(s.History)
	return &Shipment{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// ScheduleShipment plans the dispatch of some goods to a customer on a day.
func ScheduleShipment(id ShipmentID, company OrganizationID, customer PartyID, src Source, plan Plan, lines []Line, on vocab.Date) (*Shipment, error) {
	if on.IsZero() {
		return nil, fw.Violation("shipments.date", "the day is required")
	}
	lines = slices.Clone(lines)
	for i := range lines {
		lines[i].No = i + 1
	}
	s, err := ReconstituteShipment(id, ShipmentState{Company: company, Customer: customer, Source: src, Plan: plan, Lines: lines, Status: Scheduled,
		History: []Step{{Status: Scheduled, On: on}}})
	if err != nil {
		return nil, err
	}
	s.Raise(ShipmentScheduled{EventMeta: s.NewEventMeta(), Company: company.String(), Customer: customer.String(), SourceType: src.Type, SourceID: src.ID})
	return s, nil
}

// State returns the state (slices are copies).
func (s *Shipment) State() ShipmentState {
	st := s.s
	st.Lines, st.History = slices.Clone(st.Lines), slices.Clone(st.History)
	return st
}

// Change replaces the plan of a shipment that has not left. Once in transit only the tracking
// number, the estimated arrival and the cost may still change.
func (s *Shipment) Change(p Plan) error {
	var v fw.Validation
	checkPlan(&v, &p)
	if err := v.Err(); err != nil {
		return err
	}
	switch s.s.Status {
	case Scheduled:
		s.s.Plan = p
	case InTransit:
		kept := s.s.Plan
		kept.Tracking, kept.EstimatedArrival, kept.Cost = p.Tracking, p.EstimatedArrival, p.Cost
		if p.Method != kept.Method || p.Carrier != kept.Carrier || p.Recipient != kept.Recipient || p.Destination != kept.Destination ||
			p.Packages != kept.Packages || !p.WeightKg.Equal(kept.WeightKg) {
			return fw.Violation("shipments.in_transit", "a shipment in transit keeps its carrier, destination and packages")
		}
		s.s.Plan = kept
	default:
		return fw.Violation("shipments.closed", "the shipment is "+string(s.s.Status))
	}
	return nil
}

func (s *Shipment) move(from, to Status, on vocab.Date) error {
	if s.s.Status != from {
		return fw.Violation("shipments.transition", "a shipment "+string(s.s.Status)+" cannot become "+string(to))
	}
	if on.IsZero() {
		return fw.Violation("shipments.date", "the day is required")
	}
	if n := len(s.s.History); n > 0 && on.Before(s.s.History[n-1].On) {
		return fw.Violation("shipments.date", "a change of status is not before the previous one")
	}
	s.s.Status = to
	s.s.History = append(slices.Clone(s.s.History), Step{Status: to, On: on})
	return nil
}

func reason(r string) (string, error) {
	r = strings.TrimSpace(r)
	if r == "" || utf8.RuneCountInString(r) > 200 {
		return "", fw.Violation("shipments.reason", "a reason of 1 to 200 characters")
	}
	return r, nil
}

// Dispatch hands the shipment over: it needs to know how it travels, where to, in how many
// packages and, unless the customer collects it, who carries it. carrier is the carrier's name,
// for the event.
func (s *Shipment) Dispatch(on vocab.Date, carrier string) error {
	p := s.s.Plan
	if s.s.Status == Scheduled && (p.Method == "" || p.Packages < 1 || (p.Method != Pickup && (p.Carrier.IsZero() || p.Destination == ""))) {
		return fw.Violation("shipments.incomplete", "dispatching needs the method, the packages and, unless collected, the carrier and the destination")
	}
	if err := s.move(Scheduled, InTransit, on); err != nil {
		return err
	}
	s.s.Dispatched = on
	s.Raise(ShipmentDispatched{EventMeta: s.NewEventMeta(), Company: s.s.Company.String(), Customer: s.s.Customer.String(), SourceType: s.s.Source.Type,
		SourceID: s.s.Source.ID, SourceRef: s.s.Source.Ref, Method: string(p.Method), Carrier: carrier, Tracking: p.Tracking, Packages: p.Packages,
		Date: on.String()})
	return nil
}

// Deliver confirms the shipment reached its recipient.
func (s *Shipment) Deliver(on vocab.Date, receivedBy string) error {
	receivedBy = strings.TrimSpace(receivedBy)
	if utf8.RuneCountInString(receivedBy) > 200 {
		return fw.Violation("shipments.received_by", "at most 200 characters")
	}
	if err := s.move(InTransit, Delivered, on); err != nil {
		return err
	}
	s.s.Closed, s.s.ReceivedBy = on, receivedBy
	s.Raise(ShipmentDelivered{EventMeta: s.NewEventMeta(), Company: s.s.Company.String(), Customer: s.s.Customer.String(), SourceType: s.s.Source.Type,
		SourceID: s.s.Source.ID, SourceRef: s.s.Source.Ref, Date: on.String(), ReceivedBy: receivedBy})
	return nil
}

// Return records that the shipment could not be delivered and came back.
func (s *Shipment) Return(on vocab.Date, why string) error {
	why, err := reason(why)
	if err != nil {
		return err
	}
	if err := s.move(InTransit, Returned, on); err != nil {
		return err
	}
	s.s.Closed, s.s.Reason = on, why
	s.Raise(ShipmentReturned{EventMeta: s.NewEventMeta(), Company: s.s.Company.String(), Customer: s.s.Customer.String(), SourceType: s.s.Source.Type,
		SourceID: s.s.Source.ID, SourceRef: s.s.Source.Ref, Date: on.String(), Reason: why})
	return nil
}

// Cancel abandons a shipment that has not left.
func (s *Shipment) Cancel(on vocab.Date, why string) error {
	why, err := reason(why)
	if err != nil {
		return err
	}
	if err := s.move(Scheduled, Cancelled, on); err != nil {
		return err
	}
	s.s.Closed, s.s.Reason = on, why
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (s *Shipment) AuditSnapshot() map[string]any {
	return map[string]any{"source": s.s.Source.Ref, "status": string(s.s.Status), "tracking": s.s.Tracking, "packages": s.s.Packages}
}

// Shipment fields.
var (
	ShpFieldCompany  = spec.Comparable("company", func(s *Shipment) OrganizationID { return s.s.Company })
	ShpFieldCustomer = spec.Comparable("customer", func(s *Shipment) PartyID { return s.s.Customer })
	ShpFieldStatus   = spec.Comparable("status", func(s *Shipment) string { return string(s.s.Status) })
	ShpFieldCarrier  = spec.Comparable("carrier", func(s *Shipment) CarrierID { return s.s.Carrier })
	ShpFieldSrcType  = spec.Comparable("source_type", func(s *Shipment) string { return s.s.Source.Type })
	ShpFieldSrcID    = spec.Comparable("source_id", func(s *Shipment) string { return s.s.Source.ID })
	ShpFieldTracking = spec.Text("tracking", func(s *Shipment) string { return s.s.Tracking })
	ShpFieldPlanned  = spec.OrderedBy("planned_on", func(s *Shipment) vocab.Date { return s.s.History[0].On }, vocab.CompareDates)
)

// Events of a shipment.
type (
	// ShipmentScheduled is raised when a shipment is planned.
	ShipmentScheduled struct {
		fw.EventMeta
		Company    string `json:"company"`
		Customer   string `json:"customer"`
		SourceType string `json:"sourceType,omitempty"`
		SourceID   string `json:"sourceId,omitempty"`
	}
	// ShipmentDispatched is raised when a shipment leaves.
	ShipmentDispatched struct {
		fw.EventMeta
		Company    string `json:"company"`
		Customer   string `json:"customer"`
		SourceType string `json:"sourceType,omitempty"`
		SourceID   string `json:"sourceId,omitempty"`
		SourceRef  string `json:"sourceRef,omitempty"`
		Method     string `json:"method"`
		Carrier    string `json:"carrier,omitempty"`
		Tracking   string `json:"tracking,omitempty"`
		Packages   int    `json:"packages"`
		Date       string `json:"date"`
	}
	// ShipmentDelivered is raised when a shipment reaches its recipient.
	ShipmentDelivered struct {
		fw.EventMeta
		Company    string `json:"company"`
		Customer   string `json:"customer"`
		SourceType string `json:"sourceType,omitempty"`
		SourceID   string `json:"sourceId,omitempty"`
		SourceRef  string `json:"sourceRef,omitempty"`
		Date       string `json:"date"`
		ReceivedBy string `json:"receivedBy,omitempty"`
	}
	// ShipmentReturned is raised when a shipment comes back undelivered.
	ShipmentReturned struct {
		fw.EventMeta
		Company    string `json:"company"`
		Customer   string `json:"customer"`
		SourceType string `json:"sourceType,omitempty"`
		SourceID   string `json:"sourceId,omitempty"`
		SourceRef  string `json:"sourceRef,omitempty"`
		Date       string `json:"date"`
		Reason     string `json:"reason"`
	}
)

// EventType implementations.
func (ShipmentScheduled) EventType() string  { return "shipments.shipment_scheduled" }
func (ShipmentDispatched) EventType() string { return "shipments.shipment_dispatched" }
func (ShipmentDelivered) EventType() string  { return "shipments.shipment_delivered" }
func (ShipmentReturned) EventType() string   { return "shipments.shipment_returned" }

// NewShipmentID returns a new identity.
func NewShipmentID() ShipmentID { return ShipmentID{fw.NewUUID()} }

// ParseShipmentID parses a textual identity.
func ParseShipmentID(s string) (ShipmentID, error) {
	u, err := fw.ParseUUID(s)
	return ShipmentID{u}, err
}

// ShipmentRepository stores shipments.
type ShipmentRepository = fw.Repository[ShipmentID, *Shipment]

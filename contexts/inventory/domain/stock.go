// Package domain is the Inventory model: the warehouses of a company, the stock level of each
// product in each warehouse (on hand, reserved, weighted average cost), the ledger of movements
// that explains it and the reservations that hold part of it. The C# kept one mutable row per
// product and facility with no ledger and no valuation: quantities were clamped at zero instead of
// refused, adjustments left no trace, reservations were never released and neither issuances nor
// receipts touched the stock.
package domain

import (
	"context"
	"strings"
	"unicode/utf8"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/traits"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Aggregate type names.
const (
	WarehouseKind   = "inventory.warehouse"
	LevelKind       = "inventory.stock_level"
	MovementKind    = "inventory.movement"
	ReservationKind = "inventory.reservation"
)

// Identities of the context.
type (
	// WarehouseID identifies a warehouse.
	WarehouseID struct{ fw.UUID }
	// LevelID identifies a stock level.
	LevelID struct{ fw.UUID }
	// MovementID identifies a movement.
	MovementID struct{ fw.UUID }
	// ReservationID identifies a reservation.
	ReservationID struct{ fw.UUID }
	// OrganizationID is the company, an internal organization of the Parties context.
	OrganizationID struct{ fw.UUID }
	// ProductID is a product of the Products context.
	ProductID struct{ fw.UUID }
	// FacilityID is a facility of the Facilities context.
	FacilityID struct{ fw.UUID }
)

func zero() vocab.Decimal { return vocab.DecimalFromInt(0) }

// quantity reports whether q is a positive quantity of up to 4 decimals.
func quantity(q vocab.Decimal) bool { return q.IsPositive() && q.Equal(q.Round(4)) }

// WarehouseState is the persisted state of a warehouse.
type WarehouseState struct {
	Company  OrganizationID
	Code     string
	Name     string
	Facility FacilityID // where it is, when the Facilities context knows it
	Active   bool
	Audit    traits.AuditStamp
}

// Warehouse is a place where a company keeps stock (in the C# a warehouse was just a Facility
// referenced by a bare id).
type Warehouse struct {
	fw.BaseAggregateRoot[WarehouseID]
	traits.Audited
	s WarehouseState
}

// ReconstituteWarehouse rebuilds a warehouse.
func ReconstituteWarehouse(id WarehouseID, s WarehouseState) (*Warehouse, error) {
	base, err := fw.NewBaseAggregateRoot(WarehouseKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	s.Code, s.Name = strings.ToUpper(strings.TrimSpace(s.Code)), strings.TrimSpace(s.Name)
	v.Require(!s.Company.IsZero(), "company", "required", "the company is required")
	v.Require(s.Code != "" && len(s.Code) <= 10 && !strings.ContainsAny(s.Code, " \t"), "code", "format", "a code of 1 to 10 characters without spaces")
	v.Require(s.Name != "" && utf8.RuneCountInString(s.Name) <= 100, "name", "length", "a name of 1 to 100 characters")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Warehouse{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (w *Warehouse) State() WarehouseState { return w.s }

// Close retires the warehouse: it takes no more movements (the caller checks it is empty).
func (w *Warehouse) Close() error {
	if !w.s.Active {
		return fw.Violation("inventory.warehouse_closed", "the warehouse is already closed")
	}
	w.s.Active = false
	return nil
}

// AuditSnapshot implements traits.Snapshotter.
func (w *Warehouse) AuditSnapshot() map[string]any {
	return map[string]any{"code": w.s.Code, "active": w.s.Active}
}

// Warehouse fields.
var (
	WhFieldCompany = spec.Comparable("company", func(w *Warehouse) OrganizationID { return w.s.Company })
	WhFieldCode    = spec.Ordered("code", func(w *Warehouse) string { return w.s.Code })
)

// LevelState is the persisted state of a stock level.
type LevelState struct {
	Company      OrganizationID
	Warehouse    WarehouseID
	Product      ProductID
	OnHand       vocab.Decimal
	Reserved     vocab.Decimal
	AverageCost  vocab.Decimal // weighted average unit cost, 4 decimals
	ReorderPoint vocab.Decimal
	Moves        int64 // movements posted: the sequence of the ledger of this level
}

// Level is the stock of a product in a warehouse. Its quantities change only through its methods,
// each of which is explained by a movement of the ledger.
type Level struct {
	fw.BaseAggregateRoot[LevelID]
	s LevelState
}

// ReconstituteLevel rebuilds a stock level.
func ReconstituteLevel(id LevelID, s LevelState) (*Level, error) {
	base, err := fw.NewBaseAggregateRoot(LevelKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Warehouse.IsZero() && !s.Product.IsZero(), "product", "required", "company, warehouse and product are required")
	v.Require(!s.OnHand.IsNegative() && !s.Reserved.IsNegative() && !s.Reserved.GreaterThan(s.OnHand), "onHand", "range",
		"non-negative stock, reserved within what is on hand")
	v.Require(!s.AverageCost.IsNegative() && !s.ReorderPoint.IsNegative() && s.Moves >= 0, "averageCost", "range", "non-negative cost and reorder point")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Level{BaseAggregateRoot: base, s: s}, nil
}

// OpenLevel creates the empty level of a product in a warehouse.
func OpenLevel(id LevelID, company OrganizationID, warehouse WarehouseID, product ProductID) (*Level, error) {
	return ReconstituteLevel(id, LevelState{Company: company, Warehouse: warehouse, Product: product, OnHand: zero(), Reserved: zero(), AverageCost: zero(),
		ReorderPoint: zero()})
}

// State returns the state.
func (l *Level) State() LevelState { return l.s }

// Available returns what can still be promised: on hand minus reserved.
func (l *Level) Available() vocab.Decimal { return l.s.OnHand.Sub(l.s.Reserved) }

// Value returns the stock at its average cost, in cents.
func (l *Level) Value() vocab.Decimal { return l.s.OnHand.Mul(l.s.AverageCost).Round(2) }

// BelowReorder reports whether the available stock is at or under the reorder point.
func (l *Level) BelowReorder() bool {
	return l.s.ReorderPoint.IsPositive() && !l.Available().GreaterThan(l.s.ReorderPoint)
}

func (l *Level) next() int64 { l.s.Moves++; return l.s.Moves }

// Receive adds stock at a unit cost and recomputes the weighted average cost. It returns the
// sequence of the movement.
func (l *Level) Receive(q, unitCost vocab.Decimal) (int64, error) {
	if !quantity(q) || unitCost.IsNegative() || !unitCost.Equal(unitCost.Round(4)) {
		return 0, fw.Violation("inventory.quantity", "a positive quantity and a non-negative cost, of up to 4 decimals")
	}
	total := l.s.OnHand.Add(q)
	l.s.AverageCost = l.s.OnHand.Mul(l.s.AverageCost).Add(q.Mul(unitCost)).Div(total).Round(4)
	l.s.OnHand = total
	return l.next(), nil
}

// Issue takes stock out at the average cost, which it returns with the sequence. Stock never goes
// negative, and what is reserved for others is not taken (the C# clamped both at zero).
func (l *Level) Issue(q vocab.Decimal) (vocab.Decimal, int64, error) {
	if !quantity(q) {
		return vocab.Decimal{}, 0, fw.Violation("inventory.quantity", "a positive quantity of up to 4 decimals")
	}
	if q.GreaterThan(l.Available()) {
		return vocab.Decimal{}, 0, fw.Violation("inventory.insufficient_stock", "only "+l.Available().String()+" available")
	}
	l.s.OnHand = l.s.OnHand.Sub(q)
	return l.s.AverageCost, l.next(), nil
}

// IssueReserved takes out stock that was reserved: it leaves both the reserve and the stock.
func (l *Level) IssueReserved(q vocab.Decimal) (vocab.Decimal, int64, error) {
	if !quantity(q) || q.GreaterThan(l.s.Reserved) {
		return vocab.Decimal{}, 0, fw.Violation("inventory.reservation_exceeded", "more than what is reserved")
	}
	l.s.Reserved, l.s.OnHand = l.s.Reserved.Sub(q), l.s.OnHand.Sub(q)
	return l.s.AverageCost, l.next(), nil
}

// Reserve holds available stock.
func (l *Level) Reserve(q vocab.Decimal) error {
	if !quantity(q) {
		return fw.Violation("inventory.quantity", "a positive quantity of up to 4 decimals")
	}
	if q.GreaterThan(l.Available()) {
		return fw.Violation("inventory.insufficient_stock", "only "+l.Available().String()+" available")
	}
	l.s.Reserved = l.s.Reserved.Add(q)
	return nil
}

// Release frees reserved stock.
func (l *Level) Release(q vocab.Decimal) error {
	if !quantity(q) || q.GreaterThan(l.s.Reserved) {
		return fw.Violation("inventory.reservation_exceeded", "more than what is reserved")
	}
	l.s.Reserved = l.s.Reserved.Sub(q)
	return nil
}

// CountTo sets the stock to what was counted and returns the difference and the sequence (zero
// difference: no movement). A count cannot go under what is reserved. A surplus enters at the
// average cost, so the count does not change it.
func (l *Level) CountTo(counted vocab.Decimal) (vocab.Decimal, int64, error) {
	if counted.IsNegative() || !counted.Equal(counted.Round(4)) {
		return vocab.Decimal{}, 0, fw.Violation("inventory.quantity", "a non-negative count of up to 4 decimals")
	}
	if counted.LessThan(l.s.Reserved) {
		return vocab.Decimal{}, 0, fw.Violation("inventory.count_under_reserved", l.s.Reserved.String()+" are reserved: release them first")
	}
	diff := counted.Sub(l.s.OnHand)
	if diff.IsZero() {
		return diff, 0, nil
	}
	l.s.OnHand = counted
	return diff, l.next(), nil
}

// SetReorderPoint sets the available quantity under which the product is to be reordered.
func (l *Level) SetReorderPoint(p vocab.Decimal) error {
	if p.IsNegative() || !p.Equal(p.Round(4)) {
		return fw.Violation("inventory.quantity", "a non-negative reorder point of up to 4 decimals")
	}
	l.s.ReorderPoint = p
	return nil
}

// Level fields.
var (
	LvlFieldCompany   = spec.Comparable("company", func(l *Level) OrganizationID { return l.s.Company })
	LvlFieldWarehouse = spec.Comparable("warehouse_id", func(l *Level) WarehouseID { return l.s.Warehouse })
	LvlFieldProduct   = spec.Comparable("product", func(l *Level) ProductID { return l.s.Product })
	LvlFieldEmpty     = spec.Comparable("is_empty", func(l *Level) bool { return l.s.OnHand.IsZero() })
)

// MoveKind is the kind of a movement.
type MoveKind int

// Movement kinds.
const (
	Receipt MoveKind = iota + 1
	Issue
	Adjustment
	TransferOut
	TransferIn
)

var moveKinds = map[MoveKind]string{Receipt: "receipt", Issue: "issue", Adjustment: "adjustment", TransferOut: "transfer-out", TransferIn: "transfer-in"}

// String returns the stable name.
func (k MoveKind) String() string { return moveKinds[k] }

// Source is the fact a movement comes from (a receipt of Purchases, a delivery of Orders…).
type Source struct {
	Type string
	ID   string
}

// MovementState is the persisted state of a movement.
type MovementState struct {
	Company   OrganizationID
	Warehouse WarehouseID
	Product   ProductID
	Seq       int64 // order within its stock level
	Kind      MoveKind
	Date      vocab.Date
	Quantity  vocab.Decimal // signed: positive in, negative out
	UnitCost  vocab.Decimal
	Value     vocab.Decimal // quantity × unit cost, in cents, signed
	Balance   vocab.Decimal // on hand after the movement
	Lot       string        // lot or serial of a tracked product
	Note      string
	Source    Source
	Audit     traits.AuditStamp
}

// Movement is a line of the stock ledger. It never changes: a mistake is corrected with another
// movement.
type Movement struct {
	fw.BaseAggregateRoot[MovementID]
	traits.Audited
	s MovementState
}

// ReconstituteMovement rebuilds a movement.
func ReconstituteMovement(id MovementID, s MovementState) (*Movement, error) {
	base, err := fw.NewBaseAggregateRoot(MovementKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Warehouse.IsZero() && !s.Product.IsZero(), "product", "required", "company, warehouse and product are required")
	_, ok := moveKinds[s.Kind]
	v.Require(ok, "kind", "enum", "unknown kind")
	v.Require(!s.Date.IsZero() && s.Seq > 0, "date", "required", "the date and the sequence are required")
	v.Require(!s.Quantity.IsZero(), "quantity", "range", "a movement moves something")
	s.Lot, s.Note = strings.TrimSpace(s.Lot), strings.TrimSpace(s.Note)
	v.Require(len(s.Lot) <= 40 && utf8.RuneCountInString(s.Note) <= 200, "note", "length", "a lot of up to 40 and a note of up to 200 characters")
	v.Require(len(s.Source.Type) <= 80 && len(s.Source.ID) <= 80, "source", "length", "a source within limits")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Movement{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// Post records a movement of a level (after the level changed) and raises it.
func Post(id MovementID, l *Level, seq int64, kind MoveKind, date vocab.Date, q, unitCost vocab.Decimal, lot, note string, src Source) (*Movement, error) {
	ls := l.State()
	m, err := ReconstituteMovement(id, MovementState{Company: ls.Company, Warehouse: ls.Warehouse, Product: ls.Product, Seq: seq, Kind: kind, Date: date,
		Quantity: q, UnitCost: unitCost, Value: q.Mul(unitCost).Round(2), Balance: ls.OnHand, Lot: lot, Note: note, Source: src})
	if err != nil {
		return nil, err
	}
	m.Raise(StockMoved{EventMeta: m.NewEventMeta(), Snapshot: m.s})
	return m, nil
}

// State returns the state.
func (m *Movement) State() MovementState { return m.s }

// AuditSnapshot implements traits.Snapshotter.
func (m *Movement) AuditSnapshot() map[string]any {
	return map[string]any{"kind": m.s.Kind.String(), "quantity": m.s.Quantity.String(), "balance": m.s.Balance.String()}
}

// Movement fields.
var (
	MovFieldCompany   = spec.Comparable("company", func(m *Movement) OrganizationID { return m.s.Company })
	MovFieldWarehouse = spec.Comparable("warehouse_id", func(m *Movement) WarehouseID { return m.s.Warehouse })
	MovFieldProduct   = spec.Comparable("product", func(m *Movement) ProductID { return m.s.Product })
	MovFieldKind      = spec.Comparable("kind", func(m *Movement) int { return int(m.s.Kind) })
	MovFieldDate      = spec.OrderedBy("moved_on", func(m *Movement) vocab.Date { return m.s.Date }, vocab.CompareDates)
	MovFieldSeq       = spec.Ordered("seq", func(m *Movement) int64 { return m.s.Seq })
	MovFieldSrcType   = spec.Comparable("source_type", func(m *Movement) string { return m.s.Source.Type })
	MovFieldSrcID     = spec.Comparable("source_id", func(m *Movement) string { return m.s.Source.ID })
)

// ReservationState is the persisted state of a reservation.
type ReservationState struct {
	Company   OrganizationID
	Warehouse WarehouseID
	Product   ProductID
	Source    Source        // who holds the stock (an order line)
	Quantity  vocab.Decimal // reserved in total
	Open      vocab.Decimal // still held: neither issued nor released
	Audit     traits.AuditStamp
}

// Reservation holds stock of a product in a warehouse for a source until it is issued or
// released (in the C# nothing ever released or consumed a reservation).
type Reservation struct {
	fw.BaseAggregateRoot[ReservationID]
	traits.Audited
	s ReservationState
}

// ReconstituteReservation rebuilds a reservation.
func ReconstituteReservation(id ReservationID, s ReservationState) (*Reservation, error) {
	base, err := fw.NewBaseAggregateRoot(ReservationKind, id)
	if err != nil {
		return nil, err
	}
	var v fw.Validation
	v.Require(!s.Company.IsZero() && !s.Warehouse.IsZero() && !s.Product.IsZero(), "product", "required", "company, warehouse and product are required")
	v.Require(s.Source.Type != "" && s.Source.ID != "" && len(s.Source.Type) <= 80 && len(s.Source.ID) <= 80, "source", "required", "the source is required")
	v.Require(quantity(s.Quantity) && !s.Open.IsNegative() && !s.Open.GreaterThan(s.Quantity), "quantity", "range", "a positive quantity, open within it")
	if err := v.Err(); err != nil {
		return nil, err
	}
	return &Reservation{BaseAggregateRoot: base, Audited: traits.RestoredAudit(s.Audit), s: s}, nil
}

// State returns the state.
func (r *Reservation) State() ReservationState { return r.s }

// Hold creates the reservation of a source and raises it.
func Hold(id ReservationID, s ReservationState) (*Reservation, error) {
	s.Open = s.Quantity
	r, err := ReconstituteReservation(id, s)
	if err != nil {
		return nil, err
	}
	r.Raise(StockReserved{EventMeta: r.NewEventMeta(), Company: s.Company.String(), Warehouse: s.Warehouse.String(), Product: s.Product.String(),
		SourceType: s.Source.Type, SourceID: s.Source.ID, Added: s.Quantity.String(), Held: s.Quantity.String()})
	return r, nil
}

// Add holds more stock for the same source (a shortage covered later).
func (r *Reservation) Add(q vocab.Decimal) error {
	if !quantity(q) {
		return fw.Violation("inventory.quantity", "a positive quantity of up to 4 decimals")
	}
	r.s.Quantity, r.s.Open = r.s.Quantity.Add(q), r.s.Open.Add(q)
	r.Raise(StockReserved{EventMeta: r.NewEventMeta(), Company: r.s.Company.String(), Warehouse: r.s.Warehouse.String(), Product: r.s.Product.String(),
		SourceType: r.s.Source.Type, SourceID: r.s.Source.ID, Added: q.String(), Held: r.s.Open.String()})
	return nil
}

// Consume lowers what is held by an issued quantity.
func (r *Reservation) Consume(q vocab.Decimal) error {
	if !quantity(q) || q.GreaterThan(r.s.Open) {
		return fw.Violation("inventory.reservation_exceeded", "the reservation holds only "+r.s.Open.String())
	}
	r.s.Open = r.s.Open.Sub(q)
	return nil
}

// Release frees what is still held and returns it.
func (r *Reservation) Release() (vocab.Decimal, error) {
	if r.s.Open.IsZero() {
		return vocab.Decimal{}, fw.Violation("inventory.reservation_closed", "the reservation holds nothing")
	}
	q := r.s.Open
	r.s.Open = zero()
	return q, nil
}

// AuditSnapshot implements traits.Snapshotter.
func (r *Reservation) AuditSnapshot() map[string]any {
	return map[string]any{"quantity": r.s.Quantity.String(), "open": r.s.Open.String()}
}

// Reservation fields.
var (
	ResFieldCompany   = spec.Comparable("company", func(r *Reservation) OrganizationID { return r.s.Company })
	ResFieldWarehouse = spec.Comparable("warehouse_id", func(r *Reservation) WarehouseID { return r.s.Warehouse })
	ResFieldProduct   = spec.Comparable("product", func(r *Reservation) ProductID { return r.s.Product })
	ResFieldSrcType   = spec.Comparable("source_type", func(r *Reservation) string { return r.s.Source.Type })
	ResFieldSrcID     = spec.Comparable("source_id", func(r *Reservation) string { return r.s.Source.ID })
	ResFieldClosed    = spec.Comparable("closed", func(r *Reservation) bool { return r.s.Open.IsZero() })
)

// StockMoved is raised by each movement of the ledger.
type StockMoved struct {
	fw.EventMeta
	Snapshot MovementState `json:"snapshot"`
}

// EventType implements fw.Event.
func (StockMoved) EventType() string { return "inventory.stock_moved" }

// StockReserved is raised when stock is held for a source: Added now, Held in total.
type StockReserved struct {
	fw.EventMeta
	Company    string `json:"company"`
	Warehouse  string `json:"warehouse"`
	Product    string `json:"product"`
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
	Added      string `json:"added"`
	Held       string `json:"held"`
}

// EventType implements fw.Event.
func (StockReserved) EventType() string { return "inventory.stock_reserved" }

// New identities and parsing.
func NewWarehouseID() WarehouseID     { return WarehouseID{fw.NewUUID()} }
func NewLevelID() LevelID             { return LevelID{fw.NewUUID()} }
func NewMovementID() MovementID       { return MovementID{fw.NewUUID()} }
func NewReservationID() ReservationID { return ReservationID{fw.NewUUID()} }

// ParseWarehouseID parses a textual identity.
func ParseWarehouseID(s string) (WarehouseID, error) {
	u, err := fw.ParseUUID(s)
	return WarehouseID{u}, err
}

// ParseReservationID parses a textual identity.
func ParseReservationID(s string) (ReservationID, error) {
	u, err := fw.ParseUUID(s)
	return ReservationID{u}, err
}

// Repositories of the context.
type (
	WarehouseRepository   = fw.Repository[WarehouseID, *Warehouse]
	LevelRepository       = fw.Repository[LevelID, *Level]
	MovementRepository    = fw.Repository[MovementID, *Movement]
	ReservationRepository = fw.Repository[ReservationID, *Reservation]
)

// Item is what Inventory needs to know of a product.
type Item struct {
	Company      OrganizationID
	SKU          string
	Name         string
	Stocked      bool
	Tracked      bool // by lot or serial: its movements carry one
	Discontinued vocab.Date
}

// Catalog resolves products (a port Inventory owns over the Products Catalog contract).
type Catalog interface {
	Items(ctx context.Context, products []ProductID) (map[ProductID]Item, error)
}

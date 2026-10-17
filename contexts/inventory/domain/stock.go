// Package domain es el modelo de Inventario: los almacenes de una empresa, el nivel de existencias
// de cada producto en cada almacén (disponibles en mano, reservadas, coste medio ponderado), el
// libro de movimientos que lo explica y las reservas que retienen parte de él. El C# tenía una
// fila mutable por producto e instalación, sin libro y sin valoración: las cantidades se recortaban
// a cero en lugar de rechazarse, los ajustes no dejaban rastro, las reservas nunca se liberaban y
// ni las salidas ni las entradas tocaban las existencias.
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

// Nombres de tipo de agregado.
const (
	WarehouseKind   = "inventory.warehouse"
	LevelKind       = "inventory.stock_level"
	MovementKind    = "inventory.movement"
	ReservationKind = "inventory.reservation"
)

// Identidades del contexto.
type (
	// WarehouseID identifica un almacén.
	WarehouseID struct{ fw.UUID }
	// LevelID identifica un nivel de existencias.
	LevelID struct{ fw.UUID }
	// MovementID identifica un movimiento.
	MovementID struct{ fw.UUID }
	// ReservationID identifica una reserva.
	ReservationID struct{ fw.UUID }
	// OrganizationID es la empresa, una organización interna del contexto Parties.
	OrganizationID struct{ fw.UUID }
	// ProductID es un producto del contexto Products.
	ProductID struct{ fw.UUID }
	// FacilityID es una instalación del contexto Facilities.
	FacilityID struct{ fw.UUID }
)

func zero() vocab.Decimal { return vocab.DecimalFromInt(0) }

// quantity dice si q es una cantidad positiva de hasta 4 decimales.
func quantity(q vocab.Decimal) bool { return q.IsPositive() && q.Equal(q.Round(4)) }

// WarehouseState es el estado persistido de un almacén.
type WarehouseState struct {
	Company  OrganizationID
	Code     string
	Name     string
	Facility FacilityID // dónde está, cuando el contexto Facilities lo conoce
	Active   bool
	Audit    traits.AuditStamp
}

// Warehouse es un lugar donde una empresa guarda existencias (en el C# un almacén era sólo una
// Facility referenciada por un id suelto).
type Warehouse struct {
	fw.BaseAggregateRoot[WarehouseID]
	traits.Audited
	s WarehouseState
}

// ReconstituteWarehouse reconstruye un almacén.
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

// State devuelve el estado.
func (w *Warehouse) State() WarehouseState { return w.s }

// Close retira el almacén: no admite más movimientos (el que llama comprueba que está vacío).
func (w *Warehouse) Close() error {
	if !w.s.Active {
		return fw.Violation("inventory.warehouse_closed", "the warehouse is already closed")
	}
	w.s.Active = false
	return nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (w *Warehouse) AuditSnapshot() map[string]any {
	return map[string]any{"code": w.s.Code, "active": w.s.Active}
}

// Campos de los almacenes.
var (
	WhFieldCompany = spec.Comparable("company", func(w *Warehouse) OrganizationID { return w.s.Company })
	WhFieldCode    = spec.Ordered("code", func(w *Warehouse) string { return w.s.Code })
)

// LevelState es el estado persistido de un nivel de existencias.
type LevelState struct {
	Company      OrganizationID
	Warehouse    WarehouseID
	Product      ProductID
	OnHand       vocab.Decimal
	Reserved     vocab.Decimal
	AverageCost  vocab.Decimal // coste unitario medio ponderado, 4 decimales
	ReorderPoint vocab.Decimal
	Moves        int64 // movimientos asentados: la secuencia del libro de este nivel
}

// Level son las existencias de un producto en un almacén. Sus cantidades sólo cambian mediante sus
// métodos, y a cada uno lo explica un movimiento del libro.
type Level struct {
	fw.BaseAggregateRoot[LevelID]
	s LevelState
}

// ReconstituteLevel reconstruye un nivel de existencias.
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

// OpenLevel crea el nivel vacío de un producto en un almacén.
func OpenLevel(id LevelID, company OrganizationID, warehouse WarehouseID, product ProductID) (*Level, error) {
	return ReconstituteLevel(id, LevelState{Company: company, Warehouse: warehouse, Product: product, OnHand: zero(), Reserved: zero(), AverageCost: zero(),
		ReorderPoint: zero()})
}

// State devuelve el estado.
func (l *Level) State() LevelState { return l.s }

// Available devuelve lo que aún se puede prometer: lo que hay en mano menos lo reservado.
func (l *Level) Available() vocab.Decimal { return l.s.OnHand.Sub(l.s.Reserved) }

// Value devuelve las existencias a su coste medio, al céntimo.
func (l *Level) Value() vocab.Decimal { return l.s.OnHand.Mul(l.s.AverageCost).Round(2) }

// BelowReorder dice si las existencias disponibles están en el punto de pedido o por debajo.
func (l *Level) BelowReorder() bool {
	return l.s.ReorderPoint.IsPositive() && !l.Available().GreaterThan(l.s.ReorderPoint)
}

func (l *Level) next() int64 { l.s.Moves++; return l.s.Moves }

// Receive añade existencias a un coste unitario y vuelve a calcular el coste medio ponderado.
// Devuelve la secuencia del movimiento.
func (l *Level) Receive(q, unitCost vocab.Decimal) (int64, error) {
	if !quantity(q) || unitCost.IsNegative() || !unitCost.Equal(unitCost.Round(4)) {
		return 0, fw.Violation("inventory.quantity", "a positive quantity and a non-negative cost, of up to 4 decimals")
	}
	total := l.s.OnHand.Add(q)
	l.s.AverageCost = l.s.OnHand.Mul(l.s.AverageCost).Add(q.Mul(unitCost)).Div(total).Round(4)
	l.s.OnHand = total
	return l.next(), nil
}

// Issue saca existencias al coste medio, que devuelve con la secuencia. Las existencias nunca
// quedan en negativo, y lo reservado para otros no se toma (el C# recortaba ambas cosas a cero).
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

// IssueReserved saca existencias que estaban reservadas: salen a la vez de la reserva y de las
// existencias.
func (l *Level) IssueReserved(q vocab.Decimal) (vocab.Decimal, int64, error) {
	if !quantity(q) || q.GreaterThan(l.s.Reserved) {
		return vocab.Decimal{}, 0, fw.Violation("inventory.reservation_exceeded", "more than what is reserved")
	}
	l.s.Reserved, l.s.OnHand = l.s.Reserved.Sub(q), l.s.OnHand.Sub(q)
	return l.s.AverageCost, l.next(), nil
}

// Reserve retiene existencias disponibles.
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

// Release libera existencias reservadas.
func (l *Level) Release(q vocab.Decimal) error {
	if !quantity(q) || q.GreaterThan(l.s.Reserved) {
		return fw.Violation("inventory.reservation_exceeded", "more than what is reserved")
	}
	l.s.Reserved = l.s.Reserved.Sub(q)
	return nil
}

// CountTo fija las existencias en lo contado y devuelve la diferencia y la secuencia (diferencia
// cero: ningún movimiento). Un recuento no puede quedar por debajo de lo reservado. Un sobrante
// entra al coste medio, así que el recuento no cambia el coste medio.
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

// SetReorderPoint fija la cantidad disponible por debajo de la cual hay que volver a pedir el
// producto.
func (l *Level) SetReorderPoint(p vocab.Decimal) error {
	if p.IsNegative() || !p.Equal(p.Round(4)) {
		return fw.Violation("inventory.quantity", "a non-negative reorder point of up to 4 decimals")
	}
	l.s.ReorderPoint = p
	return nil
}

// Campos de los niveles.
var (
	LvlFieldCompany   = spec.Comparable("company", func(l *Level) OrganizationID { return l.s.Company })
	LvlFieldWarehouse = spec.Comparable("warehouse_id", func(l *Level) WarehouseID { return l.s.Warehouse })
	LvlFieldProduct   = spec.Comparable("product", func(l *Level) ProductID { return l.s.Product })
	LvlFieldEmpty     = spec.Comparable("is_empty", func(l *Level) bool { return l.s.OnHand.IsZero() })
)

// MoveKind es la clase de un movimiento.
type MoveKind int

// Clases de movimiento.
const (
	Receipt MoveKind = iota + 1
	Issue
	Adjustment
	TransferOut
	TransferIn
)

var moveKinds = map[MoveKind]string{Receipt: "receipt", Issue: "issue", Adjustment: "adjustment", TransferOut: "transfer-out", TransferIn: "transfer-in"}

// String devuelve el nombre estable.
func (k MoveKind) String() string { return moveKinds[k] }

// Source es el hecho del que viene un movimiento (una recepción de Purchases, una entrega de
// Orders…).
type Source struct {
	Type string
	ID   string
}

// MovementState es el estado persistido de un movimiento.
type MovementState struct {
	Company   OrganizationID
	Warehouse WarehouseID
	Product   ProductID
	Seq       int64 // orden dentro de su nivel de existencias
	Kind      MoveKind
	Date      vocab.Date
	Quantity  vocab.Decimal // con signo: positiva si entra, negativa si sale
	UnitCost  vocab.Decimal
	Value     vocab.Decimal // cantidad × coste unitario, al céntimo, con signo
	Balance   vocab.Decimal // lo que hay en mano después del movimiento
	Lot       string        // lote o número de serie de un producto con trazabilidad
	Note      string
	Source    Source
	Audit     traits.AuditStamp
}

// Movement es una línea del libro de existencias. Nunca cambia: un error se corrige con otro
// movimiento.
type Movement struct {
	fw.BaseAggregateRoot[MovementID]
	traits.Audited
	s MovementState
}

// ReconstituteMovement reconstruye un movimiento.
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

// Post registra un movimiento de un nivel (después de que el nivel cambiara) y lo lanza.
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

// State devuelve el estado.
func (m *Movement) State() MovementState { return m.s }

// AuditSnapshot implementa traits.Snapshotter.
func (m *Movement) AuditSnapshot() map[string]any {
	return map[string]any{"kind": m.s.Kind.String(), "quantity": m.s.Quantity.String(), "balance": m.s.Balance.String()}
}

// Campos de los movimientos.
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

// ReservationState es el estado persistido de una reserva.
type ReservationState struct {
	Company   OrganizationID
	Warehouse WarehouseID
	Product   ProductID
	Source    Source        // quién retiene las existencias (una línea de pedido)
	Quantity  vocab.Decimal // reservado en total
	Open      vocab.Decimal // aún retenido: ni sacado ni liberado
	Audit     traits.AuditStamp
}

// Reservation retiene existencias de un producto en un almacén para un origen hasta que se sacan
// o se liberan (en el C# nada liberaba ni consumía nunca una reserva).
type Reservation struct {
	fw.BaseAggregateRoot[ReservationID]
	traits.Audited
	s ReservationState
}

// ReconstituteReservation reconstruye una reserva.
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

// State devuelve el estado.
func (r *Reservation) State() ReservationState { return r.s }

// Hold crea la reserva de un origen y la lanza.
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

// Add retiene más existencias para el mismo origen (una falta que se cubre más tarde).
func (r *Reservation) Add(q vocab.Decimal) error {
	if !quantity(q) {
		return fw.Violation("inventory.quantity", "a positive quantity of up to 4 decimals")
	}
	r.s.Quantity, r.s.Open = r.s.Quantity.Add(q), r.s.Open.Add(q)
	r.Raise(StockReserved{EventMeta: r.NewEventMeta(), Company: r.s.Company.String(), Warehouse: r.s.Warehouse.String(), Product: r.s.Product.String(),
		SourceType: r.s.Source.Type, SourceID: r.s.Source.ID, Added: q.String(), Held: r.s.Open.String()})
	return nil
}

// Consume rebaja lo retenido en una cantidad sacada.
func (r *Reservation) Consume(q vocab.Decimal) error {
	if !quantity(q) || q.GreaterThan(r.s.Open) {
		return fw.Violation("inventory.reservation_exceeded", "the reservation holds only "+r.s.Open.String())
	}
	r.s.Open = r.s.Open.Sub(q)
	return nil
}

// Release libera lo que aún está retenido y lo devuelve.
func (r *Reservation) Release() (vocab.Decimal, error) {
	if r.s.Open.IsZero() {
		return vocab.Decimal{}, fw.Violation("inventory.reservation_closed", "the reservation holds nothing")
	}
	q := r.s.Open
	r.s.Open = zero()
	return q, nil
}

// AuditSnapshot implementa traits.Snapshotter.
func (r *Reservation) AuditSnapshot() map[string]any {
	return map[string]any{"quantity": r.s.Quantity.String(), "open": r.s.Open.String()}
}

// Campos de las reservas.
var (
	ResFieldCompany   = spec.Comparable("company", func(r *Reservation) OrganizationID { return r.s.Company })
	ResFieldWarehouse = spec.Comparable("warehouse_id", func(r *Reservation) WarehouseID { return r.s.Warehouse })
	ResFieldProduct   = spec.Comparable("product", func(r *Reservation) ProductID { return r.s.Product })
	ResFieldSrcType   = spec.Comparable("source_type", func(r *Reservation) string { return r.s.Source.Type })
	ResFieldSrcID     = spec.Comparable("source_id", func(r *Reservation) string { return r.s.Source.ID })
	ResFieldClosed    = spec.Comparable("closed", func(r *Reservation) bool { return r.s.Open.IsZero() })
)

// StockMoved lo lanza cada movimiento del libro.
type StockMoved struct {
	fw.EventMeta
	Snapshot MovementState `json:"snapshot"`
}

// EventType implementa fw.Event.
func (StockMoved) EventType() string { return "inventory.stock_moved" }

// StockReserved se lanza cuando se retienen existencias para un origen: Added ahora, Held en
// total.
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

// EventType implementa fw.Event.
func (StockReserved) EventType() string { return "inventory.stock_reserved" }

// Identidades nuevas e interpretación.
func NewWarehouseID() WarehouseID     { return WarehouseID{fw.NewUUID()} }
func NewLevelID() LevelID             { return LevelID{fw.NewUUID()} }
func NewMovementID() MovementID       { return MovementID{fw.NewUUID()} }
func NewReservationID() ReservationID { return ReservationID{fw.NewUUID()} }

// ParseWarehouseID interpreta una identidad en texto.
func ParseWarehouseID(s string) (WarehouseID, error) {
	u, err := fw.ParseUUID(s)
	return WarehouseID{u}, err
}

// ParseReservationID interpreta una identidad en texto.
func ParseReservationID(s string) (ReservationID, error) {
	u, err := fw.ParseUUID(s)
	return ReservationID{u}, err
}

// Repositorios del contexto.
type (
	WarehouseRepository   = fw.Repository[WarehouseID, *Warehouse]
	LevelRepository       = fw.Repository[LevelID, *Level]
	MovementRepository    = fw.Repository[MovementID, *Movement]
	ReservationRepository = fw.Repository[ReservationID, *Reservation]
)

// Item es lo que Inventario necesita saber de un producto.
type Item struct {
	Company      OrganizationID
	SKU          string
	Name         string
	Stocked      bool
	Tracked      bool // por lote o número de serie: sus movimientos llevan uno
	Discontinued vocab.Date
}

// Catalog resuelve productos (un puerto de Inventario sobre el contrato Catalog de Products).
type Catalog interface {
	Items(ctx context.Context, products []ProductID) (map[ProductID]Item, error)
}

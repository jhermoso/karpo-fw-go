package application

import (
	"context"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/pipeline"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// CreateWarehouse opens a warehouse of a company.
type CreateWarehouse struct {
	Company  string `json:"company"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Facility string `json:"facility,omitempty"`
}

// CloseWarehouse retires an empty warehouse.
type CloseWarehouse struct {
	ID domain.WarehouseID `json:"-"`
}

// SearchWarehouses lists the warehouses of a company.
type SearchWarehouses struct{ Company string }

// WarehouseDTO is the transport form of a warehouse.
type WarehouseDTO struct {
	ID       string `json:"id"`
	Company  string `json:"company"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Facility string `json:"facility,omitempty"`
	Active   bool   `json:"active"`
}

func warehouseDTO(w *domain.Warehouse) WarehouseDTO {
	s := w.State()
	d := WarehouseDTO{ID: w.ID().String(), Company: s.Company.String(), Code: s.Code, Name: s.Name, Active: s.Active}
	if !s.Facility.IsZero() {
		d.Facility = s.Facility.String()
	}
	return d
}

// Receive puts stock in a warehouse at a unit cost.
type Receive struct {
	Warehouse  string     `json:"warehouse"`
	Product    string     `json:"product"`
	Quantity   string     `json:"quantity"`
	UnitCost   string     `json:"unitCost"`
	Date       vocab.Date `json:"date,omitzero"`
	Lot        string     `json:"lot,omitempty"`
	Note       string     `json:"note,omitempty"`
	SourceType string     `json:"sourceType,omitempty"`
	SourceID   string     `json:"sourceId,omitempty"`
}

// Issue takes stock out of a warehouse, optionally from a reservation.
type Issue struct {
	Warehouse   string     `json:"warehouse"`
	Product     string     `json:"product"`
	Quantity    string     `json:"quantity"`
	Date        vocab.Date `json:"date,omitzero"`
	Lot         string     `json:"lot,omitempty"`
	Note        string     `json:"note,omitempty"`
	Reservation string     `json:"reservation,omitempty"`
	SourceType  string     `json:"sourceType,omitempty"`
	SourceID    string     `json:"sourceId,omitempty"`
}

// Adjust sets the stock of a product in a warehouse to what was counted, with a reason.
type Adjust struct {
	Warehouse string     `json:"warehouse"`
	Product   string     `json:"product"`
	Counted   string     `json:"counted"`
	Date      vocab.Date `json:"date,omitzero"`
	Reason    string     `json:"reason"`
}

// Transfer moves stock between two warehouses of the same company, at its cost.
type Transfer struct {
	From     string     `json:"from"`
	To       string     `json:"to"`
	Product  string     `json:"product"`
	Quantity string     `json:"quantity"`
	Date     vocab.Date `json:"date,omitzero"`
	Lot      string     `json:"lot,omitempty"`
	Note     string     `json:"note,omitempty"`
}

// Reserve holds available stock for a source (an order line).
type Reserve struct {
	Warehouse  string `json:"warehouse"`
	Product    string `json:"product"`
	Quantity   string `json:"quantity"`
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
}

// Release frees what a reservation still holds.
type Release struct {
	ID domain.ReservationID `json:"-"`
}

// SetReorderPoint sets the reorder point of a product in a warehouse.
type SetReorderPoint struct {
	Warehouse string `json:"warehouse"`
	Product   string `json:"product"`
	Point     string `json:"point"`
}

// MovementDTO is the transport form of a movement.
type MovementDTO struct {
	ID         string `json:"id"`
	Warehouse  string `json:"warehouse"`
	Product    string `json:"product"`
	Seq        int64  `json:"seq"`
	Kind       string `json:"kind"`
	Date       string `json:"date"`
	Quantity   string `json:"quantity"`
	UnitCost   string `json:"unitCost"`
	Value      string `json:"value"`
	Balance    string `json:"balance"`
	Lot        string `json:"lot,omitempty"`
	Note       string `json:"note,omitempty"`
	SourceType string `json:"sourceType,omitempty"`
	SourceID   string `json:"sourceId,omitempty"`
}

func movementDTO(m *domain.Movement) MovementDTO {
	s := m.State()
	return MovementDTO{ID: m.ID().String(), Warehouse: s.Warehouse.String(), Product: s.Product.String(), Seq: s.Seq, Kind: s.Kind.String(),
		Date: s.Date.String(), Quantity: s.Quantity.String(), UnitCost: s.UnitCost.StringFixed(4), Value: s.Value.StringFixed(2), Balance: s.Balance.String(),
		Lot: s.Lot, Note: s.Note, SourceType: s.Source.Type, SourceID: s.Source.ID}
}

// LevelDTO is the transport form of a stock level.
type LevelDTO struct {
	Warehouse    string `json:"warehouse"`
	Product      string `json:"product"`
	SKU          string `json:"sku,omitempty"`
	Name         string `json:"name,omitempty"`
	OnHand       string `json:"onHand"`
	Reserved     string `json:"reserved"`
	Available    string `json:"available"`
	AverageCost  string `json:"averageCost"`
	Value        string `json:"value"`
	ReorderPoint string `json:"reorderPoint"`
	BelowReorder bool   `json:"belowReorder"`
}

func levelDTO(l *domain.Level, it domain.Item) LevelDTO {
	s := l.State()
	return LevelDTO{Warehouse: s.Warehouse.String(), Product: s.Product.String(), SKU: it.SKU, Name: it.Name, OnHand: s.OnHand.String(),
		Reserved: s.Reserved.String(), Available: l.Available().String(), AverageCost: s.AverageCost.StringFixed(4), Value: l.Value().StringFixed(2),
		ReorderPoint: s.ReorderPoint.String(), BelowReorder: l.BelowReorder()}
}

// ReservationDTO is the transport form of a reservation.
type ReservationDTO struct {
	ID         string `json:"id"`
	Warehouse  string `json:"warehouse"`
	Product    string `json:"product"`
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
	Quantity   string `json:"quantity"`
	Open       string `json:"open"`
}

func reservationDTO(r *domain.Reservation) ReservationDTO {
	s := r.State()
	return ReservationDTO{ID: r.ID().String(), Warehouse: s.Warehouse.String(), Product: s.Product.String(), SourceType: s.Source.Type,
		SourceID: s.Source.ID, Quantity: s.Quantity.String(), Open: s.Open.String()}
}

func (s service) warehouseUseCases(svc *Service) {
	svc.CreateWarehouse = guard(PermWarehouseUpdate, func(ctx context.Context, c CreateWarehouse) (WarehouseDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		st := domain.WarehouseState{Company: company, Code: c.Code, Name: c.Name, Active: true}
		if c.Facility != "" {
			st.Facility = domain.FacilityID{UUID: parseID(&v, "facility", c.Facility)}
		}
		if err := v.Err(); err != nil {
			return WarehouseDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return WarehouseDTO{}, err
		}
		w, err := domain.ReconstituteWarehouse(domain.NewWarehouseID(), st)
		if err != nil {
			return WarehouseDTO{}, err
		}
		dup, err := s.Warehouses.Exists(ctx, spec.And(domain.WhFieldCompany.Eq(company), domain.WhFieldCode.Eq(w.State().Code)))
		if err != nil {
			return WarehouseDTO{}, err
		}
		if dup {
			return WarehouseDTO{}, fw.Violation("inventory.duplicate_warehouse", "the company already has that warehouse code")
		}
		if err := s.warehouses.Create(ctx, w); err != nil {
			return WarehouseDTO{}, err
		}
		return warehouseDTO(w), nil
	}, pipeline.Transactional[CreateWarehouse, WarehouseDTO](s.UoW))

	svc.CloseWarehouse = guard(PermWarehouseUpdate, func(ctx context.Context, c CloseWarehouse) (WarehouseDTO, error) {
		sc := scopeOf(ctx)
		w, err := s.warehouses.Update(ctx, c.ID, func(ctx context.Context, w *domain.Warehouse) error {
			if err := sc.check(domain.WarehouseKind, w.ID(), w.State().Company, true); err != nil {
				return err
			}
			stocked, err := s.Levels.Exists(ctx, spec.And(domain.LvlFieldWarehouse.Eq(w.ID()), domain.LvlFieldEmpty.Eq(false)))
			if err != nil {
				return err
			}
			if stocked {
				return fw.Violation("inventory.warehouse_not_empty", "the warehouse still has stock")
			}
			return w.Close()
		})
		if err != nil {
			return WarehouseDTO{}, err
		}
		return warehouseDTO(w), nil
	}, pipeline.Transactional[CloseWarehouse, WarehouseDTO](s.UoW))

	svc.SearchWarehouses = guard(PermWarehouseRead, func(ctx context.Context, q SearchWarehouses) ([]WarehouseDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return nil, err
		}
		ws, err := s.Warehouses.Find(ctx, domain.WhFieldCompany.Eq(company), domain.WhFieldCode.Asc())
		if err != nil {
			return nil, err
		}
		out := []WarehouseDTO{}
		for _, w := range ws {
			out = append(out, warehouseDTO(w))
		}
		return out, nil
	})
}

func (s service) stockUseCases(svc *Service) {
	svc.Receive = moving(s, PermStockMove, func(ctx context.Context, c Receive) (MovementDTO, error) {
		var v fw.Validation
		wid := domain.WarehouseID{UUID: parseID(&v, "warehouse", c.Warehouse)}
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		q, cost := parseDecimal(&v, "quantity", c.Quantity), parseDecimal(&v, "unitCost", c.UnitCost)
		if err := v.Err(); err != nil {
			return MovementDTO{}, err
		}
		w, err := s.warehouse(ctx, wid, true)
		if err != nil {
			return MovementDTO{}, err
		}
		src := domain.Source{Type: c.SourceType, ID: c.SourceID}
		if m, err := s.posted(ctx, wid, product, domain.Receipt, src); err != nil || m != nil {
			if err != nil {
				return MovementDTO{}, err
			}
			return movementDTO(m), nil
		}
		it, err := s.item(ctx, w.State().Company, product, strings.TrimSpace(c.Lot), true)
		if err != nil {
			return MovementDTO{}, err
		}
		date := today(c.Date)
		if !it.Discontinued.IsZero() && !date.Before(it.Discontinued) {
			return MovementDTO{}, fw.Violation("inventory.discontinued", "the product is discontinued: it is not received any more")
		}
		l, err := s.level(ctx, w, product)
		if err != nil {
			return MovementDTO{}, err
		}
		seq, err := l.Receive(q, cost)
		if err != nil {
			return MovementDTO{}, err
		}
		m, err := s.post(ctx, l, seq, domain.Receipt, date, q, cost, c.Lot, c.Note, src)
		if err != nil {
			return MovementDTO{}, err
		}
		return movementDTO(m), nil
	})

	svc.Issue = moving(s, PermStockMove, func(ctx context.Context, c Issue) (MovementDTO, error) {
		var v fw.Validation
		wid := domain.WarehouseID{UUID: parseID(&v, "warehouse", c.Warehouse)}
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		q := parseDecimal(&v, "quantity", c.Quantity)
		var rid domain.ReservationID
		if c.Reservation != "" {
			rid = domain.ReservationID{UUID: parseID(&v, "reservation", c.Reservation)}
		}
		if err := v.Err(); err != nil {
			return MovementDTO{}, err
		}
		w, err := s.warehouse(ctx, wid, true)
		if err != nil {
			return MovementDTO{}, err
		}
		src := domain.Source{Type: c.SourceType, ID: c.SourceID}
		if m, err := s.posted(ctx, wid, product, domain.Issue, src); err != nil || m != nil {
			if err != nil {
				return MovementDTO{}, err
			}
			return movementDTO(m), nil
		}
		if _, err := s.item(ctx, w.State().Company, product, strings.TrimSpace(c.Lot), true); err != nil {
			return MovementDTO{}, err
		}
		l, err := s.level(ctx, w, product)
		if err != nil {
			return MovementDTO{}, err
		}
		var cost vocab.Decimal
		var seq int64
		if rid.IsZero() {
			cost, seq, err = l.Issue(q)
		} else {
			if _, err = s.reservations.Update(ctx, rid, func(_ context.Context, r *domain.Reservation) error {
				if rs := r.State(); rs.Warehouse != wid || rs.Product != product {
					return fw.Violation("inventory.reservation_mismatch", "the reservation is of another product or warehouse")
				}
				return r.Consume(q)
			}); err != nil {
				return MovementDTO{}, err
			}
			cost, seq, err = l.IssueReserved(q)
		}
		if err != nil {
			return MovementDTO{}, err
		}
		m, err := s.post(ctx, l, seq, domain.Issue, today(c.Date), q.Neg(), cost, c.Lot, c.Note, src)
		if err != nil {
			return MovementDTO{}, err
		}
		return movementDTO(m), nil
	})

	svc.Adjust = moving(s, PermStockAdjust, func(ctx context.Context, c Adjust) (LevelDTO, error) {
		var v fw.Validation
		wid := domain.WarehouseID{UUID: parseID(&v, "warehouse", c.Warehouse)}
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		counted := parseDecimal(&v, "counted", c.Counted)
		v.Require(strings.TrimSpace(c.Reason) != "", "reason", "required", "a count is recorded with its reason")
		if err := v.Err(); err != nil {
			return LevelDTO{}, err
		}
		w, err := s.warehouse(ctx, wid, true)
		if err != nil {
			return LevelDTO{}, err
		}
		it, err := s.item(ctx, w.State().Company, product, "", false)
		if err != nil {
			return LevelDTO{}, err
		}
		l, err := s.level(ctx, w, product)
		if err != nil {
			return LevelDTO{}, err
		}
		diff, seq, err := l.CountTo(counted)
		if err != nil {
			return LevelDTO{}, err
		}
		if !diff.IsZero() {
			if _, err := s.post(ctx, l, seq, domain.Adjustment, today(c.Date), diff, l.State().AverageCost, "", c.Reason, domain.Source{}); err != nil {
				return LevelDTO{}, err
			}
		}
		return levelDTO(l, it), nil
	})

	svc.Transfer = moving(s, PermStockMove, func(ctx context.Context, c Transfer) ([]MovementDTO, error) {
		var v fw.Validation
		from := domain.WarehouseID{UUID: parseID(&v, "from", c.From)}
		to := domain.WarehouseID{UUID: parseID(&v, "to", c.To)}
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		q := parseDecimal(&v, "quantity", c.Quantity)
		v.Require(from != to, "to", "same", "a transfer goes to another warehouse")
		if err := v.Err(); err != nil {
			return nil, err
		}
		src, err := s.warehouse(ctx, from, true)
		if err != nil {
			return nil, err
		}
		dst, err := s.warehouse(ctx, to, true)
		if err != nil {
			return nil, err
		}
		if src.State().Company != dst.State().Company {
			return nil, fw.Violation("inventory.transfer_company", "a transfer stays within one company")
		}
		if _, err := s.item(ctx, src.State().Company, product, strings.TrimSpace(c.Lot), true); err != nil {
			return nil, err
		}
		out, err := s.level(ctx, src, product)
		if err != nil {
			return nil, err
		}
		in, err := s.level(ctx, dst, product)
		if err != nil {
			return nil, err
		}
		cost, seqOut, err := out.Issue(q)
		if err != nil {
			return nil, err
		}
		seqIn, err := in.Receive(q, cost)
		if err != nil {
			return nil, err
		}
		date := today(c.Date)
		// The two movements point to each other through the id of the outgoing one.
		mo, err := s.post(ctx, out, seqOut, domain.TransferOut, date, q.Neg(), cost, c.Lot, c.Note, domain.Source{})
		if err != nil {
			return nil, err
		}
		mi, err := s.post(ctx, in, seqIn, domain.TransferIn, date, q, cost, c.Lot, c.Note, domain.Source{Type: "inventory.transfer", ID: mo.ID().String()})
		if err != nil {
			return nil, err
		}
		return []MovementDTO{movementDTO(mo), movementDTO(mi)}, nil
	})

	svc.Reserve = moving(s, PermStockMove, func(ctx context.Context, c Reserve) (ReservationDTO, error) {
		var v fw.Validation
		wid := domain.WarehouseID{UUID: parseID(&v, "warehouse", c.Warehouse)}
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		q := parseDecimal(&v, "quantity", c.Quantity)
		if err := v.Err(); err != nil {
			return ReservationDTO{}, err
		}
		w, err := s.warehouse(ctx, wid, true)
		if err != nil {
			return ReservationDTO{}, err
		}
		if _, err := s.item(ctx, w.State().Company, product, "", false); err != nil {
			return ReservationDTO{}, err
		}
		r, err := domain.ReconstituteReservation(domain.NewReservationID(), domain.ReservationState{Company: w.State().Company, Warehouse: wid,
			Product: product, Source: domain.Source{Type: strings.TrimSpace(c.SourceType), ID: strings.TrimSpace(c.SourceID)}, Quantity: q, Open: q})
		if err != nil {
			return ReservationDTO{}, err
		}
		held, err := s.Reservations.Exists(ctx, spec.And(domain.ResFieldWarehouse.Eq(wid), domain.ResFieldProduct.Eq(product),
			domain.ResFieldSrcType.Eq(r.State().Source.Type), domain.ResFieldSrcID.Eq(r.State().Source.ID), domain.ResFieldClosed.Eq(false)))
		if err != nil {
			return ReservationDTO{}, err
		}
		if held {
			return ReservationDTO{}, fw.Violation("inventory.already_reserved", "that source already holds stock of the product in the warehouse")
		}
		l, err := s.level(ctx, w, product)
		if err != nil {
			return ReservationDTO{}, err
		}
		if err := l.Reserve(q); err != nil {
			return ReservationDTO{}, err
		}
		if err := s.Levels.Save(ctx, l); err != nil {
			return ReservationDTO{}, err
		}
		if err := s.reservations.Create(ctx, r); err != nil {
			return ReservationDTO{}, err
		}
		return reservationDTO(r), nil
	})

	svc.Release = moving(s, PermStockMove, func(ctx context.Context, c Release) (ReservationDTO, error) {
		sc := scopeOf(ctx)
		r, err := s.reservations.Update(ctx, c.ID, func(ctx context.Context, r *domain.Reservation) error {
			rs := r.State()
			if err := sc.check(domain.ReservationKind, r.ID(), rs.Company, true); err != nil {
				return err
			}
			q, err := r.Release()
			if err != nil {
				return err
			}
			ls, err := s.Levels.Find(ctx, spec.And(domain.LvlFieldWarehouse.Eq(rs.Warehouse), domain.LvlFieldProduct.Eq(rs.Product)))
			if err != nil {
				return err
			}
			if len(ls) == 0 {
				return fw.NotFound(domain.LevelKind, rs.Product)
			}
			if err := ls[0].Release(q); err != nil {
				return err
			}
			return s.Levels.Save(ctx, ls[0])
		})
		if err != nil {
			return ReservationDTO{}, err
		}
		return reservationDTO(r), nil
	})

	svc.SetReorderPoint = moving(s, PermWarehouseUpdate, func(ctx context.Context, c SetReorderPoint) (LevelDTO, error) {
		var v fw.Validation
		wid := domain.WarehouseID{UUID: parseID(&v, "warehouse", c.Warehouse)}
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		point := parseDecimal(&v, "point", c.Point)
		if err := v.Err(); err != nil {
			return LevelDTO{}, err
		}
		w, err := s.warehouse(ctx, wid, true)
		if err != nil {
			return LevelDTO{}, err
		}
		it, err := s.item(ctx, w.State().Company, product, "", false)
		if err != nil {
			return LevelDTO{}, err
		}
		l, err := s.level(ctx, w, product)
		if err != nil {
			return LevelDTO{}, err
		}
		if err := l.SetReorderPoint(point); err != nil {
			return LevelDTO{}, err
		}
		if err := s.Levels.Save(ctx, l); err != nil {
			return LevelDTO{}, err
		}
		return levelDTO(l, it), nil
	})
}

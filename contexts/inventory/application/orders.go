package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/jhermoso/karpo-fw-go/contexts/inventory/domain"
	app "github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/application/messaging"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// The Inventory copies of the Orders events it consumes (only the fields it needs).

// OrderSource is the source type of the reservations and issues of sales orders: the source id is
// the order and its line, "order|line".
const OrderSource = "orders.sales-order"

// StockRequested is orders.stock-requested.v1: the quantities of an order to hold.
type StockRequested struct {
	OrderID   string `json:"orderId"`
	Company   string `json:"company"`
	Warehouse string `json:"warehouse"`
	Lines     []struct {
		Line     int    `json:"line"`
		Product  string `json:"product"`
		Quantity string `json:"quantity"`
	} `json:"lines"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (StockRequested) IntegrationEventType() string { return "orders.stock-requested.v1" }

// DeliveryIssued is orders.delivery-issued.v1: what left the warehouse for an order.
type DeliveryIssued struct {
	DeliveryID string `json:"deliveryId"`
	OrderID    string `json:"orderId"`
	Company    string `json:"company"`
	Warehouse  string `json:"warehouse"`
	Number     string `json:"number"`
	Date       string `json:"date"`
	Lines      []struct {
		Line     int    `json:"line"`
		Product  string `json:"product"`
		Quantity string `json:"quantity"`
		Stocked  bool   `json:"stocked"`
	} `json:"lines"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (DeliveryIssued) IntegrationEventType() string { return "orders.delivery-issued.v1" }

// OrderClosed is orders.order-closed.v1: an order cancelled or closed, with the lines that may
// still hold stock.
type OrderClosed struct {
	OrderID string `json:"orderId"`
	Company string `json:"company"`
	Lines   []int  `json:"lines"`
}

// IntegrationEventType implements application.IntegrationEvent.
func (OrderClosed) IntegrationEventType() string { return "orders.order-closed.v1" }

func orderLine(order string, line int) domain.Source {
	return domain.Source{Type: OrderSource, ID: fmt.Sprintf("%s|%d", order, line)}
}

// Subscribe registers the reactions to Orders (approved decisions of docs/PEDIDOS.md: Orders
// publishes, Inventory moves the stock):
//
//   - stock requested: each line holds what is available of its product in the warehouse, up to
//     what is asked (nothing when there is none: the order sees the shortage and asks again);
//   - delivery issued: each stocked line leaves the warehouse against its reservation, at the
//     average cost;
//   - order closed: what its lines still hold is released.
//
// Redeliveries are harmless: the inbox drops them and an issue is unique by delivery and line.
func Subscribe(c *messaging.Consumer, d Deps) {
	s := newService(d)
	held := func(ctx context.Context, w domain.WarehouseID, src domain.Source) (*domain.Reservation, error) {
		rs, err := s.Reservations.Find(ctx, spec.And(domain.ResFieldWarehouse.Eq(w), domain.ResFieldSrcType.Eq(src.Type), domain.ResFieldSrcID.Eq(src.ID),
			domain.ResFieldClosed.Eq(false)))
		if err != nil || len(rs) == 0 {
			return nil, err
		}
		return rs[0], nil
	}
	invalid := func(event string, err error) error {
		return fw.Violation("inventory.invalid_event", event+": "+err.Error())
	}

	messaging.Handle(c, func(ctx context.Context, e StockRequested, _ app.Envelope) error {
		company, err1 := fw.ParseUUID(e.Company)
		wid, err2 := domain.ParseWarehouseID(e.Warehouse)
		if err := errors.Join(err1, err2); err != nil {
			return invalid("orders.stock-requested.v1", err)
		}
		w, err := s.Warehouses.Get(ctx, wid)
		if err != nil {
			return err
		}
		if w.State().Company.UUID != company || !w.State().Active {
			return nil // nothing to hold in a warehouse that is closed or of another company
		}
		for _, line := range e.Lines {
			pu, err1 := fw.ParseUUID(line.Product)
			q, err2 := vocab.ParseDecimal(line.Quantity)
			if err := errors.Join(err1, err2); err != nil {
				return invalid("orders.stock-requested.v1", err)
			}
			product := domain.ProductID{UUID: pu}
			if _, err := s.item(ctx, w.State().Company, product, "", false); err != nil {
				if errors.Is(err, fw.ErrRuleViolation) {
					continue // not a stocked product of the company: nothing to hold
				}
				return err
			}
			l, err := s.level(ctx, w, product)
			if err != nil {
				return err
			}
			take := q
			if l.Available().LessThan(take) {
				take = l.Available()
			}
			if !take.IsPositive() {
				continue
			}
			if err := l.Reserve(take); err != nil {
				return err
			}
			if err := s.Levels.Save(ctx, l); err != nil {
				return err
			}
			src := orderLine(e.OrderID, line.Line)
			r, err := held(ctx, wid, src)
			if err != nil {
				return err
			}
			if r != nil {
				_, err = s.reservations.Update(ctx, r.ID(), func(_ context.Context, r *domain.Reservation) error { return r.Add(take) })
			} else if r, err = domain.Hold(domain.NewReservationID(), domain.ReservationState{Company: w.State().Company, Warehouse: wid, Product: product,
				Source: src, Quantity: take}); err == nil {
				err = s.reservations.Create(ctx, r)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})

	messaging.Handle(c, func(ctx context.Context, e DeliveryIssued, _ app.Envelope) error {
		wid, err1 := domain.ParseWarehouseID(e.Warehouse)
		date, err2 := vocab.ParseDate(e.Date)
		if err := errors.Join(err1, err2); err != nil {
			return invalid("orders.delivery-issued.v1", err)
		}
		w, err := s.Warehouses.Get(ctx, wid)
		if err != nil {
			return err
		}
		for _, line := range e.Lines {
			if !line.Stocked {
				continue
			}
			pu, err1 := fw.ParseUUID(line.Product)
			q, err2 := vocab.ParseDecimal(line.Quantity)
			if err := errors.Join(err1, err2); err != nil {
				return invalid("orders.delivery-issued.v1", err)
			}
			product := domain.ProductID{UUID: pu}
			src := domain.Source{Type: "orders.delivery", ID: fmt.Sprintf("%s|%d", e.DeliveryID, line.Line)}
			if m, err := s.posted(ctx, wid, product, domain.Issue, src); err != nil || m != nil {
				if err != nil {
					return err
				}
				continue
			}
			l, err := s.level(ctx, w, product)
			if err != nil {
				return err
			}
			r, err := held(ctx, wid, orderLine(e.OrderID, line.Line))
			if err != nil {
				return err
			}
			// What the order line holds leaves the reserve; anything beyond it, the free stock.
			fromReserve := vocab.DecimalFromInt(0)
			if r != nil {
				fromReserve = q
				if r.State().Open.LessThan(q) {
					fromReserve = r.State().Open
				}
			}
			var cost vocab.Decimal
			var seq int64
			if fromReserve.IsPositive() {
				if _, err := s.reservations.Update(ctx, r.ID(), func(_ context.Context, r *domain.Reservation) error { return r.Consume(fromReserve) }); err != nil {
					return err
				}
				if cost, seq, err = l.IssueReserved(fromReserve); err != nil {
					return err
				}
			}
			if rest := q.Sub(fromReserve); rest.IsPositive() {
				if cost, seq, err = l.Issue(rest); err != nil {
					return err
				}
			}
			if _, err := s.post(ctx, l, seq, domain.Issue, date, q.Neg(), cost, "", "Albarán "+e.Number, src); err != nil {
				return err
			}
		}
		return nil
	})

	messaging.Handle(c, func(ctx context.Context, e OrderClosed, _ app.Envelope) error {
		for _, line := range e.Lines {
			src := orderLine(e.OrderID, line)
			rs, err := s.Reservations.Find(ctx, spec.And(domain.ResFieldSrcType.Eq(src.Type), domain.ResFieldSrcID.Eq(src.ID), domain.ResFieldClosed.Eq(false)))
			if err != nil {
				return err
			}
			for _, r := range rs {
				rs := r.State()
				var q vocab.Decimal
				if _, err := s.reservations.Update(ctx, r.ID(), func(_ context.Context, r *domain.Reservation) error {
					var err error
					q, err = r.Release()
					return err
				}); err != nil {
					return err
				}
				ls, err := s.Levels.Find(ctx, spec.And(domain.LvlFieldWarehouse.Eq(rs.Warehouse), domain.LvlFieldProduct.Eq(rs.Product)))
				if err != nil {
					return err
				}
				if len(ls) == 0 {
					continue
				}
				if err := ls[0].Release(q); err != nil {
					return err
				}
				if err := s.Levels.Save(ctx, ls[0]); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

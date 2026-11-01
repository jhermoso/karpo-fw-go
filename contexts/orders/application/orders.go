package application

import (
	"context"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// SetTerms crea o sustituye las condiciones de venta de un cliente en una empresa.
type SetTerms struct {
	Company       string `json:"company"`
	Customer      string `json:"customer"`
	PriceList     string `json:"priceList,omitempty"`
	Discount      string `json:"discount,omitempty"`
	BlockOrders   bool   `json:"blockOrders,omitempty"`
	BlockDelivery bool   `json:"blockDelivery,omitempty"`
	// BlockReason dice por qué se bloquea (D-122); sin bloqueo no se guarda.
	BlockReason string `json:"blockReason,omitempty"`
}

// SearchTerms enumera las condiciones de cliente de una empresa; las de una party fusionada, sólo
// con IncludeMergedParties (D-100).
type SearchTerms struct {
	Company              string
	IncludeMergedParties bool
}

// TermsDTO es la forma de transporte de las condiciones de un cliente.
type TermsDTO struct {
	ID            string `json:"id"`
	Company       string `json:"company"`
	Customer      string `json:"customer"`
	PriceList     string `json:"priceList,omitempty"`
	Discount      string `json:"discount"`
	BlockOrders   bool   `json:"blockOrders"`
	BlockDelivery bool   `json:"blockDelivery"`
	BlockReason   string `json:"blockReason,omitempty"` // D-122
	Version       int64  `json:"version"`
}

func termsDTO(t *domain.Terms) TermsDTO {
	s := t.State()
	return TermsDTO{ID: t.ID().String(), Company: s.Company.String(), Customer: s.Customer.String(), PriceList: optID(s.PriceList.UUID),
		Discount: money(s.Discount), BlockOrders: s.BlockOrders, BlockDelivery: s.BlockDelivery, BlockReason: s.BlockReason, Version: t.Version()}
}

// DraftOrder abre un pedido en borrador de un cliente, con la tarifa y el descuento de sus
// condiciones.
type DraftOrder struct {
	Company   string     `json:"company"`
	Customer  string     `json:"customer"`
	Date      vocab.Date `json:"date,omitzero"`
	Warehouse string     `json:"warehouse,omitempty"`
	Reference string     `json:"reference,omitempty"`
	Notes     string     `json:"notes,omitempty"`
}

// AddLine añade un producto a un borrador, con el precio de Products.
type AddLine struct {
	ID       domain.OrderID `json:"-"`
	Product  string         `json:"product"`
	Quantity string         `json:"quantity"`
}

// RemoveLine quita una línea de un borrador.
type RemoveLine struct {
	ID   domain.OrderID `json:"-"`
	Line int            `json:"line"`
}

// ConfirmOrder confirma un borrador: se numera y se piden sus existencias.
type ConfirmOrder struct {
	ID domain.OrderID `json:"-"`
}

// RequestStock vuelve a pedir a Inventory lo que le falta a un pedido confirmado.
type RequestStock struct {
	ID domain.OrderID `json:"-"`
}

// PickInput es una cantidad de una línea que se entrega.
type PickInput struct {
	Line     int    `json:"line"`
	Quantity string `json:"quantity"`
}

// Deliver sirve parte de un pedido con un albarán. Sin líneas, todo lo que se puede entregar: lo
// retenido de las líneas con existencias y lo pendiente de las demás.
type Deliver struct {
	ID    domain.OrderID `json:"-"`
	Date  vocab.Date     `json:"date,omitzero"`
	Lines []PickInput    `json:"lines,omitempty"`
}

// EndOrder anula o cierra un pedido, con un motivo.
type EndOrder struct {
	ID     domain.OrderID `json:"-"`
	Reason string         `json:"reason"`
}

// GetOrder carga un pedido.
type GetOrder struct{ ID domain.OrderID }

// SearchOrders busca pedidos del ámbito de quien llama.
type SearchOrders struct {
	Company, Customer, Status string
	IncludeMergedParties      bool // también los de clientes fusionados en otra party (D-100)
	Page, Size                int
}

// GetDelivery carga un albarán.
type GetDelivery struct{ ID domain.DeliveryID }

// SearchDeliveries busca albaranes del ámbito de quien llama.
type SearchDeliveries struct {
	Company, Customer, Order string
	Uninvoiced               bool // sólo los que aún no están en ninguna factura
	IncludeMergedParties     bool // también los de clientes fusionados en otra party (D-100)
	Page, Size               int
}

// LineDTO es la forma de transporte de una línea de pedido.
type LineDTO struct {
	No          int    `json:"no"`
	Product     string `json:"product"`
	SKU         string `json:"sku"`
	Description string `json:"description"`
	UoM         string `json:"uom"`
	TaxCode     string `json:"taxCode,omitempty"`
	Stocked     bool   `json:"stocked"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unitPrice"`
	Discount    string `json:"discount"`
	NetPrice    string `json:"netPrice"`
	Amount      string `json:"amount"`
	Reserved    string `json:"reserved"`
	Delivered   string `json:"delivered"`
	Pending     string `json:"pending"`
	Short       string `json:"short"`
}

// OrderDTO es la forma de transporte de un pedido.
type OrderDTO struct {
	ID               string    `json:"id"`
	Company          string    `json:"company"`
	Customer         string    `json:"customer"`
	Number           string    `json:"number,omitempty"`
	Date             string    `json:"date"`
	Warehouse        string    `json:"warehouse,omitempty"`
	PriceList        string    `json:"priceList,omitempty"`
	CustomerDiscount string    `json:"customerDiscount"`
	Reference        string    `json:"reference,omitempty"`
	Notes            string    `json:"notes,omitempty"`
	Status           string    `json:"status"`
	Total            string    `json:"total"`
	Pending          string    `json:"pending"`
	CloseReason      string    `json:"closeReason,omitempty"`
	Lines            []LineDTO `json:"lines"`
	Version          int64     `json:"version"`
}

func orderDTO(o *domain.Order) OrderDTO {
	s := o.State()
	d := OrderDTO{ID: o.ID().String(), Company: s.Company.String(), Customer: s.Customer.String(), Number: s.Number, Date: s.Date.String(),
		Warehouse: optID(s.Warehouse.UUID), PriceList: optID(s.PriceList.UUID), CustomerDiscount: money(s.CustomerDiscount), Reference: s.Reference,
		Notes: s.Notes, Status: s.Status.String(), Total: money(o.Total()), Pending: money(o.PendingAmount()), CloseReason: s.CloseReason,
		Lines: []LineDTO{}, Version: o.Version()}
	for _, l := range s.Lines {
		d.Lines = append(d.Lines, LineDTO{No: l.No, Product: l.Product.String(), SKU: l.SKU, Description: l.Description, UoM: l.UoM, TaxCode: l.TaxCode,
			Stocked: l.Stocked, Quantity: l.Quantity.String(), UnitPrice: l.UnitPrice.StringFixed(4), Discount: money(l.Discount),
			NetPrice: l.NetPrice.StringFixed(4), Amount: money(l.Amount), Reserved: l.Reserved.String(), Delivered: l.Delivered.String(),
			Pending: l.Pending().String(), Short: l.Short().String()})
	}
	return d
}

// DeliveryLineDTO es la forma de transporte de una línea de un albarán.
type DeliveryLineDTO struct {
	Line        int    `json:"line"`
	Product     string `json:"product"`
	SKU         string `json:"sku"`
	Description string `json:"description"`
	UoM         string `json:"uom"`
	Quantity    string `json:"quantity"`
	NetPrice    string `json:"netPrice"`
	Amount      string `json:"amount"`
}

// DeliveryDTO es la forma de transporte de un albarán.
type DeliveryDTO struct {
	ID          string            `json:"id"`
	Company     string            `json:"company"`
	Customer    string            `json:"customer"`
	Order       string            `json:"order"`
	OrderNumber string            `json:"orderNumber"`
	OrderStatus string            `json:"orderStatus,omitempty"` // del pedido tras la entrega, cuando se acaba de emitir
	Number      string            `json:"number"`
	Date        string            `json:"date"`
	Warehouse   string            `json:"warehouse,omitempty"`
	Total       string            `json:"total"`
	Invoice     string            `json:"invoice,omitempty"` // la factura de Billing que lo factura
	InvoiceNo   string            `json:"invoiceNumber,omitempty"`
	Lines       []DeliveryLineDTO `json:"lines"`
}

func deliveryDTO(d *domain.Delivery) DeliveryDTO {
	s := d.State()
	out := DeliveryDTO{ID: d.ID().String(), Company: s.Company.String(), Customer: s.Customer.String(), Order: s.Order.String(), OrderNumber: s.OrderNumber,
		Number: s.Number, Date: s.Date.String(), Warehouse: optID(s.Warehouse.UUID), Total: money(d.Total()), Invoice: s.Invoice, InvoiceNo: s.InvoiceNo,
		Lines: []DeliveryLineDTO{}}
	for _, l := range s.Lines {
		out.Lines = append(out.Lines, DeliveryLineDTO{Line: l.Line, Product: l.Product.String(), SKU: l.SKU, Description: l.Description, UoM: l.UoM,
			Quantity: l.Quantity.String(), NetPrice: l.NetPrice.StringFixed(4), Amount: money(l.Amount)})
	}
	return out
}

func (s service) termsUseCases(svc *Service) {
	svc.SetTerms = changing(s, PermTermsUpdate, func(ctx context.Context, c SetTerms) (TermsDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		customer := domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}
		st := domain.TermsState{Company: company, Customer: customer, Discount: parseDecimal(&v, "discount", c.Discount), BlockOrders: c.BlockOrders,
			BlockDelivery: c.BlockDelivery, BlockReason: c.BlockReason}
		if c.PriceList != "" {
			st.PriceList = domain.PriceListID{UUID: parseID(&v, "priceList", c.PriceList)}
		}
		if err := v.Err(); err != nil {
			return TermsDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return TermsDTO{}, err
		}
		ts, err := s.Terms.Find(ctx, spec.And(domain.TrmFieldCompany.Eq(company), domain.TrmFieldCustomer.Eq(customer)))
		if err != nil {
			return TermsDTO{}, err
		}
		if len(ts) > 0 {
			t, err := s.terms.Update(ctx, ts[0].ID(), func(_ context.Context, t *domain.Terms) error { return t.Change(st) })
			if err != nil {
				return TermsDTO{}, err
			}
			return termsDTO(t), nil
		}
		t, err := domain.ReconstituteTerms(domain.NewTermsID(), st)
		if err != nil {
			return TermsDTO{}, err
		}
		if err := s.terms.Create(ctx, t); err != nil {
			return TermsDTO{}, err
		}
		return termsDTO(t), nil
	})

	svc.SearchTerms = guard(PermTermsRead, func(ctx context.Context, q SearchTerms) ([]TermsDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}
		if err := v.Err(); err != nil {
			return nil, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, false); err != nil {
			return nil, err
		}
		merged, err := withoutMerged(ctx, s.Merged, q.IncludeMergedParties, domain.TrmFieldCustomer)
		if err != nil {
			return nil, err
		}
		ts, err := s.Terms.Find(ctx, spec.And(append(merged, domain.TrmFieldCompany.Eq(company))...))
		if err != nil {
			return nil, err
		}
		out := []TermsDTO{}
		for _, t := range ts {
			out = append(out, termsDTO(t))
		}
		return out, nil
	})
}

func (s service) orderUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.OrderID, fn func(context.Context, *domain.Order) error) (OrderDTO, error) {
		sc := scopeOf(ctx)
		o, err := s.orders.Update(ctx, id, func(ctx context.Context, o *domain.Order) error {
			if err := sc.check(domain.OrderKind, o.ID(), o.State().Company, true); err != nil {
				return err
			}
			return fn(ctx, o)
		})
		if err != nil {
			return OrderDTO{}, err
		}
		return orderDTO(o), nil
	}

	svc.DraftOrder = changing(s, PermOrderUpdate, func(ctx context.Context, c DraftOrder) (OrderDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		customer := domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}
		st := domain.OrderState{Company: company, Customer: customer, Date: c.Date, Reference: c.Reference, Notes: c.Notes}
		if c.Warehouse != "" {
			st.Warehouse = domain.WarehouseID{UUID: parseID(&v, "warehouse", c.Warehouse)}
		}
		if err := v.Err(); err != nil {
			return OrderDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return OrderDTO{}, err
		}
		if err := s.notBlocked(ctx, customer); err != nil {
			return OrderDTO{}, err
		}
		if st.Date.IsZero() {
			st.Date = vocab.DateOf(fw.Now())
		}
		terms, err := s.termsOf(ctx, company, customer)
		if err != nil {
			return OrderDTO{}, err
		}
		st.PriceList, st.CustomerDiscount = terms.PriceList, terms.Discount
		o, err := domain.DraftOrder(domain.NewOrderID(), st)
		if err != nil {
			return OrderDTO{}, err
		}
		if err := s.orders.Create(ctx, o); err != nil {
			return OrderDTO{}, err
		}
		return orderDTO(o), nil
	})

	svc.AddLine = changing(s, PermOrderUpdate, func(ctx context.Context, c AddLine) (OrderDTO, error) {
		var v fw.Validation
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		q := parseDecimal(&v, "quantity", c.Quantity)
		v.Require(q.IsPositive(), "quantity", "range", "a positive quantity")
		if err := v.Err(); err != nil {
			return OrderDTO{}, err
		}
		return update(ctx, c.ID, func(ctx context.Context, o *domain.Order) error {
			os := o.State()
			it, err := s.item(ctx, os.Company, os.PriceList, product, q, os.Date)
			if err != nil {
				return err
			}
			_, err = o.AddLine(domain.Line{Product: product, SKU: it.SKU, Description: it.Name, UoM: it.UoM, TaxCode: it.TaxCode, Stocked: it.Stocked,
				Quantity: q, UnitPrice: it.UnitPrice, Discount: it.Discount})
			return err
		})
	})

	svc.RemoveLine = changing(s, PermOrderUpdate, func(ctx context.Context, c RemoveLine) (OrderDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, o *domain.Order) error { return o.RemoveLine(c.Line) })
	})

	svc.Confirm = changing(s, PermOrderConfirm, func(ctx context.Context, c ConfirmOrder) (OrderDTO, error) {
		return update(ctx, c.ID, func(ctx context.Context, o *domain.Order) error {
			os := o.State()
			terms, err := s.termsOf(ctx, os.Company, os.Customer)
			if err != nil {
				return err
			}
			if terms.BlockOrders {
				return fw.Violation("orders.customer_blocked", "the customer is blocked for orders")
			}
			if err := s.notBlocked(ctx, os.Customer); err != nil {
				return err
			}
			// El crédito de Receivables cuenta todo lo que debe el cliente, no sólo este pedido
			// (el C# comparaba el límite con el importe del pedido que se confirmaba).
			if s.Credit != nil {
				cr, err := s.Credit.Credit(ctx, os.Company, os.Customer, os.Date)
				if err != nil {
					return err
				}
				if cr.Blocked {
					return fw.Violation("orders.customer_blocked", "the customer is blocked for credit")
				}
				if cr.Limited && o.Total().GreaterThan(cr.Available) {
					return fw.Violation("orders.credit_exceeded", "the order ("+money(o.Total())+") exceeds the available credit ("+money(cr.Available)+")")
				}
			}
			number, err := s.number(ctx, os.Company, "PED", os.Date.Year())
			if err != nil {
				return err
			}
			return o.Confirm(number)
		})
	})

	svc.RequestStock = changing(s, PermOrderConfirm, func(ctx context.Context, c RequestStock) (OrderDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, o *domain.Order) error { return o.RequestStock() })
	})

	svc.Deliver = changing(s, PermOrderDeliver, func(ctx context.Context, c Deliver) (DeliveryDTO, error) {
		var v fw.Validation
		var picks []domain.Pick
		for _, l := range c.Lines {
			picks = append(picks, domain.Pick{Line: l.Line, Quantity: parseDecimal(&v, "quantity", l.Quantity)})
		}
		if err := v.Err(); err != nil {
			return DeliveryDTO{}, err
		}
		date := c.Date
		if date.IsZero() {
			date = vocab.DateOf(fw.Now())
		}
		sc := scopeOf(ctx)
		var note *domain.Delivery
		o, err := s.orders.Update(ctx, c.ID, func(ctx context.Context, o *domain.Order) error {
			os := o.State()
			if err := sc.check(domain.OrderKind, o.ID(), os.Company, true); err != nil {
				return err
			}
			terms, err := s.termsOf(ctx, os.Company, os.Customer)
			if err != nil {
				return err
			}
			if terms.BlockDelivery {
				return fw.Violation("orders.delivery_blocked", "the customer is blocked for deliveries")
			}
			if date.Before(os.Date) {
				return fw.Violation("orders.delivery_date", "a delivery is not before its order")
			}
			picks := picks // un reintento vuelve a empezar desde lo que se pidió
			if len(picks) == 0 {
				for _, l := range os.Lines {
					q := l.Pending()
					if l.Stocked {
						q = l.Reserved
					}
					if q.IsPositive() {
						picks = append(picks, domain.Pick{Line: l.No, Quantity: q})
					}
				}
			}
			lines, err := o.Deliver(picks)
			if err != nil {
				return err
			}
			number, err := s.number(ctx, os.Company, "ALB", date.Year())
			if err != nil {
				return err
			}
			if note, err = domain.IssueDelivery(domain.NewDeliveryID(), o, number, date, lines); err != nil {
				return err
			}
			return s.deliveries.Create(ctx, note)
		})
		if err != nil {
			return DeliveryDTO{}, err
		}
		out := deliveryDTO(note)
		out.OrderStatus = o.State().Status.String()
		return out, nil
	})

	svc.Cancel = changing(s, PermOrderCancel, func(ctx context.Context, c EndOrder) (OrderDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, o *domain.Order) error { return o.Cancel(c.Reason) })
	})
	svc.Close = changing(s, PermOrderCancel, func(ctx context.Context, c EndOrder) (OrderDTO, error) {
		return update(ctx, c.ID, func(_ context.Context, o *domain.Order) error { return o.Close(c.Reason) })
	})

	svc.GetOrder = guard(PermOrderRead, func(ctx context.Context, q GetOrder) (OrderDTO, error) {
		o, err := s.Orders.Get(ctx, q.ID)
		if err != nil {
			return OrderDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.OrderKind, o.ID(), o.State().Company, false); err != nil {
			return OrderDTO{}, err
		}
		return orderDTO(o), nil
	})

	svc.SearchOrders = guard(PermOrderRead, func(ctx context.Context, q SearchOrders) (fw.Page[OrderDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Order]{within(scopeOf(ctx), domain.OrdFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.OrdFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Customer != "" {
			parts = append(parts, domain.OrdFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", q.Customer)}))
		}
		if q.Status != "" {
			st, ok := domain.ParseStatus(q.Status)
			v.Require(ok, "status", "enum", "draft, confirmed, delivered, closed or cancelled")
			parts = append(parts, domain.OrdFieldStatus.Eq(int(st)))
		}
		if err := v.Err(); err != nil {
			return fw.Page[OrderDTO]{}, err
		}
		merged, err := withoutMerged(ctx, s.Merged, q.IncludeMergedParties || q.Customer != "", domain.OrdFieldCustomer)
		if err != nil {
			return fw.Page[OrderDTO]{}, err
		}
		parts = append(parts, merged...)
		page, err := s.Orders.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.OrdFieldDate.Asc(), domain.OrdFieldNumber.Asc()))
		if err != nil {
			return fw.Page[OrderDTO]{}, err
		}
		return fw.MapPage(page, orderDTO), nil
	})

	svc.GetDelivery = guard(PermOrderRead, func(ctx context.Context, q GetDelivery) (DeliveryDTO, error) {
		d, err := s.Deliveries.Get(ctx, q.ID)
		if err != nil {
			return DeliveryDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.DeliveryKind, d.ID(), d.State().Company, false); err != nil {
			return DeliveryDTO{}, err
		}
		return deliveryDTO(d), nil
	})

	svc.SearchDeliveries = guard(PermOrderRead, func(ctx context.Context, q SearchDeliveries) (fw.Page[DeliveryDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Delivery]{within(scopeOf(ctx), domain.DelFieldCompany)}
		if q.Company != "" {
			parts = append(parts, domain.DelFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", q.Company)}))
		}
		if q.Customer != "" {
			parts = append(parts, domain.DelFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", q.Customer)}))
		}
		if q.Order != "" {
			parts = append(parts, domain.DelFieldOrder.Eq(domain.OrderID{UUID: parseID(&v, "order", q.Order)}))
		}
		if q.Uninvoiced {
			parts = append(parts, domain.DelFieldInvoiced.Eq(false))
		}
		if err := v.Err(); err != nil {
			return fw.Page[DeliveryDTO]{}, err
		}
		merged, err := withoutMerged(ctx, s.Merged, q.IncludeMergedParties || q.Customer != "", domain.DelFieldCustomer)
		if err != nil {
			return fw.Page[DeliveryDTO]{}, err
		}
		parts = append(parts, merged...)
		page, err := s.Deliveries.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(q.Page, q.Size, domain.DelFieldNumber.Asc()))
		if err != nil {
			return fw.Page[DeliveryDTO]{}, err
		}
		return fw.MapPage(page, deliveryDTO), nil
	})
}

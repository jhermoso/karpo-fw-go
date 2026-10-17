package application

import (
	"context"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application/authz"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Permisos de los presupuestos (los endpoints de presupuesto de C# sólo pedían autenticación).
// Preparar una oferta, comprometer a la empresa con ella y registrar lo que contestó el cliente van
// por separado.
var (
	PermQuoteRead    = authz.MustPermission("Orders.Quote.Read")
	PermQuoteUpdate  = authz.MustPermission("Orders.Quote.Update")
	PermQuoteSend    = authz.MustPermission("Orders.Quote.Send")
	PermQuoteResolve = authz.MustPermission("Orders.Quote.Resolve")
)

// DraftQuote abre un presupuesto en borrador para un cliente, con la tarifa y el descuento de sus
// condiciones. Sin validez, vale treinta días desde su fecha.
type DraftQuote struct {
	Company    string     `json:"company"`
	Customer   string     `json:"customer"`
	Date       vocab.Date `json:"date,omitzero"`
	ValidUntil vocab.Date `json:"validUntil,omitzero"`
	Reference  string     `json:"reference,omitempty"`
	Notes      string     `json:"notes,omitempty"`
}

// ChangeQuote sustituye las fechas, la referencia y las notas de un borrador: lo que no se envía
// se deja vacío (la validez, otra vez treinta días).
type ChangeQuote struct {
	ID         domain.QuoteID `json:"-"`
	Date       vocab.Date     `json:"date,omitzero"`
	ValidUntil vocab.Date     `json:"validUntil,omitzero"`
	Reference  string         `json:"reference,omitempty"`
	Notes      string         `json:"notes,omitempty"`
}

// AddQuoteLine añade un producto a un presupuesto en borrador, con el precio de Products.
type AddQuoteLine struct {
	ID       domain.QuoteID `json:"-"`
	Product  string         `json:"product"`
	Quantity string         `json:"quantity"`
}

// RemoveQuoteLine quita una línea de un presupuesto en borrador.
type RemoveQuoteLine struct {
	ID   domain.QuoteID `json:"-"`
	Line int            `json:"line"`
}

// SendQuote envía un presupuesto en borrador: se numera y ya no cambia.
type SendQuote struct {
	ID domain.QuoteID `json:"-"`
}

// AcceptQuote registra que el cliente tomó la oferta: se convierte en un pedido en borrador a los
// precios presupuestados, servido desde el almacén indicado (se puede poner al confirmar el pedido).
type AcceptQuote struct {
	ID        domain.QuoteID `json:"-"`
	Warehouse string         `json:"warehouse,omitempty"`
}

// EndQuote rechaza o retira un presupuesto, con un motivo.
type EndQuote struct {
	ID     domain.QuoteID `json:"-"`
	Reason string         `json:"reason,omitempty"`
}

// ExpireQuotes cierra los presupuestos enviados de una empresa cuya validez ha terminado (para una
// tarea programada).
type ExpireQuotes struct {
	Company string `json:"company"`
}

// GetQuote carga un presupuesto.
type GetQuote struct{ ID domain.QuoteID }

// SearchQuotes busca presupuestos del ámbito de quien llama, los más recientes primero.
type SearchQuotes struct {
	Company, Customer, Status string
	Page, Size                int
}

// QuoteLineDTO es la forma de transporte de una línea de presupuesto.
type QuoteLineDTO struct {
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
}

// QuoteDTO es la forma de transporte de un presupuesto.
type QuoteDTO struct {
	ID               string         `json:"id"`
	Company          string         `json:"company"`
	Customer         string         `json:"customer"`
	Number           string         `json:"number,omitempty"`
	Date             string         `json:"date"`
	ValidUntil       string         `json:"validUntil"`
	PriceList        string         `json:"priceList,omitempty"`
	CustomerDiscount string         `json:"customerDiscount"`
	Reference        string         `json:"reference,omitempty"`
	Notes            string         `json:"notes,omitempty"`
	Status           string         `json:"status"`
	Total            string         `json:"total"`
	Reason           string         `json:"reason,omitempty"`
	Order            string         `json:"order,omitempty"` // el pedido en que se convirtió
	Lines            []QuoteLineDTO `json:"lines"`
	Version          int64          `json:"version"`
}

// AcceptedQuoteDTO es un presupuesto aceptado con el pedido en que se convirtió.
type AcceptedQuoteDTO struct {
	Quote QuoteDTO `json:"quote"`
	Order OrderDTO `json:"order"`
}

// ExpiredQuotesDTO enumera los presupuestos que cerró una pasada.
type ExpiredQuotesDTO struct {
	Numbers []string `json:"numbers"`
}

func quoteDTO(q *domain.Quote) QuoteDTO {
	s := q.State()
	d := QuoteDTO{ID: q.ID().String(), Company: s.Company.String(), Customer: s.Customer.String(), Number: s.Number, Date: s.Date.String(),
		ValidUntil: s.ValidUntil.String(), PriceList: optID(s.PriceList.UUID), CustomerDiscount: money(s.CustomerDiscount), Reference: s.Reference,
		Notes: s.Notes, Status: string(s.Status), Total: money(q.Total()), Reason: s.Reason, Order: optID(s.Order.UUID), Lines: []QuoteLineDTO{},
		Version: q.Version()}
	for _, l := range s.Lines {
		d.Lines = append(d.Lines, QuoteLineDTO{No: l.No, Product: l.Product.String(), SKU: l.SKU, Description: l.Description, UoM: l.UoM, TaxCode: l.TaxCode,
			Stocked: l.Stocked, Quantity: l.Quantity.String(), UnitPrice: l.UnitPrice.StringFixed(4), Discount: money(l.Discount),
			NetPrice: l.NetPrice.StringFixed(4), Amount: money(l.Amount)})
	}
	return d
}

// item pone precio a un producto de la empresa para una venta en una fecha, y rechaza lo que no se
// puede vender.
func (s service) item(ctx context.Context, company domain.OrganizationID, list domain.PriceListID, product domain.ProductID, q vocab.Decimal,
	on vocab.Date) (domain.Item, error) {
	it, ok, err := s.Catalog.Price(ctx, company, product, list, q, on)
	if err != nil {
		return domain.Item{}, err
	}
	if !ok || it.Company != company {
		return domain.Item{}, fw.Violation("orders.unknown_product", "the product is not in the catalog of the company")
	}
	if !it.Sellable {
		return domain.Item{}, fw.Violation("orders.not_for_sale", "the product is not for sale or is blocked")
	}
	if !it.Retired.IsZero() && !on.Before(it.Retired) {
		return domain.Item{}, fw.Violation("orders.discontinued", "the product is discontinued")
	}
	if it.TaxCode == "" {
		return domain.Item{}, fw.Violation("orders.no_tax_code", "the product has no tax code: it could not be invoiced")
	}
	return it, nil
}

func (s service) quoteUseCases(svc *Service) {
	update := func(ctx context.Context, id domain.QuoteID, fn func(context.Context, *domain.Quote) error) (*domain.Quote, error) {
		sc := scopeOf(ctx)
		return s.quotes.Update(ctx, id, func(ctx context.Context, q *domain.Quote) error {
			if err := sc.check(domain.QuoteKind, q.ID(), q.State().Company, true); err != nil {
				return err
			}
			return fn(ctx, q)
		})
	}
	dto := func(q *domain.Quote, err error) (QuoteDTO, error) {
		if err != nil {
			return QuoteDTO{}, err
		}
		return quoteDTO(q), nil
	}

	svc.DraftQuote = changing(s, PermQuoteUpdate, func(ctx context.Context, c DraftQuote) (QuoteDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		customer := domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}
		if err := v.Err(); err != nil {
			return QuoteDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return QuoteDTO{}, err
		}
		if err := s.notBlocked(ctx, customer); err != nil {
			return QuoteDTO{}, err
		}
		st := domain.QuoteState{Company: company, Customer: customer, Date: c.Date, ValidUntil: c.ValidUntil, Reference: c.Reference, Notes: c.Notes}
		if st.Date.IsZero() {
			st.Date = vocab.DateOf(fw.Now())
		}
		terms, err := s.termsOf(ctx, company, customer)
		if err != nil {
			return QuoteDTO{}, err
		}
		st.PriceList, st.CustomerDiscount = terms.PriceList, terms.Discount
		q, err := domain.DraftQuote(domain.NewQuoteID(), st)
		if err != nil {
			return QuoteDTO{}, err
		}
		if err := s.quotes.Create(ctx, q); err != nil {
			return QuoteDTO{}, err
		}
		return quoteDTO(q), nil
	})

	svc.ChangeQuote = changing(s, PermQuoteUpdate, func(ctx context.Context, c ChangeQuote) (QuoteDTO, error) {
		return dto(update(ctx, c.ID, func(_ context.Context, q *domain.Quote) error {
			date, until := c.Date, c.ValidUntil
			if date.IsZero() {
				date = q.State().Date
			}
			if until.IsZero() {
				until = date.AddDays(domain.DefaultValidityDays)
			}
			return q.Change(date, until, c.Reference, c.Notes)
		}))
	})

	svc.AddQuoteLine = changing(s, PermQuoteUpdate, func(ctx context.Context, c AddQuoteLine) (QuoteDTO, error) {
		var v fw.Validation
		product := domain.ProductID{UUID: parseID(&v, "product", c.Product)}
		n := parseDecimal(&v, "quantity", c.Quantity)
		v.Require(n.IsPositive(), "quantity", "range", "a positive quantity")
		if err := v.Err(); err != nil {
			return QuoteDTO{}, err
		}
		return dto(update(ctx, c.ID, func(ctx context.Context, q *domain.Quote) error {
			qs := q.State()
			it, err := s.item(ctx, qs.Company, qs.PriceList, product, n, qs.Date)
			if err != nil {
				return err
			}
			_, err = q.AddLine(domain.QuoteLine{Product: product, SKU: it.SKU, Description: it.Name, UoM: it.UoM, TaxCode: it.TaxCode, Stocked: it.Stocked,
				Quantity: n, UnitPrice: it.UnitPrice, Discount: it.Discount})
			return err
		}))
	})

	svc.RemoveQuoteLine = changing(s, PermQuoteUpdate, func(ctx context.Context, c RemoveQuoteLine) (QuoteDTO, error) {
		return dto(update(ctx, c.ID, func(_ context.Context, q *domain.Quote) error { return q.RemoveLine(c.Line) }))
	})

	svc.SendQuote = changing(s, PermQuoteSend, func(ctx context.Context, c SendQuote) (QuoteDTO, error) {
		return dto(update(ctx, c.ID, func(ctx context.Context, q *domain.Quote) error {
			qs := q.State()
			if qs.Status != domain.QuoteDraft { // antes de tomar un número para nada
				return fw.Violation("orders.quote_not_draft", "only a draft quote is sent")
			}
			number, err := s.number(ctx, qs.Company, "PRE", qs.Date.Year())
			if err != nil {
				return err
			}
			return q.Send(number, vocab.DateOf(fw.Now()))
		}))
	})

	svc.AcceptQuote = changing(s, PermQuoteResolve, func(ctx context.Context, c AcceptQuote) (AcceptedQuoteDTO, error) {
		var v fw.Validation
		var warehouse domain.WarehouseID
		if c.Warehouse != "" {
			warehouse = domain.WarehouseID{UUID: parseID(&v, "warehouse", c.Warehouse)}
		}
		if err := v.Err(); err != nil {
			return AcceptedQuoteDTO{}, err
		}
		today, id := vocab.DateOf(fw.Now()), domain.NewOrderID()
		q, err := update(ctx, c.ID, func(_ context.Context, q *domain.Quote) error { return q.Accept(id, today) })
		if err != nil {
			return AcceptedQuoteDTO{}, err
		}
		// El pedido toma los precios del presupuesto tal cual: es lo que se ofreció.
		qs := q.State()
		o, err := domain.DraftOrder(id, domain.OrderState{Company: qs.Company, Customer: qs.Customer, Date: today, Warehouse: warehouse, PriceList: qs.PriceList,
			CustomerDiscount: qs.CustomerDiscount, Reference: qs.Reference, Notes: qs.Notes})
		if err != nil {
			return AcceptedQuoteDTO{}, err
		}
		for _, l := range qs.Lines {
			if _, err := o.AddLine(domain.Line{Product: l.Product, SKU: l.SKU, Description: l.Description, UoM: l.UoM, TaxCode: l.TaxCode, Stocked: l.Stocked,
				Quantity: l.Quantity, UnitPrice: l.UnitPrice, Discount: l.Discount}); err != nil {
				return AcceptedQuoteDTO{}, err
			}
		}
		if err := s.orders.Create(ctx, o); err != nil {
			return AcceptedQuoteDTO{}, err
		}
		return AcceptedQuoteDTO{Quote: quoteDTO(q), Order: orderDTO(o)}, nil
	})

	svc.RejectQuote = changing(s, PermQuoteResolve, func(ctx context.Context, c EndQuote) (QuoteDTO, error) {
		return dto(update(ctx, c.ID, func(_ context.Context, q *domain.Quote) error { return q.Reject(c.Reason) }))
	})

	svc.WithdrawQuote = changing(s, PermQuoteUpdate, func(ctx context.Context, c EndQuote) (QuoteDTO, error) {
		return dto(update(ctx, c.ID, func(_ context.Context, q *domain.Quote) error { return q.Withdraw(c.Reason) }))
	})

	svc.ExpireQuotes = changing(s, PermQuoteResolve, func(ctx context.Context, c ExpireQuotes) (ExpiredQuotesDTO, error) {
		var v fw.Validation
		company := domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}
		if err := v.Err(); err != nil {
			return ExpiredQuotesDTO{}, err
		}
		if err := scopeOf(ctx).check("parties.party", company, company, true); err != nil {
			return ExpiredQuotesDTO{}, err
		}
		sent, err := s.Quotes.Find(ctx, spec.And(domain.QuoFieldCompany.Eq(company), domain.QuoFieldStatus.Eq(string(domain.QuoteSent))))
		if err != nil {
			return ExpiredQuotesDTO{}, err
		}
		out, today := ExpiredQuotesDTO{Numbers: []string{}}, vocab.DateOf(fw.Now())
		for _, q := range sent {
			if !q.Due(today) {
				continue
			}
			expired := false
			if _, err := s.quotes.Update(ctx, q.ID(), func(_ context.Context, x *domain.Quote) error { expired = x.Expire(today); return nil }); err != nil {
				return ExpiredQuotesDTO{}, err
			}
			if expired {
				out.Numbers = append(out.Numbers, q.State().Number)
			}
		}
		slices.Sort(out.Numbers)
		return out, nil
	})

	svc.GetQuote = guard(PermQuoteRead, func(ctx context.Context, c GetQuote) (QuoteDTO, error) {
		q, err := s.Quotes.Get(ctx, c.ID)
		if err != nil {
			return QuoteDTO{}, err
		}
		if err := scopeOf(ctx).check(domain.QuoteKind, q.ID(), q.State().Company, false); err != nil {
			return QuoteDTO{}, err
		}
		return quoteDTO(q), nil
	})

	svc.SearchQuotes = guard(PermQuoteRead, func(ctx context.Context, c SearchQuotes) (fw.Page[QuoteDTO], error) {
		var v fw.Validation
		parts := []spec.Specification[*domain.Quote]{within(scopeOf(ctx), domain.QuoFieldCompany)}
		if c.Company != "" {
			parts = append(parts, domain.QuoFieldCompany.Eq(domain.OrganizationID{UUID: parseID(&v, "company", c.Company)}))
		}
		if c.Customer != "" {
			parts = append(parts, domain.QuoFieldCustomer.Eq(domain.PartyID{UUID: parseID(&v, "customer", c.Customer)}))
		}
		if c.Status != "" {
			v.Require(slices.Contains(domain.QuoteStatuses, domain.QuoteStatus(c.Status)), "status", "enum", "a status")
			parts = append(parts, domain.QuoFieldStatus.Eq(c.Status))
		}
		if err := v.Err(); err != nil {
			return fw.Page[QuoteDTO]{}, err
		}
		page, err := s.Quotes.FindPage(ctx, spec.And(parts...), fw.NewPageRequest(c.Page, c.Size, domain.QuoFieldDate.Desc(), domain.QuoFieldNumber.Desc()))
		if err != nil {
			return fw.Page[QuoteDTO]{}, err
		}
		return fw.MapPage(page, quoteDTO), nil
	})
}

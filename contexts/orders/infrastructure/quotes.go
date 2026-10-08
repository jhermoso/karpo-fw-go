package infrastructure

import (
	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

var quotesDDL = []string{
	`CREATE TABLE ord_quotes (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, customer {uuid} NOT NULL,
	quote_number {str:20}, quote_date {date} NOT NULL, valid_until {date} NOT NULL, price_list {uuid}, customer_discount {str:10} NOT NULL,
	reference {str:40}, notes {str:500}, status {str:20} NOT NULL, reason {str:200}, order_id {uuid}, ` + audit + `)`,
	`CREATE INDEX ix_ord_quotes_customer ON ord_quotes (company, customer, status)`,
	`CREATE INDEX ix_ord_quotes_number ON ord_quotes (company, quote_number)`,
	`CREATE TABLE ord_quote_lines (quote_id {uuid} NOT NULL, line_no {int} NOT NULL, product {uuid} NOT NULL, sku {str:30} NOT NULL,
	description {str:120} NOT NULL, uom {str:10} NOT NULL, tax_code {str:10}, stocked {bool} NOT NULL, quantity {str:30} NOT NULL,
	unit_price {str:30} NOT NULL, discount {str:10} NOT NULL, net_price {str:30} NOT NULL, amount {str:30} NOT NULL,
	PRIMARY KEY (quote_id, line_no), FOREIGN KEY (quote_id) REFERENCES ord_quotes (id))`,
}

// QuoteMapping maps Quote to ord_quotes and its lines.
func QuoteMapping() sqlrepo.Mapping[domain.QuoteID, *domain.Quote] {
	return sqlrepo.Mapping[domain.QuoteID, *domain.Quote]{
		Table: "ord_quotes",
		Columns: sqlrepo.WithAuditColumns("company", "customer", "quote_number", "quote_date", "valid_until", "price_list", "customer_discount", "reference",
			"notes", "status", "reason", "order_id"),
		Dehydrate: func(q *domain.Quote) (sqlrepo.Values, error) {
			s := q.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "customer": s.Customer, "quote_number": opt(s.Number), "quote_date": s.Date,
				"valid_until": s.ValidUntil, "price_list": optUUID(s.PriceList.UUID), "customer_discount": s.CustomerDiscount.StringFixed(2),
				"reference": opt(s.Reference), "notes": opt(s.Notes), "status": string(s.Status), "reason": opt(s.Reason), "order_id": optUUID(s.Order.UUID)},
				q.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Quote, error) {
			s := domain.QuoteState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Number: r.String("quote_number"), Date: r.Date("quote_date"), ValidUntil: r.Date("valid_until"),
				PriceList: domain.PriceListID{UUID: r.UUID("price_list")}, CustomerDiscount: r.Decimal("customer_discount"), Reference: r.String("reference"),
				Notes: r.String("notes"), Status: domain.QuoteStatus(r.String("status")), Reason: r.String("reason"),
				Order: domain.OrderID{UUID: r.UUID("order_id")}, Audit: r.AuditStamp()}
			for _, c := range byNo(children.Of("lines")) {
				s.Lines = append(s.Lines, domain.QuoteLine{No: int(c.Int64("line_no")), Product: domain.ProductID{UUID: c.UUID("product")}, SKU: c.String("sku"),
					Description: c.String("description"), UoM: c.String("uom"), TaxCode: c.String("tax_code"), Stocked: c.Bool("stocked"),
					Quantity: c.Decimal("quantity"), UnitPrice: c.Decimal("unit_price"), Discount: c.Decimal("discount"), NetPrice: c.Decimal("net_price"),
					Amount: c.Decimal("amount")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteQuote(domain.QuoteID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Quote]{{
			Name: "lines", Table: "ord_quote_lines", ForeignKey: "quote_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "product", "sku", "description", "uom", "tax_code", "stocked", "quantity", "unit_price", "discount", "net_price",
				"amount"},
			Dehydrate: func(q *domain.Quote) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range q.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(l.No), "product": l.Product, "sku": l.SKU, "description": l.Description, "uom": l.UoM,
						"tax_code": opt(l.TaxCode), "stocked": l.Stocked, "quantity": l.Quantity.String(), "unit_price": l.UnitPrice.StringFixed(4),
						"discount": l.Discount.StringFixed(2), "net_price": l.NetPrice.StringFixed(4), "amount": l.Amount.StringFixed(2)})
				}
				return out, nil
			},
		}},
	}
}

// QuoteRepositoryFactory builds the quote repository.
func QuoteRepositoryFactory(b hotswap.Backend) (domain.QuoteRepository, error) {
	return repository(b, QuoteMapping())
}

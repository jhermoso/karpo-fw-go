// Package infrastructure stores the Orders context: SQL mappings, versioned schema of the five
// engines (with the inbox of the Inventory events), hot-swap factories and the adapters to
// Products and Receivables.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	products "github.com/jhermoso/karpo-fw-go/contexts/products/contracts"
	receivables "github.com/jhermoso/karpo-fw-go/contexts/receivables/contracts"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// Context is the name of the bounded context.
const Context = "orders"

// Technical tables of the context.
const (
	TableOutbox            = "orders_outbox"
	TableIntegrationOutbox = "orders_integration_outbox"
	TableAuditLog          = "orders_audit_log"
	TableInbox             = "orders_inbox"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE ord_terms (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, customer {uuid} NOT NULL,
	price_list {uuid}, discount {str:10} NOT NULL, block_orders {bool} NOT NULL, block_delivery {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_ord_terms ON ord_terms (company, customer)`,
	`CREATE TABLE ord_orders (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, customer {uuid} NOT NULL,
	order_number {str:20}, order_date {date} NOT NULL, warehouse {uuid}, price_list {uuid}, customer_discount {str:10} NOT NULL,
	reference {str:40}, notes {str:500}, status {int} NOT NULL, close_reason {str:200}, ` + audit + `)`,
	`CREATE INDEX ix_ord_orders_customer ON ord_orders (company, customer, status)`,
	`CREATE INDEX ix_ord_orders_number ON ord_orders (company, order_number)`,
	`CREATE TABLE ord_order_lines (order_id {uuid} NOT NULL, line_no {int} NOT NULL, product {uuid} NOT NULL, sku {str:30} NOT NULL,
	description {str:120} NOT NULL, uom {str:10} NOT NULL, tax_code {str:10}, stocked {bool} NOT NULL, quantity {str:30} NOT NULL,
	unit_price {str:30} NOT NULL, discount {str:10} NOT NULL, net_price {str:30} NOT NULL, amount {str:30} NOT NULL, reserved {str:30} NOT NULL,
	delivered {str:30} NOT NULL, PRIMARY KEY (order_id, line_no), FOREIGN KEY (order_id) REFERENCES ord_orders (id))`,
	`CREATE TABLE ord_deliveries (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, customer {uuid} NOT NULL,
	order_id {uuid} NOT NULL, order_number {str:20} NOT NULL, delivery_number {str:20} NOT NULL, delivered_on {date} NOT NULL, warehouse {uuid}, ` + audit + `,
	FOREIGN KEY (order_id) REFERENCES ord_orders (id))`,
	`CREATE UNIQUE INDEX ux_ord_deliveries_number ON ord_deliveries (company, delivery_number)`,
	`CREATE INDEX ix_ord_deliveries_order ON ord_deliveries (order_id)`,
	`CREATE TABLE ord_delivery_lines (delivery_id {uuid} NOT NULL, line_no {int} NOT NULL, product {uuid} NOT NULL, sku {str:30} NOT NULL,
	description {str:120} NOT NULL, uom {str:10} NOT NULL, tax_code {str:10}, stocked {bool} NOT NULL, quantity {str:30} NOT NULL,
	net_price {str:30} NOT NULL, amount {str:30} NOT NULL, PRIMARY KEY (delivery_id, line_no), FOREIGN KEY (delivery_id) REFERENCES ord_deliveries (id))`,
	`CREATE TABLE ord_counters (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, series {str:5} NOT NULL,
	fiscal_year {int} NOT NULL, last_number {bigint} NOT NULL)`,
	`CREATE UNIQUE INDEX ux_ord_counters ON ord_counters (company, series, fiscal_year)`,
}

func technicalDDL(d string) []string {
	switch d {
	case "sqlite":
		return slices.Concat(sqlite.OutboxDDL(TableOutbox), sqlite.OutboxDDL(TableIntegrationOutbox), sqlite.AuditDDL(TableAuditLog), sqlite.InboxDDL(TableInbox))
	case "postgres":
		return slices.Concat(postgres.OutboxDDL(TableOutbox), postgres.OutboxDDL(TableIntegrationOutbox), postgres.AuditDDL(TableAuditLog), postgres.InboxDDL(TableInbox))
	case "sqlserver":
		return slices.Concat(sqlserver.OutboxDDL(TableOutbox), sqlserver.OutboxDDL(TableIntegrationOutbox), sqlserver.AuditDDL(TableAuditLog), sqlserver.InboxDDL(TableInbox))
	case "oracle":
		return slices.Concat(oracle.OutboxDDL(TableOutbox), oracle.OutboxDDL(TableIntegrationOutbox), oracle.AuditDDL(TableAuditLog), oracle.InboxDDL(TableInbox))
	case "mysql":
		return slices.Concat(mysql.OutboxDDL(TableOutbox), mysql.OutboxDDL(TableIntegrationOutbox), mysql.AuditDDL(TableAuditLog), mysql.InboxDDL(TableInbox))
	}
	return nil
}

// Migrations is the versioned schema of the context.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "customer terms, sales orders and delivery notes", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes, audit log and inbox", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"ord_delivery_lines", "ord_deliveries", "ord_order_lines", "ord_orders", "ord_terms", "ord_counters", TableOutbox,
	TableIntegrationOutbox, TableAuditLog, TableInbox}

// DropAll removes the tables of the context and its migration history (tests only).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range Tables {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
	_, _ = db.ExecContext(ctx, "DELETE FROM "+sqlrepo.DefaultMigrationsTable+" WHERE context = '"+Context+"'")
}

func opt(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func optUUID(u fw.UUID) any {
	if u.IsZero() {
		return nil
	}
	return u
}

func byNo(rows []*sqlrepo.Row) []*sqlrepo.Row {
	out := slices.Clone(rows)
	slices.SortFunc(out, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
	return out
}

// TermsMapping maps Terms to ord_terms.
func TermsMapping() sqlrepo.Mapping[domain.TermsID, *domain.Terms] {
	return sqlrepo.Mapping[domain.TermsID, *domain.Terms]{
		Table:   "ord_terms",
		Columns: sqlrepo.WithAuditColumns("company", "customer", "price_list", "discount", "block_orders", "block_delivery"),
		Dehydrate: func(t *domain.Terms) (sqlrepo.Values, error) {
			s := t.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "customer": s.Customer, "price_list": optUUID(s.PriceList.UUID),
				"discount": s.Discount.StringFixed(2), "block_orders": s.BlockOrders, "block_delivery": s.BlockDelivery}, t.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Terms, error) {
			s := domain.TermsState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				PriceList: domain.PriceListID{UUID: r.UUID("price_list")}, Discount: r.Decimal("discount"), BlockOrders: r.Bool("block_orders"),
				BlockDelivery: r.Bool("block_delivery"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteTerms(domain.TermsID{UUID: r.UUID("id")}, s)
		},
	}
}

// OrderMapping maps Order to ord_orders and its lines.
func OrderMapping() sqlrepo.Mapping[domain.OrderID, *domain.Order] {
	return sqlrepo.Mapping[domain.OrderID, *domain.Order]{
		Table: "ord_orders",
		Columns: sqlrepo.WithAuditColumns("company", "customer", "order_number", "order_date", "warehouse", "price_list", "customer_discount", "reference",
			"notes", "status", "close_reason"),
		Dehydrate: func(o *domain.Order) (sqlrepo.Values, error) {
			s := o.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "customer": s.Customer, "order_number": opt(s.Number), "order_date": s.Date,
				"warehouse": optUUID(s.Warehouse.UUID), "price_list": optUUID(s.PriceList.UUID), "customer_discount": s.CustomerDiscount.StringFixed(2),
				"reference": opt(s.Reference), "notes": opt(s.Notes), "status": int64(s.Status), "close_reason": opt(s.CloseReason)}, o.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Order, error) {
			s := domain.OrderState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Number: r.String("order_number"), Date: r.Date("order_date"), Warehouse: domain.WarehouseID{UUID: r.UUID("warehouse")},
				PriceList: domain.PriceListID{UUID: r.UUID("price_list")}, CustomerDiscount: r.Decimal("customer_discount"), Reference: r.String("reference"),
				Notes: r.String("notes"), Status: domain.Status(r.Int64("status")), CloseReason: r.String("close_reason"), Audit: r.AuditStamp()}
			for _, c := range byNo(children.Of("lines")) {
				s.Lines = append(s.Lines, domain.Line{No: int(c.Int64("line_no")), Product: domain.ProductID{UUID: c.UUID("product")}, SKU: c.String("sku"),
					Description: c.String("description"), UoM: c.String("uom"), TaxCode: c.String("tax_code"), Stocked: c.Bool("stocked"),
					Quantity: c.Decimal("quantity"), UnitPrice: c.Decimal("unit_price"), Discount: c.Decimal("discount"), NetPrice: c.Decimal("net_price"),
					Amount: c.Decimal("amount"), Reserved: c.Decimal("reserved"), Delivered: c.Decimal("delivered")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteOrder(domain.OrderID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Order]{{
			Name: "lines", Table: "ord_order_lines", ForeignKey: "order_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "product", "sku", "description", "uom", "tax_code", "stocked", "quantity", "unit_price", "discount", "net_price",
				"amount", "reserved", "delivered"},
			Dehydrate: func(o *domain.Order) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range o.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(l.No), "product": l.Product, "sku": l.SKU, "description": l.Description, "uom": l.UoM,
						"tax_code": opt(l.TaxCode), "stocked": l.Stocked, "quantity": l.Quantity.String(), "unit_price": l.UnitPrice.StringFixed(4),
						"discount": l.Discount.StringFixed(2), "net_price": l.NetPrice.StringFixed(4), "amount": l.Amount.StringFixed(2),
						"reserved": l.Reserved.String(), "delivered": l.Delivered.String()})
				}
				return out, nil
			},
		}},
	}
}

// DeliveryMapping maps Delivery to ord_deliveries and its lines.
func DeliveryMapping() sqlrepo.Mapping[domain.DeliveryID, *domain.Delivery] {
	return sqlrepo.Mapping[domain.DeliveryID, *domain.Delivery]{
		Table:   "ord_deliveries",
		Columns: sqlrepo.WithAuditColumns("company", "customer", "order_id", "order_number", "delivery_number", "delivered_on", "warehouse"),
		Dehydrate: func(d *domain.Delivery) (sqlrepo.Values, error) {
			s := d.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "customer": s.Customer, "order_id": s.Order, "order_number": s.OrderNumber,
				"delivery_number": s.Number, "delivered_on": s.Date, "warehouse": optUUID(s.Warehouse.UUID)}, d.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Delivery, error) {
			s := domain.DeliveryState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Order: domain.OrderID{UUID: r.UUID("order_id")}, OrderNumber: r.String("order_number"), Number: r.String("delivery_number"),
				Date: r.Date("delivered_on"), Warehouse: domain.WarehouseID{UUID: r.UUID("warehouse")}, Audit: r.AuditStamp()}
			for _, c := range byNo(children.Of("lines")) {
				s.Lines = append(s.Lines, domain.DeliveryLine{Line: int(c.Int64("line_no")), Product: domain.ProductID{UUID: c.UUID("product")}, SKU: c.String("sku"),
					Description: c.String("description"), UoM: c.String("uom"), TaxCode: c.String("tax_code"), Stocked: c.Bool("stocked"),
					Quantity: c.Decimal("quantity"), NetPrice: c.Decimal("net_price"), Amount: c.Decimal("amount")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteDelivery(domain.DeliveryID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Delivery]{{
			Name: "lines", Table: "ord_delivery_lines", ForeignKey: "delivery_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "product", "sku", "description", "uom", "tax_code", "stocked", "quantity", "net_price", "amount"},
			Dehydrate: func(d *domain.Delivery) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range d.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(l.Line), "product": l.Product, "sku": l.SKU, "description": l.Description, "uom": l.UoM,
						"tax_code": opt(l.TaxCode), "stocked": l.Stocked, "quantity": l.Quantity.String(), "net_price": l.NetPrice.StringFixed(4),
						"amount": l.Amount.StringFixed(2)})
				}
				return out, nil
			},
		}},
	}
}

// CounterMapping maps Counter to ord_counters.
func CounterMapping() sqlrepo.Mapping[domain.CounterID, *domain.Counter] {
	return sqlrepo.Mapping[domain.CounterID, *domain.Counter]{
		Table:   "ord_counters",
		Columns: []string{"company", "series", "fiscal_year", "last_number"},
		Dehydrate: func(c *domain.Counter) (sqlrepo.Values, error) {
			company, series, year, last := c.Values()
			return sqlrepo.Values{"company": company, "series": series, "fiscal_year": int64(year), "last_number": last}, nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Counter, error) {
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCounter(domain.CounterID{UUID: r.UUID("id")}, domain.OrganizationID{UUID: r.UUID("company")}, r.String("series"),
				int(r.Int64("fiscal_year")), r.Int64("last_number"))
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("orders: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// Repository factories.
func OrderRepositoryFactory(b hotswap.Backend) (domain.OrderRepository, error) {
	return repository(b, OrderMapping())
}

func DeliveryRepositoryFactory(b hotswap.Backend) (domain.DeliveryRepository, error) {
	return repository(b, DeliveryMapping())
}

func TermsRepositoryFactory(b hotswap.Backend) (domain.TermsRepository, error) {
	return repository(b, TermsMapping())
}

func CounterRepositoryFactory(b hotswap.Backend) (domain.CounterRepository, error) {
	return repository(b, CounterMapping())
}

// OutboxFactory builds the domain event outbox.
func OutboxFactory(b hotswap.Backend) (application.OutboxStore, error) { return outbox(b, TableOutbox) }

// IntegrationOutboxFactory builds the integration outbox.
func IntegrationOutboxFactory(b hotswap.Backend) (application.OutboxStore, error) {
	return outbox(b, TableIntegrationOutbox)
}

func outbox(b hotswap.Backend, table string) (application.OutboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewOutbox(db, table)
	case *memory.Store:
		return memory.NewOutbox(db), nil
	}
	return nil, unsupported(b)
}

// AuditLogFactory builds the audit log.
func AuditLogFactory(b hotswap.Backend) (application.AuditLog, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewAuditLog(db, TableAuditLog)
	case *memory.Store:
		return memory.NewAuditLog(db), nil
	}
	return nil, unsupported(b)
}

// InboxFactory builds the inbox of the events Orders consumes.
func InboxFactory(b hotswap.Backend) (application.InboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewInbox(db, TableInbox)
	case *memory.Store:
		return memory.NewInbox(db), nil
	}
	return nil, unsupported(b)
}

// ProductsCatalog adapts the Products Catalog and Pricing contracts to the Orders port (ACL).
type ProductsCatalog struct {
	Catalog products.Catalog
	Pricing products.Pricing
}

var _ domain.Catalog = ProductsCatalog{}

// Price implements domain.Catalog.
func (p ProductsCatalog) Price(ctx context.Context, company domain.OrganizationID, product domain.ProductID, list domain.PriceListID, q vocab.Decimal,
	on vocab.Date) (domain.Item, bool, error) {
	refs, err := p.Catalog.Products(ctx, []string{product.String()})
	if err != nil {
		return domain.Item{}, false, err
	}
	r, ok := refs[product.String()]
	if !ok {
		return domain.Item{}, false, nil
	}
	owner, err := fw.ParseUUID(r.Company)
	if err != nil {
		return domain.Item{}, false, fmt.Errorf("orders: invalid product from Products: %+v", r)
	}
	it := domain.Item{Company: domain.OrganizationID{UUID: owner}, SKU: r.SKU, Name: r.Name, UoM: r.UoM, TaxCode: r.TaxCode, Stocked: r.Stocked,
		Sellable: r.ForSale && !r.BlockedSales}
	if r.Discontinued != "" {
		if it.Retired, err = vocab.ParseDate(r.Discontinued); err != nil {
			return domain.Item{}, false, fmt.Errorf("orders: invalid product from Products: %+v", r)
		}
	}
	if it.Company != company {
		return it, true, nil // the caller refuses it; no price of another company is asked
	}
	priceList := ""
	if !list.IsZero() {
		priceList = list.String()
	}
	quote, err := p.Pricing.Quote(ctx, company.String(), product.String(), priceList, q.String(), on.String())
	if err != nil {
		return domain.Item{}, false, err
	}
	price, err1 := vocab.ParseDecimal(quote.UnitPrice)
	discount, err2 := vocab.ParseDecimal(quote.Discount)
	if err1 != nil || err2 != nil {
		return domain.Item{}, false, fmt.Errorf("orders: invalid quote from Products: %+v", quote)
	}
	it.UnitPrice, it.Discount = price, discount
	return it, true, nil
}

// ReceivablesCredit adapts the Receivables Credit contract to the Orders port (ACL).
type ReceivablesCredit struct{ Exposure receivables.Credit }

var _ domain.CreditCheck = ReceivablesCredit{}

// Credit implements domain.CreditCheck.
func (r ReceivablesCredit) Credit(ctx context.Context, company domain.OrganizationID, customer domain.PartyID, on vocab.Date) (domain.Credit, error) {
	e, err := r.Exposure.Exposure(ctx, company.String(), customer.String(), on.String())
	if err != nil {
		return domain.Credit{}, err
	}
	out := domain.Credit{Blocked: e.Blocked, Limited: e.Limited, Available: vocab.DecimalFromInt(0)}
	if e.Limited && e.Available != "" {
		if out.Available, err = vocab.ParseDecimal(e.Available); err != nil {
			return domain.Credit{}, fmt.Errorf("orders: invalid exposure from Receivables: %+v", e)
		}
	}
	return out, nil
}

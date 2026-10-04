// Package infrastructure stores the Products context: SQL mappings, versioned schema of the five
// engines and hot-swap factories. No product is seeded (the C# had none) and the units of measure
// are code, not rows.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/products/domain"
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
const Context = "products"

// Technical tables of the context.
const (
	TableOutbox            = "products_outbox"
	TableIntegrationOutbox = "products_integration_outbox"
	TableAuditLog          = "products_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE prd_categories (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, code {str:20} NOT NULL,
	name {str:100} NOT NULL, parent_id {uuid}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_prd_categories_code ON prd_categories (company, code)`,
	`CREATE TABLE prd_products (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, sku {str:30} NOT NULL,
	name {str:120} NOT NULL, description {str:500}, kind {int} NOT NULL, uom {str:10} NOT NULL, category_id {uuid}, tax_code {str:10},
	expense_category {str:30}, base_price {str:30} NOT NULL, standard_cost {str:30} NOT NULL, for_sale {bool} NOT NULL, for_purchase {bool} NOT NULL,
	stocked {bool} NOT NULL, tracking {int} NOT NULL, blocked_sales {bool} NOT NULL, blocked_purchase {bool} NOT NULL, discontinued {date}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_prd_products_sku ON prd_products (company, sku)`,
	`CREATE INDEX ix_prd_products_category ON prd_products (company, category_id)`,
	`CREATE TABLE prd_barcodes (product_id {uuid} NOT NULL, line_no {int} NOT NULL, code_type {str:10} NOT NULL, code_value {str:30} NOT NULL,
	PRIMARY KEY (product_id, line_no), FOREIGN KEY (product_id) REFERENCES prd_products (id))`,
	`CREATE INDEX ix_prd_barcodes_value ON prd_barcodes (code_value)`,
	`CREATE TABLE prd_components (product_id {uuid} NOT NULL, line_no {int} NOT NULL, component_id {uuid} NOT NULL, quantity {str:30} NOT NULL,
	PRIMARY KEY (product_id, line_no), FOREIGN KEY (product_id) REFERENCES prd_products (id), FOREIGN KEY (component_id) REFERENCES prd_products (id))`,
	`CREATE TABLE prd_product_suppliers (product_id {uuid} NOT NULL, line_no {int} NOT NULL, supplier {uuid} NOT NULL, supplier_code {str:50},
	lead_days {int} NOT NULL, preferred {bool} NOT NULL, PRIMARY KEY (product_id, line_no), FOREIGN KEY (product_id) REFERENCES prd_products (id))`,
	`CREATE TABLE prd_price_lists (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, code {str:20} NOT NULL,
	name {str:100} NOT NULL, currency {str:3} NOT NULL, active {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_prd_price_lists_code ON prd_price_lists (company, code)`,
	`CREATE TABLE prd_price_lines (price_list_id {uuid} NOT NULL, line_no {int} NOT NULL, product_id {uuid} NOT NULL, min_quantity {str:30} NOT NULL,
	unit_price {str:30} NOT NULL, discount {str:10} NOT NULL, valid_from {date} NOT NULL, valid_to {date}, PRIMARY KEY (price_list_id, line_no),
	FOREIGN KEY (price_list_id) REFERENCES prd_price_lists (id), FOREIGN KEY (product_id) REFERENCES prd_products (id))`,
	`CREATE INDEX ix_prd_price_lines_product ON prd_price_lines (product_id)`,
}

func technicalDDL(d string) []string {
	switch d {
	case "sqlite":
		return slices.Concat(sqlite.OutboxDDL(TableOutbox), sqlite.OutboxDDL(TableIntegrationOutbox), sqlite.AuditDDL(TableAuditLog))
	case "postgres":
		return slices.Concat(postgres.OutboxDDL(TableOutbox), postgres.OutboxDDL(TableIntegrationOutbox), postgres.AuditDDL(TableAuditLog))
	case "sqlserver":
		return slices.Concat(sqlserver.OutboxDDL(TableOutbox), sqlserver.OutboxDDL(TableIntegrationOutbox), sqlserver.AuditDDL(TableAuditLog))
	case "oracle":
		return slices.Concat(oracle.OutboxDDL(TableOutbox), oracle.OutboxDDL(TableIntegrationOutbox), oracle.AuditDDL(TableAuditLog))
	case "mysql":
		return slices.Concat(mysql.OutboxDDL(TableOutbox), mysql.OutboxDDL(TableIntegrationOutbox), mysql.AuditDDL(TableAuditLog))
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
		{Version: 1, Name: "categories, products and price lists", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"prd_price_lines", "prd_price_lists", "prd_product_suppliers", "prd_components", "prd_barcodes", "prd_products", "prd_categories",
	TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

func optDate(d vocab.Date) any {
	if d.IsZero() {
		return nil
	}
	return d
}

func byNo(rows []*sqlrepo.Row) []*sqlrepo.Row {
	out := slices.Clone(rows)
	slices.SortFunc(out, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
	return out
}

// CategoryMapping maps Category to prd_categories.
func CategoryMapping() sqlrepo.Mapping[domain.CategoryID, *domain.Category] {
	return sqlrepo.Mapping[domain.CategoryID, *domain.Category]{
		Table:   "prd_categories",
		Columns: sqlrepo.WithAuditColumns("company", "code", "name", "parent_id"),
		Dehydrate: func(c *domain.Category) (sqlrepo.Values, error) {
			s := c.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "code": s.Code, "name": s.Name, "parent_id": optUUID(s.Parent.UUID)},
				c.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Category, error) {
			s := domain.CategoryState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("code"), Name: r.String("name"),
				Parent: domain.CategoryID{UUID: r.UUID("parent_id")}, Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCategory(domain.CategoryID{UUID: r.UUID("id")}, s)
		},
	}
}

// ProductMapping maps Product to prd_products, its barcodes, components and suppliers.
func ProductMapping() sqlrepo.Mapping[domain.ProductID, *domain.Product] {
	return sqlrepo.Mapping[domain.ProductID, *domain.Product]{
		Table: "prd_products",
		Columns: sqlrepo.WithAuditColumns("company", "sku", "name", "description", "kind", "uom", "category_id", "tax_code", "expense_category",
			"base_price", "standard_cost", "for_sale", "for_purchase", "stocked", "tracking", "blocked_sales", "blocked_purchase", "discontinued"),
		Dehydrate: func(p *domain.Product) (sqlrepo.Values, error) {
			s := p.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "sku": s.SKU, "name": s.Name, "description": opt(s.Description),
				"kind": int64(s.Kind), "uom": s.UoM, "category_id": optUUID(s.Category.UUID), "tax_code": opt(s.TaxCode),
				"expense_category": opt(s.ExpenseCategory), "base_price": s.BasePrice.StringFixed(4), "standard_cost": s.StandardCost.StringFixed(4),
				"for_sale": s.ForSale, "for_purchase": s.ForPurchase, "stocked": s.Stocked, "tracking": int64(s.Tracking), "blocked_sales": s.BlockedSales,
				"blocked_purchase": s.BlockedPurchase, "discontinued": optDate(s.Discontinued)}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Product, error) {
			s := domain.ProductState{Company: domain.OrganizationID{UUID: r.UUID("company")}, SKU: r.String("sku"), Discontinued: r.Date("discontinued"),
				Audit: r.AuditStamp(), Details: domain.Details{Name: r.String("name"), Description: r.String("description"), Kind: domain.Kind(r.Int64("kind")),
					UoM: r.String("uom"), Category: domain.CategoryID{UUID: r.UUID("category_id")}, TaxCode: r.String("tax_code"),
					ExpenseCategory: r.String("expense_category"), BasePrice: r.Decimal("base_price"), StandardCost: r.Decimal("standard_cost"),
					ForSale: r.Bool("for_sale"), ForPurchase: r.Bool("for_purchase"), Stocked: r.Bool("stocked"), Tracking: domain.Tracking(r.Int64("tracking")),
					BlockedSales: r.Bool("blocked_sales"), BlockedPurchase: r.Bool("blocked_purchase")}}
			for _, c := range byNo(children.Of("barcodes")) {
				s.Barcodes = append(s.Barcodes, domain.Barcode{Type: c.String("code_type"), Value: c.String("code_value")})
			}
			for _, c := range byNo(children.Of("components")) {
				s.Components = append(s.Components, domain.Component{Product: domain.ProductID{UUID: c.UUID("component_id")}, Quantity: c.Decimal("quantity")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("suppliers")) {
				s.Suppliers = append(s.Suppliers, domain.SupplierItem{Supplier: domain.PartyID{UUID: c.UUID("supplier")}, Code: c.String("supplier_code"),
					LeadDays: int(c.Int64("lead_days")), Preferred: c.Bool("preferred")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteProduct(domain.ProductID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Product]{{
			Name: "barcodes", Table: "prd_barcodes", ForeignKey: "product_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "code_type", "code_value"},
			Dehydrate: func(p *domain.Product) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, b := range p.State().Barcodes {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "code_type": b.Type, "code_value": b.Value})
				}
				return out, nil
			},
		}, {
			Name: "components", Table: "prd_components", ForeignKey: "product_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "component_id", "quantity"},
			Dehydrate: func(p *domain.Product) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, c := range p.State().Components {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "component_id": c.Product, "quantity": c.Quantity.String()})
				}
				return out, nil
			},
		}, {
			Name: "suppliers", Table: "prd_product_suppliers", ForeignKey: "product_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "supplier", "supplier_code", "lead_days", "preferred"},
			Dehydrate: func(p *domain.Product) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, x := range p.State().Suppliers {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "supplier": x.Supplier, "supplier_code": opt(x.Code),
						"lead_days": int64(x.LeadDays), "preferred": x.Preferred})
				}
				return out, nil
			},
		}},
	}
}

// PriceListMapping maps PriceList to prd_price_lists and its lines.
func PriceListMapping() sqlrepo.Mapping[domain.PriceListID, *domain.PriceList] {
	return sqlrepo.Mapping[domain.PriceListID, *domain.PriceList]{
		Table:   "prd_price_lists",
		Columns: sqlrepo.WithAuditColumns("company", "code", "name", "currency", "active"),
		Dehydrate: func(p *domain.PriceList) (sqlrepo.Values, error) {
			s := p.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "code": s.Code, "name": s.Name, "currency": s.Currency.String(),
				"active": s.Active}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.PriceList, error) {
			cur, err := vocab.NewCurrencyCode(r.String("currency"))
			if err != nil {
				return nil, err
			}
			s := domain.PriceListState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("code"), Name: r.String("name"),
				Currency: cur, Active: r.Bool("active"), Audit: r.AuditStamp()}
			for _, c := range byNo(children.Of("lines")) {
				s.Lines = append(s.Lines, domain.PriceLine{Product: domain.ProductID{UUID: c.UUID("product_id")}, MinQuantity: c.Decimal("min_quantity"),
					UnitPrice: c.Decimal("unit_price"), Discount: c.Decimal("discount"), From: c.Date("valid_from"), To: c.Date("valid_to")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstitutePriceList(domain.PriceListID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.PriceList]{{
			Name: "lines", Table: "prd_price_lines", ForeignKey: "price_list_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "product_id", "min_quantity", "unit_price", "discount", "valid_from", "valid_to"},
			Dehydrate: func(p *domain.PriceList) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, l := range p.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "product_id": l.Product, "min_quantity": l.MinQuantity.String(),
						"unit_price": l.UnitPrice.StringFixed(4), "discount": l.Discount.StringFixed(2), "valid_from": l.From, "valid_to": optDate(l.To)})
				}
				return out, nil
			},
		}},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("products: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// ProductRepositoryFactory builds the product repository.
func ProductRepositoryFactory(b hotswap.Backend) (domain.ProductRepository, error) {
	return repository(b, ProductMapping())
}

// CategoryRepositoryFactory builds the category repository.
func CategoryRepositoryFactory(b hotswap.Backend) (domain.CategoryRepository, error) {
	return repository(b, CategoryMapping())
}

// PriceListRepositoryFactory builds the price list repository.
func PriceListRepositoryFactory(b hotswap.Backend) (domain.PriceListRepository, error) {
	return repository(b, PriceListMapping())
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

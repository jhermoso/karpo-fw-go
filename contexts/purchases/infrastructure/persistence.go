// Package infrastructure stores the Purchases context: SQL mappings, versioned schema of the five
// engines, hot-swap factories and the adapter to the Fiscal tax engine.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	fiscal "github.com/jhermoso/karpo-fw-go/contexts/fiscal/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/purchases/domain"
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
const Context = "purchases"

// Technical tables of the context.
const (
	TableOutbox            = "purchases_outbox"
	TableIntegrationOutbox = "purchases_integration_outbox"
	TableAuditLog          = "purchases_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE pur_invoices (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, supplier {uuid} NOT NULL,
	supplier_number {str:60} NOT NULL, register_number {str:20} NOT NULL, fiscal_year {int} NOT NULL, issued {date} NOT NULL, received {date} NOT NULL,
	due_on {date} NOT NULL, corrects {uuid}, non_deductible {bool} NOT NULL, withholding_rate {str:10} NOT NULL, withholding {str:30} NOT NULL,
	country {str:2}, net {str:30} NOT NULL, tax {str:30} NOT NULL, declared_total {str:30} NOT NULL, cancelled {bool} NOT NULL,
	cancel_reason {str:200}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_pur_invoices_register ON pur_invoices (company, register_number)`,
	`CREATE INDEX ix_pur_invoices_supplier ON pur_invoices (company, supplier, supplier_number)`,
	`CREATE INDEX ix_pur_invoices_received ON pur_invoices (company, received)`,
	`CREATE TABLE pur_invoice_lines (invoice_id {uuid} NOT NULL, line_no {int} NOT NULL, description {str:200}, category {str:30} NOT NULL,
	base {str:30} NOT NULL, tax_code {str:10}, treatment {str:10}, PRIMARY KEY (invoice_id, line_no),
	FOREIGN KEY (invoice_id) REFERENCES pur_invoices (id))`,
	`CREATE TABLE pur_invoice_taxes (invoice_id {uuid} NOT NULL, line_no {int} NOT NULL, tax_type {str:10}, tax_code {str:10}, treatment {str:10},
	treatment_kind {str:20} NOT NULL, rate {str:10} NOT NULL, base {str:30} NOT NULL, amount {str:30} NOT NULL, PRIMARY KEY (invoice_id, line_no),
	FOREIGN KEY (invoice_id) REFERENCES pur_invoices (id))`,
	`CREATE TABLE pur_invoice_accounts (invoice_id {uuid} NOT NULL, line_no {int} NOT NULL, iban {str:34} NOT NULL, amount {str:30} NOT NULL,
	PRIMARY KEY (invoice_id, line_no), FOREIGN KEY (invoice_id) REFERENCES pur_invoices (id))`,
	`CREATE TABLE pur_suppliers (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, supplier {uuid} NOT NULL,
	category {str:30}, withholding_rate {str:10} NOT NULL, payment_days {int} NOT NULL, iban {str:34}, blocked {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_pur_suppliers ON pur_suppliers (company, supplier)`,
	`CREATE TABLE pur_counters (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, fiscal_year {int} NOT NULL,
	last_number {bigint} NOT NULL)`,
	`CREATE UNIQUE INDEX ux_pur_counters ON pur_counters (company, fiscal_year)`,
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
		{Version: 1, Name: "received invoices, suppliers and register", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"pur_invoice_accounts", "pur_invoice_taxes", "pur_invoice_lines", "pur_invoices", "pur_suppliers", "pur_counters", TableOutbox,
	TableIntegrationOutbox, TableAuditLog}

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

func byNo(rows []*sqlrepo.Row) []*sqlrepo.Row {
	out := slices.Clone(rows)
	slices.SortFunc(out, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
	return out
}

// InvoiceMapping maps Invoice to pur_invoices, its lines, tax breakdown and accounts.
func InvoiceMapping() sqlrepo.Mapping[domain.InvoiceID, *domain.Invoice] {
	return sqlrepo.Mapping[domain.InvoiceID, *domain.Invoice]{
		Table: "pur_invoices",
		Columns: sqlrepo.WithAuditColumns("company", "supplier", "supplier_number", "register_number", "fiscal_year", "issued", "received", "due_on",
			"corrects", "non_deductible", "withholding_rate", "withholding", "country", "net", "tax", "declared_total", "cancelled", "cancel_reason"),
		Dehydrate: func(i *domain.Invoice) (sqlrepo.Values, error) {
			s := i.State()
			var corrects any
			if !s.Corrects.IsZero() {
				corrects = s.Corrects
			}
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "supplier": s.Supplier, "supplier_number": s.SupplierNumber,
				"register_number": s.Register, "fiscal_year": int64(s.Year), "issued": s.Issued, "received": s.Received, "due_on": s.Due, "corrects": corrects,
				"non_deductible": s.NonDeductible, "withholding_rate": s.WithholdingRate.StringFixed(2), "withholding": s.Withholding.StringFixed(2),
				"country": opt(s.Breakdown.Country), "net": s.Breakdown.Net.StringFixed(2), "tax": s.Breakdown.Tax.StringFixed(2),
				"declared_total": s.DeclaredTotal.StringFixed(2), "cancelled": s.Cancelled, "cancel_reason": opt(s.CancelReason)}, i.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Invoice, error) {
			s := domain.InvoiceState{Draft: domain.Draft{Company: domain.OrganizationID{UUID: r.UUID("company")}, Supplier: domain.PartyID{UUID: r.UUID("supplier")},
				SupplierNumber: r.String("supplier_number"), Issued: r.Date("issued"), Received: r.Date("received"), Due: r.Date("due_on"),
				Corrects: domain.InvoiceID{UUID: r.UUID("corrects")}, NonDeductible: r.Bool("non_deductible"), WithholdingRate: r.Decimal("withholding_rate"),
				DeclaredTotal: r.Decimal("declared_total")}, Register: r.String("register_number"), Year: int(r.Int64("fiscal_year")),
				Breakdown: domain.Breakdown{Country: r.String("country"), Net: r.Decimal("net"), Tax: r.Decimal("tax")}, Withholding: r.Decimal("withholding"),
				Cancelled: r.Bool("cancelled"), CancelReason: r.String("cancel_reason"), Audit: r.AuditStamp()}
			for _, c := range byNo(children.Of("lines")) {
				s.Lines = append(s.Lines, domain.Line{No: int(c.Int64("line_no")), Description: c.String("description"), Category: domain.Category(c.String("category")),
					Base: c.Decimal("base"), TaxCode: c.String("tax_code"), Treatment: c.String("treatment")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("taxes")) {
				s.Breakdown.Lines = append(s.Breakdown.Lines, domain.TaxLine{TaxType: c.String("tax_type"), TaxCode: c.String("tax_code"),
					Treatment: c.String("treatment"), TreatmentKind: c.String("treatment_kind"), Rate: c.Decimal("rate"), Base: c.Decimal("base"),
					Amount: c.Decimal("amount")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("accounts")) {
				iban, err := vocab.NewIBAN(c.String("iban"))
				if err != nil {
					return nil, err
				}
				s.PayTo = append(s.PayTo, domain.PayTo{IBAN: iban, Amount: c.Decimal("amount")})
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteInvoice(domain.InvoiceID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Invoice]{{
			Name: "lines", Table: "pur_invoice_lines", ForeignKey: "invoice_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "description", "category", "base", "tax_code", "treatment"},
			Dehydrate: func(i *domain.Invoice) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range i.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(l.No), "description": opt(l.Description), "category": string(l.Category),
						"base": l.Base.StringFixed(2), "tax_code": opt(l.TaxCode), "treatment": opt(l.Treatment)})
				}
				return out, nil
			},
		}, {
			Name: "taxes", Table: "pur_invoice_taxes", ForeignKey: "invoice_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "tax_type", "tax_code", "treatment", "treatment_kind", "rate", "base", "amount"},
			Dehydrate: func(i *domain.Invoice) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, t := range i.State().Breakdown.Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "tax_type": opt(t.TaxType), "tax_code": opt(t.TaxCode), "treatment": opt(t.Treatment),
						"treatment_kind": t.TreatmentKind, "rate": t.Rate.StringFixed(2), "base": t.Base.StringFixed(2), "amount": t.Amount.StringFixed(2)})
				}
				return out, nil
			},
		}, {
			Name: "accounts", Table: "pur_invoice_accounts", ForeignKey: "invoice_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "iban", "amount"},
			Dehydrate: func(i *domain.Invoice) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, p := range i.State().PayTo {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "iban": p.IBAN.String(), "amount": p.Amount.StringFixed(2)})
				}
				return out, nil
			},
		}},
	}
}

// SupplierMapping maps SupplierProfile to pur_suppliers.
func SupplierMapping() sqlrepo.Mapping[domain.SupplierID, *domain.SupplierProfile] {
	return sqlrepo.Mapping[domain.SupplierID, *domain.SupplierProfile]{
		Table:   "pur_suppliers",
		Columns: sqlrepo.WithAuditColumns("company", "supplier", "category", "withholding_rate", "payment_days", "iban", "blocked"),
		Dehydrate: func(p *domain.SupplierProfile) (sqlrepo.Values, error) {
			s := p.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "supplier": s.Supplier, "category": opt(string(s.Category)),
				"withholding_rate": s.WithholdingRate.StringFixed(2), "payment_days": int64(s.PaymentDays), "iban": opt(s.IBAN.String()),
				"blocked": s.Blocked}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.SupplierProfile, error) {
			s := domain.SupplierState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Supplier: domain.PartyID{UUID: r.UUID("supplier")},
				Category: domain.Category(r.String("category")), WithholdingRate: r.Decimal("withholding_rate"), PaymentDays: int(r.Int64("payment_days")),
				Blocked: r.Bool("blocked"), Audit: r.AuditStamp()}
			if t := r.String("iban"); t != "" {
				iban, err := vocab.NewIBAN(t)
				if err != nil {
					return nil, err
				}
				s.IBAN = iban
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteSupplier(domain.SupplierID{UUID: r.UUID("id")}, s)
		},
	}
}

// CounterMapping maps Counter to pur_counters.
func CounterMapping() sqlrepo.Mapping[domain.CounterID, *domain.Counter] {
	return sqlrepo.Mapping[domain.CounterID, *domain.Counter]{
		Table:   "pur_counters",
		Columns: []string{"company", "fiscal_year", "last_number"},
		Dehydrate: func(c *domain.Counter) (sqlrepo.Values, error) {
			company, year, last := c.Values()
			return sqlrepo.Values{"company": company, "fiscal_year": int64(year), "last_number": last}, nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Counter, error) {
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCounter(domain.CounterID{UUID: r.UUID("id")}, domain.OrganizationID{UUID: r.UUID("company")},
				int(r.Int64("fiscal_year")), r.Int64("last_number"))
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("purchases: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// InvoiceRepositoryFactory builds the invoice repository.
func InvoiceRepositoryFactory(b hotswap.Backend) (domain.InvoiceRepository, error) {
	return repository(b, InvoiceMapping())
}

// SupplierRepositoryFactory builds the supplier profile repository.
func SupplierRepositoryFactory(b hotswap.Backend) (domain.SupplierRepository, error) {
	return repository(b, SupplierMapping())
}

// CounterRepositoryFactory builds the register counter repository.
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

// FiscalTaxes adapts the Fiscal TaxEngine to the Purchases Taxes port (ACL). The buying company is
// the taxpayer whose jurisdiction applies (phase 1: both parties in the same territory).
type FiscalTaxes struct{ Engine fiscal.TaxEngine }

var _ domain.Taxes = FiscalTaxes{}

// Calculate implements domain.Taxes.
func (f FiscalTaxes) Calculate(ctx context.Context, company domain.OrganizationID, accrual vocab.Date, lines []domain.TaxableLine) (domain.Breakdown, error) {
	doc := fiscal.TaxableDocument{Seller: company.String(), Date: accrual.String()}
	for _, l := range lines {
		doc.Lines = append(doc.Lines, fiscal.TaxableLine{Ref: l.Ref, Base: l.Base.StringFixed(2), TaxCode: l.TaxCode, Treatment: l.Treatment})
	}
	r, err := f.Engine.Calculate(ctx, doc)
	if err != nil {
		return domain.Breakdown{}, err
	}
	d := func(s string) vocab.Decimal {
		if s == "" {
			return vocab.DecimalFromInt(0)
		}
		x, perr := vocab.ParseDecimal(s)
		if perr != nil && err == nil {
			err = perr
		}
		return x
	}
	out := domain.Breakdown{Country: r.Country, Net: d(r.Net), Tax: d(r.Tax)}
	for _, l := range r.Lines {
		out.Lines = append(out.Lines, domain.TaxLine{TaxType: l.TaxType, TaxCode: l.TaxCode, Treatment: l.Treatment, TreatmentKind: l.TreatmentKind,
			Rate: d(l.Rate), Base: d(l.Base), Amount: d(l.Amount)})
	}
	return out, err
}

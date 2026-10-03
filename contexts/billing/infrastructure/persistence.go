// Package infrastructure stores the Billing context: SQL mappings, versioned schema of the five
// engines, hot-swap factories and the adapters to Fiscal (tax engine) and Parties (identities).
package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	fiscal "github.com/jhermoso/karpo-fw-go/contexts/fiscal/contracts"
	parties "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
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
const Context = "billing"

// Technical tables of the context.
const (
	TableOutbox            = "billing_outbox"
	TableIntegrationOutbox = "billing_integration_outbox"
	TableAuditLog          = "billing_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE bil_series (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, seller {uuid} NOT NULL, code {str:20} NOT NULL,
	series_year {int} NOT NULL, last_number {bigint} NOT NULL, corrective {bool} NOT NULL, active {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_bil_series_code ON bil_series (seller, code, series_year)`,
	`CREATE TABLE bil_invoices (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, seller {uuid} NOT NULL, customer {uuid} NOT NULL,
	kind {int} NOT NULL, status {int} NOT NULL, currency {str:3} NOT NULL, description {str:500}, operation_date {date}, due_date {date},
	equivalence_surcharge {bool} NOT NULL, corrects {uuid}, reason {str:2}, series_id {uuid}, invoice_number {str:40}, issue_date {date},
	seller_nif {str:20}, seller_name {str:200}, seller_country {str:2}, customer_nif {str:20}, customer_name {str:200}, customer_country {str:2},
	tax_country {str:2}, net {str:30}, tax_amount {str:30}, surcharge {str:30}, ` + audit + `,
	FOREIGN KEY (series_id) REFERENCES bil_series (id), FOREIGN KEY (corrects) REFERENCES bil_invoices (id))`,
	`CREATE INDEX ix_bil_invoices_seller ON bil_invoices (seller, issue_date)`,
	`CREATE INDEX ix_bil_invoices_customer ON bil_invoices (customer)`,
	`CREATE TABLE bil_invoice_lines (id {uuid} NOT NULL PRIMARY KEY, invoice_id {uuid} NOT NULL, description {str:500} NOT NULL,
	quantity {str:30} NOT NULL, unit_price {str:30} NOT NULL, discount {str:20}, tax_code {str:5}, treatment {str:5}, net {str:30} NOT NULL,
	FOREIGN KEY (invoice_id) REFERENCES bil_invoices (id))`,
	`CREATE INDEX ix_bil_invoice_lines_invoice ON bil_invoice_lines (invoice_id)`,
	`CREATE TABLE bil_invoice_taxes (invoice_id {uuid} NOT NULL, line_no {int} NOT NULL, tax_type {str:10}, tax_code {str:5}, treatment {str:5},
	treatment_kind {str:20} NOT NULL, rate {str:20} NOT NULL, base {str:30} NOT NULL, amount {str:30} NOT NULL, surcharge_rate {str:20},
	surcharge_amount {str:30}, PRIMARY KEY (invoice_id, line_no), FOREIGN KEY (invoice_id) REFERENCES bil_invoices (id))`,
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
		{Version: 1, Name: "series and invoices", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"bil_invoice_taxes", "bil_invoice_lines", "bil_invoices", "bil_series", TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

func optDecimal(d vocab.Decimal) any {
	if d.IsZero() {
		return nil
	}
	return d.String()
}

// SeriesMapping maps Series to bil_series.
func SeriesMapping() sqlrepo.Mapping[domain.SeriesID, *domain.Series] {
	return sqlrepo.Mapping[domain.SeriesID, *domain.Series]{
		Table:   "bil_series",
		Columns: sqlrepo.WithAuditColumns("seller", "code", "series_year", "last_number", "corrective", "active"),
		Dehydrate: func(s *domain.Series) (sqlrepo.Values, error) {
			st := s.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"seller": st.Seller, "code": st.Code, "series_year": int64(st.Year), "last_number": st.Last,
				"corrective": st.Corrective, "active": st.Active}, s.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Series, error) {
			st := domain.SeriesState{Seller: domain.OrganizationID{UUID: r.UUID("seller")}, Code: r.String("code"), Year: int(r.Int64("series_year")),
				Last: r.Int64("last_number"), Corrective: r.Bool("corrective"), Active: r.Bool("active"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteSeries(domain.SeriesID{UUID: r.UUID("id")}, st)
		},
	}
}

// InvoiceMapping maps Invoice to bil_invoices, its lines and its tax breakdown.
func InvoiceMapping() sqlrepo.Mapping[domain.InvoiceID, *domain.Invoice] {
	return sqlrepo.Mapping[domain.InvoiceID, *domain.Invoice]{
		Table: "bil_invoices",
		Columns: sqlrepo.WithAuditColumns("seller", "customer", "kind", "status", "currency", "description", "operation_date", "due_date",
			"equivalence_surcharge", "corrects", "reason", "series_id", "invoice_number", "issue_date", "seller_nif", "seller_name", "seller_country",
			"customer_nif", "customer_name", "customer_country", "tax_country", "net", "tax_amount", "surcharge"),
		Dehydrate: func(i *domain.Invoice) (sqlrepo.Values, error) {
			s := i.State()
			v := sqlrepo.Values{"seller": s.Seller, "customer": s.Customer, "kind": int64(s.Kind), "status": int64(s.Status), "currency": s.Currency.String(),
				"description": opt(s.Description), "operation_date": optDate(s.OperationDate), "due_date": optDate(s.DueDate),
				"equivalence_surcharge": s.EquivalenceSurcharge, "corrects": optUUID(s.Corrects.UUID), "reason": opt(s.Reason),
				"series_id": optUUID(s.Series.UUID), "invoice_number": opt(s.Number), "issue_date": optDate(s.IssueDate),
				"seller_nif": opt(s.SellerIdentity.NIF), "seller_name": opt(s.SellerIdentity.Name), "seller_country": opt(s.SellerIdentity.Country),
				"customer_nif": opt(s.CustomerIdentity.NIF), "customer_name": opt(s.CustomerIdentity.Name), "customer_country": opt(s.CustomerIdentity.Country),
				"tax_country": opt(s.Taxes.Country), "net": i.Net().StringFixed(2), "tax_amount": nil, "surcharge": nil}
			if s.Status == domain.Issued {
				v["tax_amount"], v["surcharge"] = s.Taxes.Tax.StringFixed(2), s.Taxes.Surcharge.StringFixed(2)
			}
			return sqlrepo.AuditStampValues(v, i.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Invoice, error) {
			cur, err := vocab.NewCurrencyCode(r.String("currency"))
			if err != nil {
				return nil, err
			}
			s := domain.InvoiceState{Seller: domain.OrganizationID{UUID: r.UUID("seller")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Kind: domain.Kind(r.Int64("kind")), Status: domain.Status(r.Int64("status")), Currency: cur, Description: r.String("description"),
				OperationDate: r.Date("operation_date"), DueDate: r.Date("due_date"), EquivalenceSurcharge: r.Bool("equivalence_surcharge"),
				Corrects: domain.InvoiceID{UUID: r.UUID("corrects")}, Reason: r.String("reason"), Series: domain.SeriesID{UUID: r.UUID("series_id")},
				Number: r.String("invoice_number"), IssueDate: r.Date("issue_date"), Audit: r.AuditStamp(),
				SellerIdentity:   domain.Identity{NIF: r.String("seller_nif"), Name: r.String("seller_name"), Country: r.String("seller_country")},
				CustomerIdentity: domain.Identity{NIF: r.String("customer_nif"), Name: r.String("customer_name"), Country: r.String("customer_country")}}
			for _, c := range children.Of("lines") {
				s.Lines = append(s.Lines, domain.Line{ID: domain.LineID{UUID: c.UUID("id")}, Description: c.String("description"),
					Quantity: c.Decimal("quantity"), UnitPrice: c.Decimal("unit_price"), Discount: c.Decimal("discount"), TaxCode: c.String("tax_code"),
					Treatment: c.String("treatment"), Net: c.Decimal("net")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(s.Lines, func(a, b domain.Line) int { return bytes.Compare(a.ID.Bytes(), b.ID.Bytes()) })
			if s.Status == domain.Issued {
				s.Taxes = domain.Breakdown{Country: r.String("tax_country"), Net: r.Decimal("net"), Tax: r.Decimal("tax_amount"), Surcharge: r.Decimal("surcharge")}
				type line struct {
					no int64
					l  domain.TaxLine
				}
				var ls []line
				for _, c := range children.Of("taxes") {
					ls = append(ls, line{c.Int64("line_no"), domain.TaxLine{TaxType: c.String("tax_type"), TaxCode: c.String("tax_code"),
						Treatment: c.String("treatment"), TreatmentKind: c.String("treatment_kind"), Rate: c.Decimal("rate"), Base: c.Decimal("base"),
						Amount: c.Decimal("amount"), SurchargeRate: c.Decimal("surcharge_rate"), SurchargeAmount: c.Decimal("surcharge_amount")}})
					if err := c.Err(); err != nil {
						return nil, err
					}
				}
				slices.SortFunc(ls, func(a, b line) int { return int(a.no - b.no) })
				for _, l := range ls {
					s.Taxes.Lines = append(s.Taxes.Lines, l.l)
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteInvoice(domain.InvoiceID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Invoice]{{
			Name: "lines", Table: "bil_invoice_lines", ForeignKey: "invoice_id", OrderBy: []string{"id"},
			Columns: []string{"id", "description", "quantity", "unit_price", "discount", "tax_code", "treatment", "net"},
			Dehydrate: func(i *domain.Invoice) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range i.State().Lines {
					out = append(out, sqlrepo.Values{"id": l.ID, "description": l.Description, "quantity": l.Quantity.String(),
						"unit_price": l.UnitPrice.String(), "discount": optDecimal(l.Discount), "tax_code": opt(l.TaxCode), "treatment": opt(l.Treatment),
						"net": l.Net.StringFixed(2)})
				}
				return out, nil
			},
		}, {
			Name: "taxes", Table: "bil_invoice_taxes", ForeignKey: "invoice_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "tax_type", "tax_code", "treatment", "treatment_kind", "rate", "base", "amount", "surcharge_rate", "surcharge_amount"},
			Dehydrate: func(i *domain.Invoice) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, l := range i.State().Taxes.Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "tax_type": opt(l.TaxType), "tax_code": opt(l.TaxCode),
						"treatment": opt(l.Treatment), "treatment_kind": l.TreatmentKind, "rate": l.Rate.String(), "base": l.Base.StringFixed(2),
						"amount": l.Amount.StringFixed(2), "surcharge_rate": optDecimal(l.SurchargeRate), "surcharge_amount": optDecimal(l.SurchargeAmount)})
				}
				return out, nil
			},
		}},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("billing: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// SeriesRepositoryFactory builds the series repository.
func SeriesRepositoryFactory(b hotswap.Backend) (domain.SeriesRepository, error) {
	return repository(b, SeriesMapping())
}

// InvoiceRepositoryFactory builds the invoice repository.
func InvoiceRepositoryFactory(b hotswap.Backend) (domain.InvoiceRepository, error) {
	return repository(b, InvoiceMapping())
}

// OutboxFactory builds the domain event outbox; IntegrationOutboxFactory, the Published Language one.
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

// FiscalTaxes adapts the Fiscal TaxEngine to the Billing Taxes port (ACL).
type FiscalTaxes struct{ Engine fiscal.TaxEngine }

var _ domain.Taxes = FiscalTaxes{}

// Calculate implements domain.Taxes.
func (f FiscalTaxes) Calculate(ctx context.Context, seller domain.OrganizationID, accrual vocab.Date, surcharge bool, lines []domain.TaxableLine) (domain.Breakdown, error) {
	doc := fiscal.TaxableDocument{Seller: seller.String(), Date: accrual.String(), EquivalenceSurcharge: surcharge}
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
	out := domain.Breakdown{Country: r.Country, Net: d(r.Net), Tax: d(r.Tax), Surcharge: d(r.Surcharge)}
	for _, l := range r.Lines {
		out.Lines = append(out.Lines, domain.TaxLine{TaxType: l.TaxType, TaxCode: l.TaxCode, Treatment: l.Treatment, TreatmentKind: l.TreatmentKind,
			Rate: d(l.Rate), Base: d(l.Base), Amount: d(l.Amount), SurchargeRate: d(l.SurchargeRate), SurchargeAmount: d(l.SurchargeAmount)})
	}
	return out, err
}

// PartiesIdentities adapts the Parties TaxIdentities contract to the Billing Identities port (ACL).
type PartiesIdentities struct{ TaxIdentities parties.TaxIdentities }

var _ domain.Identities = PartiesIdentities{}

// Identities implements domain.Identities.
func (p PartiesIdentities) Identities(ctx context.Context, ids []domain.PartyID) (map[domain.PartyID]domain.Identity, error) {
	req := make([]string, 0, len(ids))
	for _, id := range ids {
		req = append(req, id.String())
	}
	m, err := p.TaxIdentities.TaxIdentities(ctx, req)
	if err != nil {
		return nil, err
	}
	out := map[domain.PartyID]domain.Identity{}
	for id, ti := range m {
		u, err := fw.ParseUUID(id)
		if err != nil {
			continue
		}
		out[domain.PartyID{UUID: u}] = domain.Identity{NIF: vocab.NormalizeDocumentNumber(ti.Number), Name: ti.Name, Country: ti.Country}
	}
	return out, nil
}

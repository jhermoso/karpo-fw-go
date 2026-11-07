// Package infrastructure guarda el contexto Billing: mapeos SQL, esquema versionado de los cinco
// motores, factorías intercambiables en caliente y los adaptadores a Fiscal (motor de impuestos) y
// Parties (identidades).
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

// Context es el nombre del contexto delimitado.
const Context = "billing"

// Tablas técnicas del contexto.
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

// TableInbox es el inbox de los eventos que consume Billing.
const TableInbox = "billing_inbox"

var sourceDDL = []string{
	`ALTER TABLE bil_invoices {add:source_type} {str:80}{addEnd}`,
	`ALTER TABLE bil_invoices {add:source_id} {str:80}{addEnd}`,
	`ALTER TABLE bil_invoices {add:source_ref} {str:40}{addEnd}`,
	`CREATE INDEX ix_bil_invoices_source ON bil_invoices (seller, source_type, source_id)`,
}

// uniqueNumber impide que un número se repita en su serie aunque dos emisiones se crucen. Los
// borradores no tienen serie ni número, y en SQL Server el filtro los deja fuera del índice.
const uniqueNumber = "ux_bil_invoices_number"

var numberDDL = []string{
	`CREATE UNIQUE INDEX ` + uniqueNumber + ` ON bil_invoices (series_id, invoice_number){whereNotNull:invoice_number}`,
}

// issueDateDDL guarda en la serie la fecha de su última factura, para que no retroceda, y la toma de
// las facturas ya emitidas.
var issueDateDDL = []string{
	`ALTER TABLE bil_series {add:last_issue_date} {date}{addEnd}`,
	`UPDATE bil_series SET last_issue_date = (SELECT MAX(issue_date) FROM bil_invoices WHERE bil_invoices.series_id = bil_series.id)`,
}

// addressDDL congela en la factura el domicilio fiscal de las dos partes (plan Sage, T1-G2). La calle
// cabe entera: tipo de vía, línea 1, número y resto, y línea 2 de Parties suman como mucho unos 540.
var addressDDL = []string{
	`ALTER TABLE bil_invoices {add:seller_street} {str:600}{addEnd}`,
	`ALTER TABLE bil_invoices {add:seller_postal_code} {str:20}{addEnd}`,
	`ALTER TABLE bil_invoices {add:seller_locality} {str:100}{addEnd}`,
	`ALTER TABLE bil_invoices {add:seller_region} {str:100}{addEnd}`,
	`ALTER TABLE bil_invoices {add:seller_address_country} {str:2}{addEnd}`,
	`ALTER TABLE bil_invoices {add:customer_street} {str:600}{addEnd}`,
	`ALTER TABLE bil_invoices {add:customer_postal_code} {str:20}{addEnd}`,
	`ALTER TABLE bil_invoices {add:customer_locality} {str:100}{addEnd}`,
	`ALTER TABLE bil_invoices {add:customer_region} {str:100}{addEnd}`,
	`ALTER TABLE bil_invoices {add:customer_address_country} {str:2}{addEnd}`,
}

// calculationDDL guarda en cada factura cómo se calcularon sus líneas (D-144): la forma de redondear
// y los decimales del precio. Las facturas que ya hay se calcularon como siempre: una vez al final y
// con 4 decimales. El precio de la línea pasa de (18,4) a (20,6): hasta 6 decimales, con las mismas
// 14 cifras enteras. En SQLite {dec} es texto y no cambia; en Oracle se puede ampliar la precisión y
// la escala de una columna NUMBER con datos.
func calculationDDL() map[string][]string {
	out := sqlrepo.RenderDDLAll(
		`ALTER TABLE bil_invoices {add:line_rounding} {str:20}{addEnd}`,
		`ALTER TABLE bil_invoices {add:price_decimals} {int}{addEnd}`,
		`UPDATE bil_invoices SET line_rounding = 'once', price_decimals = 4`)
	widen := map[string]string{
		"postgres":  `ALTER TABLE bil_invoice_lines ALTER COLUMN unit_price TYPE NUMERIC(20,6)`,
		"sqlserver": `ALTER TABLE bil_invoice_lines ALTER COLUMN unit_price DECIMAL(20,6) NOT NULL`,
		"mysql":     `ALTER TABLE bil_invoice_lines MODIFY COLUMN unit_price DECIMAL(20,6) NOT NULL`,
		"oracle":    `ALTER TABLE bil_invoice_lines MODIFY (unit_price NUMBER(20,6))`,
	}
	for d, stmt := range widen {
		out[d] = append(out[d], stmt)
	}
	return out
}

// uniqueSource impide que un documento esté en dos facturas: un albarán se factura una vez (la
// invariante de shipment_item_billing en Trench).
const uniqueSource = "ux_bil_invoice_sources_source"

// sourcesDDL lleva los orígenes de una factura a una tabla hija, uno por documento (D-146), y copia
// en ella el de las facturas que ya lo tenían antes de crear el índice único: si dos facturas
// tuvieran el mismo documento, la creación del índice lo dice y la migración no se da por hecha.
// Las columnas source_* de bil_invoices se quedan con el primer origen, para quien ya las lea.
var sourcesDDL = []string{
	`CREATE TABLE bil_invoice_sources (invoice_id {uuid} NOT NULL, line_no {int} NOT NULL, source_type {str:80} NOT NULL,
	source_id {str:80} NOT NULL, source_ref {str:40}, PRIMARY KEY (invoice_id, line_no), FOREIGN KEY (invoice_id) REFERENCES bil_invoices (id))`,
	`INSERT INTO bil_invoice_sources (invoice_id, line_no, source_type, source_id, source_ref)
	SELECT id, 1, source_type, source_id, source_ref FROM bil_invoices WHERE source_type IS NOT NULL`,
	`CREATE UNIQUE INDEX ` + uniqueSource + ` ON bil_invoice_sources (source_type, source_id)`,
}

// pendingDDL guarda los albaranes de los clientes con facturación periódica hasta el cierre (D-146).
var pendingDDL = []string{
	`CREATE TABLE bil_pending_deliveries (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, seller {uuid} NOT NULL, customer {uuid} NOT NULL,
	delivery_number {str:40} NOT NULL, order_number {str:40}, delivery_date {date} NOT NULL, ` + audit + `)`,
	`CREATE INDEX ix_bil_pending_deliveries ON bil_pending_deliveries (seller, delivery_date)`,
	`CREATE TABLE bil_pending_delivery_lines (pending_id {uuid} NOT NULL, line_no {int} NOT NULL, description {str:500} NOT NULL,
	quantity {dec:18,4} NOT NULL, unit_price {dec:20,6} NOT NULL, tax_code {str:5}, PRIMARY KEY (pending_id, line_no),
	FOREIGN KEY (pending_id) REFERENCES bil_pending_deliveries (id))`,
}

// errSourceTaken es un conflicto: otra factura ya tiene ese documento (dos cierres a la vez).
var errSourceTaken = fmt.Errorf("%w: billing: the document is already in another invoice", fw.ErrConflict)

// errNumberTaken es un conflicto: la emisión se reintenta y toma otro número.
var errNumberTaken = fmt.Errorf("%w: billing: the invoice number is already taken in its series", fw.ErrConflict)

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

// Migrations es el esquema versionado del contexto.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "series and invoices", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
		{Version: 3, Name: "source document of an invoice", Up: sqlrepo.RenderDDLAll(sourceDDL...)},
		{Version: 4, Name: "inbox of the Orders events", Up: map[string][]string{"sqlite": sqlite.InboxDDL(TableInbox), "postgres": postgres.InboxDDL(TableInbox),
			"sqlserver": sqlserver.InboxDDL(TableInbox), "oracle": oracle.InboxDDL(TableInbox), "mysql": mysql.InboxDDL(TableInbox)}},
		{Version: 5, Name: "decimal columns", Run: decimalColumnsV5.Run},
		{Version: 6, Name: "unique invoice number per series", Up: sqlrepo.RenderDDLAll(numberDDL...)},
		{Version: 7, Name: "last issue date of a series", Up: sqlrepo.RenderDDLAll(issueDateDDL...)},
		{Version: 8, Name: "fiscal addresses of an invoice", Up: sqlrepo.RenderDDLAll(addressDDL...)},
		{Version: 9, Name: "line calculation of an invoice: rounding and price decimals", Up: calculationDDL()},
		{Version: 10, Name: "sources of an invoice", Up: sqlrepo.RenderDDLAll(sourcesDDL...)},
		{Version: 11, Name: "delivery notes pending of the period close", Up: sqlrepo.RenderDDLAll(pendingDDL...)},
	}}
}

// Migrator devuelve el migrador del esquema del contexto.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lista las tablas del contexto, las hijas primero (orden de borrado).
var Tables = []string{"bil_pending_delivery_lines", "bil_pending_deliveries", "bil_invoice_sources", "bil_invoice_taxes", "bil_invoice_lines", "bil_invoices",
	"bil_series", TableOutbox, TableIntegrationOutbox, TableAuditLog, TableInbox}

// DropAll borra las tablas del contexto y su historial de migraciones (sólo para pruebas).
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
	return d
}

func address(r *sqlrepo.Row, who string) domain.Address {
	return domain.Address{Street: r.String(who + "_street"), PostalCode: r.String(who + "_postal_code"), Locality: r.String(who + "_locality"),
		Region: r.String(who + "_region"), Country: r.String(who + "_address_country")}
}

// SeriesMapping mapea Series a bil_series.
func SeriesMapping() sqlrepo.Mapping[domain.SeriesID, *domain.Series] {
	return sqlrepo.Mapping[domain.SeriesID, *domain.Series]{
		Table:   "bil_series",
		Columns: sqlrepo.WithAuditColumns("seller", "code", "series_year", "last_number", "last_issue_date", "corrective", "active"),
		Dehydrate: func(s *domain.Series) (sqlrepo.Values, error) {
			st := s.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"seller": st.Seller, "code": st.Code, "series_year": int64(st.Year), "last_number": st.Last,
				"last_issue_date": optDate(st.LastIssueDate), "corrective": st.Corrective, "active": st.Active}, s.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Series, error) {
			st := domain.SeriesState{Seller: domain.OrganizationID{UUID: r.UUID("seller")}, Code: r.String("code"), Year: int(r.Int64("series_year")),
				Last: r.Int64("last_number"), LastIssueDate: r.Date("last_issue_date"), Corrective: r.Bool("corrective"), Active: r.Bool("active"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteSeries(domain.SeriesID{UUID: r.UUID("id")}, st)
		},
	}
}

// InvoiceMapping mapea Invoice a bil_invoices, sus líneas y su desglose de impuestos.
func InvoiceMapping() sqlrepo.Mapping[domain.InvoiceID, *domain.Invoice] {
	return sqlrepo.Mapping[domain.InvoiceID, *domain.Invoice]{
		Table:    "bil_invoices",
		Decimals: decimalColumns.Of("bil_invoices"),
		Unique:   []sqlrepo.UniqueIndex{{Name: uniqueNumber, Columns: []string{"series_id", "invoice_number"}, Err: errNumberTaken}},
		Columns: sqlrepo.WithAuditColumns("seller", "customer", "kind", "status", "currency", "description", "operation_date", "due_date",
			"equivalence_surcharge", "corrects", "reason", "series_id", "invoice_number", "issue_date", "seller_nif", "seller_name", "seller_country",
			"customer_nif", "customer_name", "customer_country", "tax_country", "net", "tax_amount", "surcharge", "source_type", "source_id", "source_ref",
			"seller_street", "seller_postal_code", "seller_locality", "seller_region", "seller_address_country",
			"customer_street", "customer_postal_code", "customer_locality", "customer_region", "customer_address_country",
			"line_rounding", "price_decimals"),
		Dehydrate: func(i *domain.Invoice) (sqlrepo.Values, error) {
			s := i.State()
			var first domain.Source // las columnas de la cabecera guardan el primer origen
			if len(s.Sources) > 0 {
				first = s.Sources[0]
			}
			v := sqlrepo.Values{"seller": s.Seller, "customer": s.Customer, "kind": int64(s.Kind), "status": int64(s.Status), "currency": s.Currency.String(),
				"description": opt(s.Description), "operation_date": optDate(s.OperationDate), "due_date": optDate(s.DueDate),
				"equivalence_surcharge": s.EquivalenceSurcharge, "corrects": optUUID(s.Corrects.UUID), "reason": opt(s.Reason),
				"series_id": optUUID(s.Series.UUID), "invoice_number": opt(s.Number), "issue_date": optDate(s.IssueDate),
				"seller_nif": opt(s.SellerIdentity.NIF), "seller_name": opt(s.SellerIdentity.Name), "seller_country": opt(s.SellerIdentity.Country),
				"customer_nif": opt(s.CustomerIdentity.NIF), "customer_name": opt(s.CustomerIdentity.Name), "customer_country": opt(s.CustomerIdentity.Country),
				"tax_country": opt(s.Taxes.Country), "net": i.Net().Round(2), "tax_amount": nil, "surcharge": nil,
				"source_type": opt(first.Type), "source_id": opt(first.ID), "source_ref": opt(first.Ref),
				"line_rounding": string(s.Calculation.Rounding), "price_decimals": int64(s.Calculation.PriceDecimals)}
			if s.Status == domain.Issued {
				v["tax_amount"], v["surcharge"] = s.Taxes.Tax.Round(2), s.Taxes.Surcharge.Round(2)
			}
			for who, a := range map[string]domain.Address{"seller": s.SellerIdentity.Address, "customer": s.CustomerIdentity.Address} {
				v[who+"_street"], v[who+"_postal_code"], v[who+"_locality"] = opt(a.Street), opt(a.PostalCode), opt(a.Locality)
				v[who+"_region"], v[who+"_address_country"] = opt(a.Region), opt(a.Country)
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
				SellerIdentity: domain.Identity{NIF: r.String("seller_nif"), Name: r.String("seller_name"), Country: r.String("seller_country"),
					Address: address(r, "seller")},
				CustomerIdentity: domain.Identity{NIF: r.String("customer_nif"), Name: r.String("customer_name"), Country: r.String("customer_country"),
					Address: address(r, "customer")}}
			if !r.IsNull("line_rounding") { // nulo sólo en una fila anterior a la migración 9: el cálculo de siempre
				s.Calculation = domain.Calculation{Rounding: domain.LineRounding(r.String("line_rounding")), PriceDecimals: int(r.Int64("price_decimals"))}
			}
			for _, c := range children.Of("lines") {
				s.Lines = append(s.Lines, domain.Line{ID: domain.LineID{UUID: c.UUID("id")}, Description: c.String("description"),
					Quantity: c.Decimal("quantity"), UnitPrice: c.Decimal("unit_price"), Discount: c.Decimal("discount"), TaxCode: c.String("tax_code"),
					Treatment: c.String("treatment"), Net: c.Decimal("net")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(s.Lines, func(a, b domain.Line) int { return bytes.Compare(a.ID.Bytes(), b.ID.Bytes()) })
			srcs := slices.Clone(children.Of("sources"))
			slices.SortFunc(srcs, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
			for _, c := range srcs {
				s.Sources = append(s.Sources, domain.Source{Type: c.String("source_type"), ID: c.String("source_id"), Ref: c.String("source_ref")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
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
			Columns:  []string{"id", "description", "quantity", "unit_price", "discount", "tax_code", "treatment", "net"},
			Decimals: decimalColumns.Of("bil_invoice_lines"),
			Dehydrate: func(i *domain.Invoice) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range i.State().Lines {
					out = append(out, sqlrepo.Values{"id": l.ID, "description": l.Description, "quantity": l.Quantity,
						"unit_price": l.UnitPrice, "discount": optDecimal(l.Discount), "tax_code": opt(l.TaxCode), "treatment": opt(l.Treatment),
						"net": l.Net.Round(2)})
				}
				return out, nil
			},
		}, {
			Name: "taxes", Table: "bil_invoice_taxes", ForeignKey: "invoice_id", OrderBy: []string{"line_no"},
			Columns:  []string{"line_no", "tax_type", "tax_code", "treatment", "treatment_kind", "rate", "base", "amount", "surcharge_rate", "surcharge_amount"},
			Decimals: decimalColumns.Of("bil_invoice_taxes"),
			Dehydrate: func(i *domain.Invoice) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, l := range i.State().Taxes.Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "tax_type": opt(l.TaxType), "tax_code": opt(l.TaxCode),
						"treatment": opt(l.Treatment), "treatment_kind": l.TreatmentKind, "rate": l.Rate, "base": l.Base.Round(2),
						"amount": l.Amount.Round(2), "surcharge_rate": optDecimal(l.SurchargeRate), "surcharge_amount": optDecimal(l.SurchargeAmount)})
				}
				return out, nil
			},
		}, {
			Name: "sources", Table: "bil_invoice_sources", ForeignKey: "invoice_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "source_type", "source_id", "source_ref"},
			Unique:  []sqlrepo.UniqueIndex{{Name: uniqueSource, Columns: []string{"source_type", "source_id"}, Err: errSourceTaken}},
			Dehydrate: func(i *domain.Invoice) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, s := range i.State().Sources {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "source_type": s.Type, "source_id": s.ID, "source_ref": opt(s.Ref)})
				}
				return out, nil
			},
		}},
	}
}

// PendingDeliveryMapping mapea PendingDelivery a bil_pending_deliveries y sus líneas.
func PendingDeliveryMapping() sqlrepo.Mapping[domain.PendingDeliveryID, *domain.PendingDelivery] {
	return sqlrepo.Mapping[domain.PendingDeliveryID, *domain.PendingDelivery]{
		Table:   "bil_pending_deliveries",
		Columns: sqlrepo.WithAuditColumns("seller", "customer", "delivery_number", "order_number", "delivery_date"),
		Dehydrate: func(p *domain.PendingDelivery) (sqlrepo.Values, error) {
			s := p.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"seller": s.Seller, "customer": s.Customer, "delivery_number": s.Number,
				"order_number": opt(s.OrderNumber), "delivery_date": s.Date}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.PendingDelivery, error) {
			s := domain.PendingDeliveryState{Seller: domain.OrganizationID{UUID: r.UUID("seller")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Number: r.String("delivery_number"), OrderNumber: r.String("order_number"), Date: r.Date("delivery_date"), Audit: r.AuditStamp()}
			rows := slices.Clone(children.Of("lines"))
			slices.SortFunc(rows, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
			for _, c := range rows {
				s.Lines = append(s.Lines, domain.LineInput{Description: c.String("description"), Quantity: c.Decimal("quantity"),
					UnitPrice: c.Decimal("unit_price"), TaxCode: c.String("tax_code")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstitutePendingDelivery(domain.PendingDeliveryID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.PendingDelivery]{{
			Name: "lines", Table: "bil_pending_delivery_lines", ForeignKey: "pending_id", OrderBy: []string{"line_no"},
			Columns:  []string{"line_no", "description", "quantity", "unit_price", "tax_code"},
			Decimals: decimalColumns.Of("bil_pending_delivery_lines"),
			Dehydrate: func(p *domain.PendingDelivery) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, l := range p.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "description": l.Description, "quantity": l.Quantity,
						"unit_price": l.UnitPrice, "tax_code": opt(l.TaxCode)})
				}
				return out, nil
			},
		}},
	}
}

// PendingDeliveryRepositoryFactory construye el repositorio de albaranes pendientes.
func PendingDeliveryRepositoryFactory(b hotswap.Backend) (domain.PendingDeliveryRepository, error) {
	return repository(b, PendingDeliveryMapping())
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

// SeriesRepositoryFactory construye el repositorio de series.
func SeriesRepositoryFactory(b hotswap.Backend) (domain.SeriesRepository, error) {
	return repository(b, SeriesMapping())
}

// InvoiceRepositoryFactory construye el repositorio de facturas.
func InvoiceRepositoryFactory(b hotswap.Backend) (domain.InvoiceRepository, error) {
	return repository(b, InvoiceMapping())
}

// OutboxFactory construye el outbox de eventos de dominio; IntegrationOutboxFactory, el del Published Language.
func OutboxFactory(b hotswap.Backend) (application.OutboxStore, error) { return outbox(b, TableOutbox) }

// IntegrationOutboxFactory construye el outbox de integración.
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

// AuditLogFactory construye el registro de auditoría.
func AuditLogFactory(b hotswap.Backend) (application.AuditLog, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewAuditLog(db, TableAuditLog)
	case *memory.Store:
		return memory.NewAuditLog(db), nil
	}
	return nil, unsupported(b)
}

// FiscalTaxes adapta el TaxEngine de Fiscal al puerto Taxes de Billing (ACL).
type FiscalTaxes struct{ Engine fiscal.TaxEngine }

var _ domain.Taxes = FiscalTaxes{}

// Calculate implementa domain.Taxes.
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

// PartiesIdentities adapta el contrato TaxIdentities de Parties al puerto Identities de Billing (ACL).
type PartiesIdentities struct{ TaxIdentities parties.TaxIdentities }

var _ domain.Identities = PartiesIdentities{}

var _ domain.Blocks = PartiesIdentities{}

// Blocked implementa domain.Blocks; una party que Parties no conoce no está bloqueada.
func (p PartiesIdentities) Blocked(ctx context.Context, party domain.PartyID) (bool, error) {
	m, err := p.TaxIdentities.TaxIdentities(ctx, []string{party.String()})
	if err != nil {
		return false, err
	}
	return m[party.String()].Blocked, nil
}

// Identities implementa domain.Identities.
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
		id := domain.Identity{NIF: vocab.NormalizeDocumentNumber(ti.Number), Name: ti.Name, Country: ti.Country}
		if a := ti.Address; a != nil {
			id.Address = domain.Address{Street: a.Street, PostalCode: a.PostalCode, Locality: a.Locality, Region: a.Region, Country: a.Country}
		}
		out[domain.PartyID{UUID: u}] = id
	}
	return out, nil
}

// InboxFactory construye el inbox de los eventos que consume Billing.
func InboxFactory(b hotswap.Backend) (application.InboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewInbox(db, TableInbox)
	case *memory.Store:
		return memory.NewInbox(db), nil
	}
	return nil, unsupported(b)
}

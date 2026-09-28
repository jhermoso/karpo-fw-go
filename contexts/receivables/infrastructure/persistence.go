// Package infrastructure stores the Receivables context: SQL mappings, versioned schema of the five
// engines (with the inbox of the Billing events), hot-swap factories.
package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/receivables/domain"
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
const Context = "receivables"

// Technical tables of the context.
const (
	TableOutbox            = "receivables_outbox"
	TableIntegrationOutbox = "receivables_integration_outbox"
	TableAuditLog          = "receivables_audit_log"
	TableInbox             = "receivables_inbox"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE rec_terms (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, seller {uuid} NOT NULL, code {str:10} NOT NULL,
	description {str:100} NOT NULL, installments {int} NOT NULL, days_to_first {int} NOT NULL, days_between {int} NOT NULL, fixed_days {str:20},
	no_payment_from {int}, no_payment_to {int}, commercial_month {bool} NOT NULL, control_holidays {bool} NOT NULL, backward_days {int} NOT NULL,
	active {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_rec_terms_code ON rec_terms (seller, code)`,
	`CREATE TABLE rec_credit_profiles (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, seller {uuid} NOT NULL, customer {uuid} NOT NULL,
	terms_id {uuid}, limited {bool} NOT NULL, credit_limit {str:30}, blocked {bool} NOT NULL, ` + audit + `,
	FOREIGN KEY (terms_id) REFERENCES rec_terms (id))`,
	`CREATE UNIQUE INDEX ux_rec_credit_profiles ON rec_credit_profiles (seller, customer)`,
	`CREATE TABLE rec_receivables (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, seller {uuid} NOT NULL, customer {uuid} NOT NULL,
	invoice_number {str:40} NOT NULL, issued {date} NOT NULL, currency {str:3} NOT NULL, total {str:30} NOT NULL, open_amount {str:30} NOT NULL,
	settled {bool} NOT NULL, ` + audit + `)`,
	`CREATE INDEX ix_rec_receivables_customer ON rec_receivables (seller, customer, settled)`,
	`CREATE TABLE rec_installments (receivable_id {uuid} NOT NULL, installment_no {int} NOT NULL, due_date {date} NOT NULL, amount {str:30} NOT NULL,
	collected {str:30} NOT NULL, PRIMARY KEY (receivable_id, installment_no), FOREIGN KEY (receivable_id) REFERENCES rec_receivables (id))`,
	`CREATE INDEX ix_rec_installments_due ON rec_installments (due_date)`,
	`CREATE TABLE rec_collections (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, seller {uuid} NOT NULL, payer {uuid} NOT NULL,
	collected_on {date} NOT NULL, amount {str:30} NOT NULL, currency {str:3} NOT NULL, method {int} NOT NULL, reference {str:100},
	cancelled {bool} NOT NULL, ` + audit + `)`,
	`CREATE INDEX ix_rec_collections_payer ON rec_collections (seller, payer)`,
	`CREATE TABLE rec_allocations (id {uuid} NOT NULL PRIMARY KEY, collection_id {uuid} NOT NULL, receivable_id {uuid} NOT NULL,
	installment_no {int} NOT NULL, amount {str:30} NOT NULL, allocated_on {date} NOT NULL,
	FOREIGN KEY (collection_id) REFERENCES rec_collections (id), FOREIGN KEY (receivable_id) REFERENCES rec_receivables (id))`,
	`CREATE INDEX ix_rec_allocations_collection ON rec_allocations (collection_id)`,
	`CREATE INDEX ix_rec_allocations_receivable ON rec_allocations (receivable_id)`,
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
		{Version: 1, Name: "terms, credit profiles, receivables and collections", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes, audit log and inbox", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"rec_allocations", "rec_collections", "rec_installments", "rec_receivables", "rec_credit_profiles", "rec_terms",
	TableOutbox, TableIntegrationOutbox, TableAuditLog, TableInbox}

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

func optInt(n int) any {
	if n == 0 {
		return nil
	}
	return int64(n)
}

func joinDays(ds []int) string {
	parts := make([]string, len(ds))
	for i, d := range ds {
		parts[i] = strconv.Itoa(d)
	}
	return strings.Join(parts, ",")
}

func splitDays(s string) ([]int, error) {
	if s == "" {
		return nil, nil
	}
	var out []int
	for _, p := range strings.Split(s, ",") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// TermsMapping maps Terms to rec_terms.
func TermsMapping() sqlrepo.Mapping[domain.TermsID, *domain.Terms] {
	return sqlrepo.Mapping[domain.TermsID, *domain.Terms]{
		Table: "rec_terms",
		Columns: sqlrepo.WithAuditColumns("seller", "code", "description", "installments", "days_to_first", "days_between", "fixed_days",
			"no_payment_from", "no_payment_to", "commercial_month", "control_holidays", "backward_days", "active"),
		Dehydrate: func(t *domain.Terms) (sqlrepo.Values, error) {
			s := t.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"seller": s.Seller, "code": s.Code, "description": s.Description,
				"installments": int64(s.Installments), "days_to_first": int64(s.DaysToFirst), "days_between": int64(s.DaysBetween),
				"fixed_days": opt(joinDays(s.FixedDays)), "no_payment_from": optInt(int(s.NoPayment.From)), "no_payment_to": optInt(int(s.NoPayment.To)),
				"commercial_month": s.CommercialMonth, "control_holidays": s.ControlHolidays, "backward_days": int64(s.BackwardDays),
				"active": s.Active}, t.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Terms, error) {
			fixed, err := splitDays(r.String("fixed_days"))
			if err != nil {
				return nil, err
			}
			s := domain.TermsState{Seller: domain.OrganizationID{UUID: r.UUID("seller")}, Code: r.String("code"), Description: r.String("description"),
				Installments: int(r.Int64("installments")), DaysToFirst: int(r.Int64("days_to_first")), DaysBetween: int(r.Int64("days_between")),
				FixedDays: fixed, NoPayment: domain.MonthRange{From: time.Month(r.Int64("no_payment_from")), To: time.Month(r.Int64("no_payment_to"))},
				CommercialMonth: r.Bool("commercial_month"), ControlHolidays: r.Bool("control_holidays"), BackwardDays: int(r.Int64("backward_days")),
				Active: r.Bool("active"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteTerms(domain.TermsID{UUID: r.UUID("id")}, s)
		},
	}
}

// CreditMapping maps CreditProfile to rec_credit_profiles.
func CreditMapping() sqlrepo.Mapping[domain.CreditProfileID, *domain.CreditProfile] {
	return sqlrepo.Mapping[domain.CreditProfileID, *domain.CreditProfile]{
		Table:   "rec_credit_profiles",
		Columns: sqlrepo.WithAuditColumns("seller", "customer", "terms_id", "limited", "credit_limit", "blocked"),
		Dehydrate: func(p *domain.CreditProfile) (sqlrepo.Values, error) {
			s := p.State()
			v := sqlrepo.Values{"seller": s.Seller, "customer": s.Customer, "terms_id": nil, "limited": s.Limited, "credit_limit": nil, "blocked": s.Blocked}
			if !s.Terms.IsZero() {
				v["terms_id"] = s.Terms
			}
			if s.Limited {
				v["credit_limit"] = s.Limit.StringFixed(2)
			}
			return sqlrepo.AuditStampValues(v, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.CreditProfile, error) {
			s := domain.CreditProfileState{Seller: domain.OrganizationID{UUID: r.UUID("seller")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Terms: domain.TermsID{UUID: r.UUID("terms_id")}, Limited: r.Bool("limited"), Limit: r.Decimal("credit_limit"), Blocked: r.Bool("blocked"),
				Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCreditProfile(domain.CreditProfileID{UUID: r.UUID("id")}, s)
		},
	}
}

// ReceivableMapping maps Receivable to rec_receivables and its installments.
func ReceivableMapping() sqlrepo.Mapping[domain.ReceivableID, *domain.Receivable] {
	return sqlrepo.Mapping[domain.ReceivableID, *domain.Receivable]{
		Table:   "rec_receivables",
		Columns: sqlrepo.WithAuditColumns("seller", "customer", "invoice_number", "issued", "currency", "total", "open_amount", "settled"),
		Dehydrate: func(r *domain.Receivable) (sqlrepo.Values, error) {
			s := r.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"seller": s.Seller, "customer": s.Customer, "invoice_number": s.Number, "issued": s.Issued,
				"currency": s.Currency.String(), "total": s.Total.StringFixed(2), "open_amount": r.Open().StringFixed(2), "settled": r.Settled()},
				r.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Receivable, error) {
			cur, err := vocab.NewCurrencyCode(r.String("currency"))
			if err != nil {
				return nil, err
			}
			s := domain.ReceivableState{Seller: domain.OrganizationID{UUID: r.UUID("seller")}, Customer: domain.PartyID{UUID: r.UUID("customer")},
				Number: r.String("invoice_number"), Issued: r.Date("issued"), Currency: cur, Total: r.Decimal("total"), Audit: r.AuditStamp()}
			for _, c := range children.Of("installments") {
				s.Installments = append(s.Installments, domain.Installment{No: int(c.Int64("installment_no")), Due: c.Date("due_date"),
					Amount: c.Decimal("amount"), Collected: c.Decimal("collected")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(s.Installments, func(a, b domain.Installment) int { return a.No - b.No })
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteReceivable(domain.ReceivableID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Receivable]{{
			Name: "installments", Table: "rec_installments", ForeignKey: "receivable_id", OrderBy: []string{"installment_no"},
			Columns: []string{"installment_no", "due_date", "amount", "collected"},
			Dehydrate: func(r *domain.Receivable) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, i := range r.State().Installments {
					out = append(out, sqlrepo.Values{"installment_no": int64(i.No), "due_date": i.Due, "amount": i.Amount.StringFixed(2),
						"collected": i.Collected.StringFixed(2)})
				}
				return out, nil
			},
		}},
	}
}

// CollectionMapping maps Collection to rec_collections and its allocations.
func CollectionMapping() sqlrepo.Mapping[domain.CollectionID, *domain.Collection] {
	return sqlrepo.Mapping[domain.CollectionID, *domain.Collection]{
		Table:   "rec_collections",
		Columns: sqlrepo.WithAuditColumns("seller", "payer", "collected_on", "amount", "currency", "method", "reference", "cancelled"),
		Dehydrate: func(c *domain.Collection) (sqlrepo.Values, error) {
			s := c.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"seller": s.Seller, "payer": s.Payer, "collected_on": s.Date, "amount": s.Amount.StringFixed(2),
				"currency": s.Currency.String(), "method": int64(s.Method), "reference": opt(s.Reference), "cancelled": s.Cancelled}, c.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Collection, error) {
			cur, err := vocab.NewCurrencyCode(r.String("currency"))
			if err != nil {
				return nil, err
			}
			s := domain.CollectionState{Seller: domain.OrganizationID{UUID: r.UUID("seller")}, Payer: domain.PartyID{UUID: r.UUID("payer")},
				Date: r.Date("collected_on"), Amount: r.Decimal("amount"), Currency: cur, Method: domain.Method(r.Int64("method")),
				Reference: r.String("reference"), Cancelled: r.Bool("cancelled"), Audit: r.AuditStamp()}
			for _, c := range children.Of("allocations") {
				s.Allocations = append(s.Allocations, domain.Allocation{ID: domain.AllocationID{UUID: c.UUID("id")},
					Receivable: domain.ReceivableID{UUID: c.UUID("receivable_id")}, Installment: int(c.Int64("installment_no")),
					Amount: c.Decimal("amount"), On: c.Date("allocated_on")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(s.Allocations, func(a, b domain.Allocation) int { return bytes.Compare(a.ID.Bytes(), b.ID.Bytes()) })
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCollection(domain.CollectionID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Collection]{{
			Name: "allocations", Table: "rec_allocations", ForeignKey: "collection_id", OrderBy: []string{"id"},
			Columns: []string{"id", "receivable_id", "installment_no", "amount", "allocated_on"},
			Dehydrate: func(c *domain.Collection) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, a := range c.State().Allocations {
					out = append(out, sqlrepo.Values{"id": a.ID, "receivable_id": a.Receivable, "installment_no": int64(a.Installment),
						"amount": a.Amount.StringFixed(2), "allocated_on": a.On})
				}
				return out, nil
			},
		}},
	}
}

func unsupported(b hotswap.Backend) error {
	return fmt.Errorf("receivables: unsupported backend %T", b)
}

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
func TermsRepositoryFactory(b hotswap.Backend) (domain.TermsRepository, error) {
	return repository(b, TermsMapping())
}

func CreditRepositoryFactory(b hotswap.Backend) (domain.CreditProfileRepository, error) {
	return repository(b, CreditMapping())
}

func ReceivableRepositoryFactory(b hotswap.Backend) (domain.ReceivableRepository, error) {
	return repository(b, ReceivableMapping())
}

func CollectionRepositoryFactory(b hotswap.Backend) (domain.CollectionRepository, error) {
	return repository(b, CollectionMapping())
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

// InboxFactory builds the inbox of the Billing events.
func InboxFactory(b hotswap.Backend) (application.InboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewInbox(db, TableInbox)
	case *memory.Store:
		return memory.NewInbox(db), nil
	}
	return nil, unsupported(b)
}

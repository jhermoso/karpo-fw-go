// Package infrastructure stores the Payments context: SQL mappings, versioned schema of the five
// engines (with the inbox of the Payroll, Fiscal and Treasury events), hot-swap factories and the
// adapter to the Payroll Remittance port.
package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/payments/domain"
	payroll "github.com/jhermoso/karpo-fw-go/contexts/payroll/contracts"
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
const Context = "payments"

// Technical tables of the context.
const (
	TableOutbox            = "payments_outbox"
	TableIntegrationOutbox = "payments_integration_outbox"
	TableAuditLog          = "payments_audit_log"
	TableInbox             = "payments_inbox"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE pay_payables (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, payee {uuid}, authority {str:20},
	kind {str:20} NOT NULL, source_type {str:80} NOT NULL, source_id {str:80} NOT NULL, document {str:60} NOT NULL, description {str:200},
	issued {date} NOT NULL, due_on {date} NOT NULL, currency {str:3} NOT NULL, amount {str:30} NOT NULL, paid {str:30} NOT NULL,
	settled {bool} NOT NULL, cancelled {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_pay_payables_source ON pay_payables (company, source_type, source_id)`,
	`CREATE INDEX ix_pay_payables_due ON pay_payables (company, settled, cancelled, due_on)`,
	`CREATE INDEX ix_pay_payables_payee ON pay_payables (company, payee)`,
	`CREATE TABLE pay_payable_accounts (payable_id {uuid} NOT NULL, line_no {int} NOT NULL, iban {str:34} NOT NULL, amount {str:30} NOT NULL,
	PRIMARY KEY (payable_id, line_no), FOREIGN KEY (payable_id) REFERENCES pay_payables (id))`,
	`CREATE TABLE pay_payments (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, payee {uuid}, authority {str:20},
	paid_on {date} NOT NULL, amount {str:30} NOT NULL, currency {str:3} NOT NULL, method {int} NOT NULL, reference {str:100},
	cancelled {bool} NOT NULL, ` + audit + `)`,
	`CREATE INDEX ix_pay_payments_payee ON pay_payments (company, payee)`,
	`CREATE INDEX ix_pay_payments_reference ON pay_payments (company, reference)`,
	`CREATE TABLE pay_allocations (id {uuid} NOT NULL PRIMARY KEY, payment_id {uuid} NOT NULL, payable_id {uuid} NOT NULL, kind {int} NOT NULL,
	amount {str:30} NOT NULL, allocated_on {date} NOT NULL,
	FOREIGN KEY (payment_id) REFERENCES pay_payments (id), FOREIGN KEY (payable_id) REFERENCES pay_payables (id))`,
	`CREATE INDEX ix_pay_allocations_payment ON pay_allocations (payment_id)`,
	`CREATE INDEX ix_pay_allocations_payable ON pay_allocations (payable_id)`,
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
		{Version: 1, Name: "payables and payments", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes, audit log and inbox", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"pay_allocations", "pay_payments", "pay_payable_accounts", "pay_payables", TableOutbox, TableIntegrationOutbox, TableAuditLog, TableInbox}

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

// PayableMapping maps Payable to pay_payables and its accounts.
func PayableMapping() sqlrepo.Mapping[domain.PayableID, *domain.Payable] {
	return sqlrepo.Mapping[domain.PayableID, *domain.Payable]{
		Table: "pay_payables",
		Columns: sqlrepo.WithAuditColumns("company", "payee", "authority", "kind", "source_type", "source_id", "document", "description", "issued", "due_on",
			"currency", "amount", "paid", "settled", "cancelled"),
		Dehydrate: func(p *domain.Payable) (sqlrepo.Values, error) {
			s := p.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "payee": optUUID(s.Payee.UUID), "authority": opt(s.Authority),
				"kind": s.Kind.String(), "source_type": s.Source.Type, "source_id": s.Source.ID, "document": s.Document, "description": opt(s.Description),
				"issued": s.Issued, "due_on": s.Due, "currency": s.Currency.String(), "amount": s.Amount.StringFixed(2), "paid": s.Paid.StringFixed(2),
				"settled": p.Settled(), "cancelled": s.Cancelled}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Payable, error) {
			cur, err := vocab.NewCurrencyCode(r.String("currency"))
			if err != nil {
				return nil, err
			}
			kind, ok := domain.ParseKind(r.String("kind"))
			if !ok {
				return nil, fmt.Errorf("payments: unknown payable kind %q", r.String("kind"))
			}
			s := domain.PayableState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Payee: domain.PartyID{UUID: r.UUID("payee")},
				Authority: r.String("authority"), Kind: kind, Source: domain.Source{Type: r.String("source_type"), ID: r.String("source_id")},
				Document: r.String("document"), Description: r.String("description"), Issued: r.Date("issued"), Due: r.Date("due_on"), Currency: cur,
				Amount: r.Decimal("amount"), Paid: r.Decimal("paid"), Cancelled: r.Bool("cancelled"), Audit: r.AuditStamp()}
			type line struct {
				no int64
				to domain.PayTo
			}
			var ls []line
			for _, c := range children.Of("accounts") {
				iban, err := vocab.NewIBAN(c.String("iban"))
				if err != nil {
					return nil, err
				}
				ls = append(ls, line{c.Int64("line_no"), domain.PayTo{IBAN: iban, Amount: c.Decimal("amount")}})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(ls, func(a, b line) int { return int(a.no - b.no) })
			for _, l := range ls {
				s.PayTo = append(s.PayTo, l.to)
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstitutePayable(domain.PayableID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Payable]{{
			Name: "accounts", Table: "pay_payable_accounts", ForeignKey: "payable_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "iban", "amount"},
			Dehydrate: func(p *domain.Payable) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, t := range p.State().PayTo {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "iban": t.IBAN.String(), "amount": t.Amount.StringFixed(2)})
				}
				return out, nil
			},
		}},
	}
}

// PaymentMapping maps Payment to pay_payments and its allocations.
func PaymentMapping() sqlrepo.Mapping[domain.PaymentID, *domain.Payment] {
	return sqlrepo.Mapping[domain.PaymentID, *domain.Payment]{
		Table:   "pay_payments",
		Columns: sqlrepo.WithAuditColumns("company", "payee", "authority", "paid_on", "amount", "currency", "method", "reference", "cancelled"),
		Dehydrate: func(p *domain.Payment) (sqlrepo.Values, error) {
			s := p.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "payee": optUUID(s.Payee.UUID), "authority": opt(s.Authority),
				"paid_on": s.Date, "amount": s.Amount.StringFixed(2), "currency": s.Currency.String(), "method": int64(s.Method), "reference": opt(s.Reference),
				"cancelled": s.Cancelled}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Payment, error) {
			cur, err := vocab.NewCurrencyCode(r.String("currency"))
			if err != nil {
				return nil, err
			}
			s := domain.PaymentState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Payee: domain.PartyID{UUID: r.UUID("payee")},
				Authority: r.String("authority"), Date: r.Date("paid_on"), Amount: r.Decimal("amount"), Currency: cur, Method: domain.Method(r.Int64("method")),
				Reference: r.String("reference"), Cancelled: r.Bool("cancelled"), Audit: r.AuditStamp()}
			for _, c := range children.Of("allocations") {
				s.Allocations = append(s.Allocations, domain.Allocation{ID: domain.AllocationID{UUID: c.UUID("id")},
					Payable: domain.PayableID{UUID: c.UUID("payable_id")}, Kind: domain.Kind(c.Int64("kind")), Amount: c.Decimal("amount"),
					On: c.Date("allocated_on")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(s.Allocations, func(a, b domain.Allocation) int { return bytes.Compare(a.ID.Bytes(), b.ID.Bytes()) })
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstitutePayment(domain.PaymentID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Payment]{{
			Name: "allocations", Table: "pay_allocations", ForeignKey: "payment_id", OrderBy: []string{"id"},
			Columns: []string{"id", "payable_id", "kind", "amount", "allocated_on"},
			Dehydrate: func(p *domain.Payment) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, a := range p.State().Allocations {
					out = append(out, sqlrepo.Values{"id": a.ID, "payable_id": a.Payable, "kind": int64(a.Kind), "amount": a.Amount.StringFixed(2),
						"allocated_on": a.On})
				}
				return out, nil
			},
		}},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("payments: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// PayableRepositoryFactory builds the payable repository.
func PayableRepositoryFactory(b hotswap.Backend) (domain.PayableRepository, error) {
	return repository(b, PayableMapping())
}

// PaymentRepositoryFactory builds the payment repository.
func PaymentRepositoryFactory(b hotswap.Backend) (domain.PaymentRepository, error) {
	return repository(b, PaymentMapping())
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

// InboxFactory builds the inbox of the events Payments consumes.
func InboxFactory(b hotswap.Backend) (application.InboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewInbox(db, TableInbox)
	case *memory.Store:
		return memory.NewInbox(db), nil
	}
	return nil, unsupported(b)
}

// PayrollNetPay adapts the Payroll Remittance contract to the Payments port (ACL).
type PayrollNetPay struct{ Remittance payroll.Remittance }

var _ domain.NetPaySplits = PayrollNetPay{}

// NetPayments implements domain.NetPaySplits.
func (p PayrollNetPay) NetPayments(ctx context.Context, payslipIDs []string) (map[string][]domain.PayTo, error) {
	in, err := p.Remittance.NetPayments(ctx, payslipIDs)
	if err != nil {
		return nil, err
	}
	out := map[string][]domain.PayTo{}
	for id, refs := range in {
		for _, r := range refs {
			iban, err1 := vocab.NewIBAN(r.IBAN)
			amount, err2 := vocab.ParseDecimal(r.Amount)
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("payments: invalid net pay split from Payroll: %+v", r)
			}
			out[id] = append(out[id], domain.PayTo{IBAN: iban, Amount: amount})
		}
	}
	return out, nil
}

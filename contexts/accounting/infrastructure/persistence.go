// Package infrastructure stores the Accounting context: SQL mappings, versioned schema of the five
// engines (with the inbox of the events it posts), hot-swap factories. No chart is seeded: the C#
// had none (the Sage import loads each company's).
package infrastructure

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/accounting/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
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
const Context = "accounting"

// Technical tables of the context.
const (
	TableOutbox            = "accounting_outbox"
	TableIntegrationOutbox = "accounting_integration_outbox"
	TableAuditLog          = "accounting_audit_log"
	TableInbox             = "accounting_inbox"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE acc_accounts (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, code {str:12} NOT NULL,
	name {str:200} NOT NULL, postable {bool} NOT NULL, active {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_acc_accounts_code ON acc_accounts (company, code)`,
	`CREATE TABLE acc_ledgers (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, start_month {int} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_acc_ledgers_company ON acc_ledgers (company)`,
	`CREATE TABLE acc_ledger_roles (ledger_id {uuid} NOT NULL, posting_role {str:40} NOT NULL, account_code {str:12} NOT NULL,
	PRIMARY KEY (ledger_id, posting_role), FOREIGN KEY (ledger_id) REFERENCES acc_ledgers (id))`,
	`CREATE TABLE acc_ledger_taxes (ledger_id {uuid} NOT NULL, tax_code {str:5} NOT NULL, account_code {str:12} NOT NULL,
	PRIMARY KEY (ledger_id, tax_code), FOREIGN KEY (ledger_id) REFERENCES acc_ledgers (id))`,
	`CREATE TABLE acc_closed_periods (ledger_id {uuid} NOT NULL, fiscal_year {int} NOT NULL, fiscal_period {int} NOT NULL,
	PRIMARY KEY (ledger_id, fiscal_year, fiscal_period), FOREIGN KEY (ledger_id) REFERENCES acc_ledgers (id))`,
	`CREATE TABLE acc_entries (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, entry_number {bigint} NOT NULL,
	fiscal_year {int} NOT NULL, fiscal_period {int} NOT NULL, entry_date {date} NOT NULL, description {str:300} NOT NULL,
	source_type {str:80} NOT NULL, source_id {str:80} NOT NULL, source_key {str:120}, reverses {uuid}, reversed_by {uuid}, reversed {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_acc_entries_number ON acc_entries (company, fiscal_year, entry_number)`,
	`CREATE UNIQUE INDEX ux_acc_entries_source ON acc_entries (company, source_type, source_id)`,
	`CREATE INDEX ix_acc_entries_date ON acc_entries (company, entry_date)`,
	`CREATE TABLE acc_entry_lines (entry_id {uuid} NOT NULL, line_no {int} NOT NULL, account_code {str:12} NOT NULL, party {uuid},
	debit {str:30} NOT NULL, credit {str:30} NOT NULL, description {str:200}, PRIMARY KEY (entry_id, line_no),
	FOREIGN KEY (entry_id) REFERENCES acc_entries (id))`,
	`CREATE INDEX ix_acc_entry_lines_account ON acc_entry_lines (account_code)`,
	`CREATE TABLE acc_counters (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, fiscal_year {int} NOT NULL,
	last_number {bigint} NOT NULL)`,
	`CREATE UNIQUE INDEX ux_acc_counters ON acc_counters (company, fiscal_year)`,
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
		{Version: 1, Name: "chart of accounts, ledgers and journal", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes, audit log and inbox", Up: technical},
		{Version: 3, Name: "facts that wait to be posted", Up: sqlrepo.RenderDDLAll(parkedDDL...)},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"acc_parked_facts", "acc_entry_lines", "acc_entries", "acc_counters", "acc_closed_periods", "acc_ledger_taxes", "acc_ledger_roles", "acc_ledgers",
	"acc_accounts", TableOutbox, TableIntegrationOutbox, TableAuditLog, TableInbox}

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

// AccountMapping maps Account to acc_accounts.
func AccountMapping() sqlrepo.Mapping[domain.AccountID, *domain.Account] {
	return sqlrepo.Mapping[domain.AccountID, *domain.Account]{
		Table:   "acc_accounts",
		Columns: sqlrepo.WithAuditColumns("company", "code", "name", "postable", "active"),
		Dehydrate: func(a *domain.Account) (sqlrepo.Values, error) {
			s := a.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "code": s.Code, "name": s.Name, "postable": s.Postable, "active": s.Active},
				a.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Account, error) {
			s := domain.AccountState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("code"), Name: r.String("name"),
				Postable: r.Bool("postable"), Active: r.Bool("active"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteAccount(domain.AccountID{UUID: r.UUID("id")}, s)
		},
	}
}

// LedgerMapping maps Ledger to acc_ledgers, its roles, tax accounts and closed periods.
func LedgerMapping() sqlrepo.Mapping[domain.LedgerID, *domain.Ledger] {
	return sqlrepo.Mapping[domain.LedgerID, *domain.Ledger]{
		Table:   "acc_ledgers",
		Columns: sqlrepo.WithAuditColumns("company", "start_month"),
		Dehydrate: func(l *domain.Ledger) (sqlrepo.Values, error) {
			s := l.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "start_month": int64(s.StartMonth)}, l.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Ledger, error) {
			s := domain.LedgerState{Company: domain.OrganizationID{UUID: r.UUID("company")}, StartMonth: int(r.Int64("start_month")),
				Accounts: map[domain.Role]string{}, TaxCodes: map[string]string{}, Audit: r.AuditStamp()}
			for _, c := range children.Of("roles") {
				s.Accounts[domain.Role(c.String("posting_role"))] = c.String("account_code")
			}
			for _, c := range children.Of("taxes") {
				s.TaxCodes[c.String("tax_code")] = c.String("account_code")
			}
			for _, c := range children.Of("closed") {
				s.Closed = append(s.Closed, domain.Period{Year: int(c.Int64("fiscal_year")), Month: int(c.Int64("fiscal_period"))})
			}
			slices.SortFunc(s.Closed, func(a, b domain.Period) int { return (a.Year*100 + a.Month) - (b.Year*100 + b.Month) })
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteLedger(domain.LedgerID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Ledger]{{
			Name: "roles", Table: "acc_ledger_roles", ForeignKey: "ledger_id", OrderBy: []string{"posting_role"},
			Columns: []string{"posting_role", "account_code"},
			Dehydrate: func(l *domain.Ledger) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for r, c := range l.State().Accounts {
					out = append(out, sqlrepo.Values{"posting_role": string(r), "account_code": c})
				}
				return out, nil
			},
		}, {
			Name: "taxes", Table: "acc_ledger_taxes", ForeignKey: "ledger_id", OrderBy: []string{"tax_code"},
			Columns: []string{"tax_code", "account_code"},
			Dehydrate: func(l *domain.Ledger) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for t, c := range l.State().TaxCodes {
					out = append(out, sqlrepo.Values{"tax_code": t, "account_code": c})
				}
				return out, nil
			},
		}, {
			Name: "closed", Table: "acc_closed_periods", ForeignKey: "ledger_id", OrderBy: []string{"fiscal_year", "fiscal_period"},
			Columns: []string{"fiscal_year", "fiscal_period"},
			Dehydrate: func(l *domain.Ledger) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, p := range l.State().Closed {
					out = append(out, sqlrepo.Values{"fiscal_year": int64(p.Year), "fiscal_period": int64(p.Month)})
				}
				return out, nil
			},
		}},
	}
}

// EntryMapping maps Entry to acc_entries and its lines.
func EntryMapping() sqlrepo.Mapping[domain.EntryID, *domain.Entry] {
	return sqlrepo.Mapping[domain.EntryID, *domain.Entry]{
		Table: "acc_entries",
		Columns: sqlrepo.WithAuditColumns("company", "entry_number", "fiscal_year", "fiscal_period", "entry_date", "description", "source_type",
			"source_id", "source_key", "reverses", "reversed_by", "reversed"),
		Dehydrate: func(e *domain.Entry) (sqlrepo.Values, error) {
			s := e.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "entry_number": s.Number, "fiscal_year": int64(s.Year),
				"fiscal_period": int64(s.Month), "entry_date": s.Date, "description": s.Description, "source_type": s.Source.Type, "source_id": s.Source.ID,
				"source_key": opt(s.Source.Key), "reverses": optUUID(s.Reverses.UUID), "reversed_by": optUUID(s.ReversedBy.UUID),
				"reversed": !s.ReversedBy.IsZero()}, e.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Entry, error) {
			s := domain.EntryState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Number: r.Int64("entry_number"), Year: int(r.Int64("fiscal_year")),
				Month: int(r.Int64("fiscal_period")), Date: r.Date("entry_date"), Description: r.String("description"),
				Source:   domain.Source{Type: r.String("source_type"), ID: r.String("source_id"), Key: r.String("source_key")},
				Reverses: domain.EntryID{UUID: r.UUID("reverses")}, ReversedBy: domain.EntryID{UUID: r.UUID("reversed_by")}, Audit: r.AuditStamp()}
			type line struct {
				no int64
				l  domain.Line
			}
			var ls []line
			for _, c := range children.Of("lines") {
				ls = append(ls, line{c.Int64("line_no"), domain.Line{Account: c.String("account_code"), Party: domain.PartyID{UUID: c.UUID("party")},
					Debit: c.Decimal("debit"), Credit: c.Decimal("credit"), Description: c.String("description")}})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(ls, func(a, b line) int { return int(a.no - b.no) })
			for _, x := range ls {
				s.Lines = append(s.Lines, x.l)
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteEntry(domain.EntryID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Entry]{{
			Name: "lines", Table: "acc_entry_lines", ForeignKey: "entry_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "account_code", "party", "debit", "credit", "description"},
			Dehydrate: func(e *domain.Entry) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, l := range e.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "account_code": l.Account, "party": optUUID(l.Party.UUID),
						"debit": l.Debit.StringFixed(2), "credit": l.Credit.StringFixed(2), "description": opt(strings.TrimSpace(l.Description))})
				}
				return out, nil
			},
		}},
	}
}

// CounterMapping maps Counter to acc_counters.
func CounterMapping() sqlrepo.Mapping[domain.CounterID, *domain.Counter] {
	return sqlrepo.Mapping[domain.CounterID, *domain.Counter]{
		Table:   "acc_counters",
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

func unsupported(b hotswap.Backend) error { return fmt.Errorf("accounting: unsupported backend %T", b) }

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
func AccountRepositoryFactory(b hotswap.Backend) (domain.AccountRepository, error) {
	return repository(b, AccountMapping())
}

func LedgerRepositoryFactory(b hotswap.Backend) (domain.LedgerRepository, error) {
	return repository(b, LedgerMapping())
}

func EntryRepositoryFactory(b hotswap.Backend) (domain.EntryRepository, error) {
	return repository(b, EntryMapping())
}

func CounterRepositoryFactory(b hotswap.Backend) (domain.CounterRepository, error) {
	return repository(b, CounterMapping())
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

// InboxFactory builds the inbox of the events Accounting posts.
func InboxFactory(b hotswap.Backend) (application.InboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewInbox(db, TableInbox)
	case *memory.Store:
		return memory.NewInbox(db), nil
	}
	return nil, unsupported(b)
}

var parkedDDL = []string{
	`CREATE TABLE acc_parked_facts (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid}, event_type {str:100} NOT NULL,
	envelope_id {str:64} NOT NULL, source_name {str:50}, subject_id {str:100}, occurred_at {ts}, correlation_id {str:100}, causation_id {str:100},
	payload {text} NOT NULL, code {str:100}, reason {str:500}, received_at {ts} NOT NULL, attempts {int} NOT NULL, status {str:20} NOT NULL,
	resolved_at {ts}, note {str:500}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_acc_parked_envelope ON acc_parked_facts (envelope_id)`,
	`CREATE INDEX ix_acc_parked_company ON acc_parked_facts (company, status)`,
	`CREATE INDEX ix_acc_parked_subject ON acc_parked_facts (subject_id, status)`,
}

// ParkedMapping maps ParkedFact to acc_parked_facts.
func ParkedMapping() sqlrepo.Mapping[domain.ParkedID, *domain.ParkedFact] {
	text := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	moment := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t
	}
	at := func(r *sqlrepo.Row, col string) time.Time {
		if t := r.NullTime(col); t != nil {
			return t.UTC()
		}
		return time.Time{}
	}
	return sqlrepo.Mapping[domain.ParkedID, *domain.ParkedFact]{
		Table: "acc_parked_facts",
		Columns: sqlrepo.WithAuditColumns("company", "event_type", "envelope_id", "source_name", "subject_id", "occurred_at", "correlation_id",
			"causation_id", "payload", "code", "reason", "received_at", "attempts", "status", "resolved_at", "note"),
		Dehydrate: func(p *domain.ParkedFact) (sqlrepo.Values, error) {
			s := p.State()
			var company any
			if !s.Company.IsZero() {
				company = s.Company
			}
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": company, "event_type": s.EventType, "envelope_id": s.EnvelopeID,
				"source_name": text(s.Source), "subject_id": text(s.Subject), "occurred_at": moment(s.OccurredAt), "correlation_id": text(s.CorrelationID),
				"causation_id": text(s.CausationID), "payload": s.Data, "code": text(s.Code), "reason": text(s.Reason), "received_at": s.ReceivedAt,
				"attempts": int64(s.Attempts), "status": string(s.Status), "resolved_at": moment(s.ResolvedAt), "note": text(s.Note)}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.ParkedFact, error) {
			s := domain.ParkedState{Company: domain.OrganizationID{UUID: r.UUID("company")}, EventType: r.String("event_type"), EnvelopeID: r.String("envelope_id"),
				Source: r.String("source_name"), Subject: r.String("subject_id"), OccurredAt: at(r, "occurred_at"), CorrelationID: r.String("correlation_id"),
				CausationID: r.String("causation_id"), Data: r.String("payload"), Code: r.String("code"), Reason: r.String("reason"),
				ReceivedAt: r.Time("received_at").UTC(), Attempts: int(r.Int64("attempts")), Status: domain.ParkedStatus(r.String("status")),
				ResolvedAt: at(r, "resolved_at"), Note: r.String("note"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteParked(domain.ParkedID{UUID: r.UUID("id")}, s)
		},
	}
}

// ParkedRepositoryFactory builds the repository of the facts that wait.
func ParkedRepositoryFactory(b hotswap.Backend) (domain.ParkedRepository, error) {
	return repository(b, ParkedMapping())
}

// Package infrastructure stores the Financial context: SQL mapping, versioned schema of the five
// engines and hot-swap factories.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/financial/domain"
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
const Context = "financial"

// Technical tables of the context.
const (
	TableOutbox            = "financial_outbox"
	TableIntegrationOutbox = "financial_integration_outbox"
	TableAuditLog          = "financial_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

// Text columns that take accents are four times the characters they hold: Oracle counts bytes.
var schemaDDL = []string{
	`CREATE TABLE fin_accounts (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, account_number {str:34} NOT NULL,
	virtual_account {bool} NOT NULL, bic {str:11}, currency {str:3} NOT NULL, account_name {str:800}, product {uuid}, status {str:20} NOT NULL,
	demo {bool} NOT NULL, opened_on {date} NOT NULL, closed_on {date}, reason {str:800}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_fin_accounts_number ON fin_accounts (company, account_number)`,
	`CREATE INDEX ix_fin_accounts_status ON fin_accounts (company, status)`,
	`CREATE TABLE fin_account_holders (account_id {uuid} NOT NULL, line_no {int} NOT NULL, party {uuid} NOT NULL, role_name {str:20} NOT NULL,
	from_day {date} NOT NULL, thru_day {date}, primary_holder {bool} NOT NULL, current_holder {bool} NOT NULL,
	PRIMARY KEY (account_id, line_no), FOREIGN KEY (account_id) REFERENCES fin_accounts (id))`,
	`CREATE INDEX ix_fin_account_holders_party ON fin_account_holders (party, current_holder)`,
	`CREATE TABLE fin_account_uses (account_id {uuid} NOT NULL, line_no {int} NOT NULL, use_code {str:30} NOT NULL, from_day {date} NOT NULL,
	thru_day {date}, current_use {bool} NOT NULL, PRIMARY KEY (account_id, line_no), FOREIGN KEY (account_id) REFERENCES fin_accounts (id))`,
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
		{Version: 1, Name: "customer accounts, their holders and uses", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
		{Version: 3, Name: "products, agreements and the agreement of an account", Up: sqlrepo.RenderDDLAll(catalogDDL...)},
	}}
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"fin_account_uses", "fin_account_holders", "fin_accounts", "fin_agreements", "fin_products", TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

// Engines do not agree on the order of the children: they are sorted here.
func byNo(rows []*sqlrepo.Row) []*sqlrepo.Row {
	out := slices.Clone(rows)
	slices.SortFunc(out, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
	return out
}

// AccountMapping maps Account to fin_accounts, its holders and its uses.
func AccountMapping() sqlrepo.Mapping[domain.AccountID, *domain.Account] {
	return sqlrepo.Mapping[domain.AccountID, *domain.Account]{
		Table: "fin_accounts",
		Columns: sqlrepo.WithAuditColumns("company", "account_number", "virtual_account", "bic", "currency", "account_name", "product", "agreement", "status",
			"demo", "opened_on", "closed_on", "reason"),
		Dehydrate: func(a *domain.Account) (sqlrepo.Values, error) {
			s := a.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "account_number": s.Number, "virtual_account": s.Virtual, "bic": opt(s.BIC),
				"currency": s.Currency.String(), "account_name": opt(s.Name), "product": optUUID(s.Product), "agreement": optUUID(s.Agreement), "status": string(s.Status), "demo": s.Demo,
				"opened_on": s.Opened, "closed_on": optDate(s.Closed), "reason": opt(s.Reason)}, a.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Account, error) {
			s := domain.AccountState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Number: r.String("account_number"), Virtual: r.Bool("virtual_account"),
				BIC: r.String("bic"), Name: r.String("account_name"), Product: r.UUID("product"), Agreement: r.UUID("agreement"), Status: domain.Status(r.String("status")), Demo: r.Bool("demo"),
				Opened: r.Date("opened_on"), Closed: r.Date("closed_on"), Reason: r.String("reason"), Audit: r.AuditStamp()}
			var err error
			if s.Currency, err = vocab.NewCurrencyCode(r.String("currency")); err != nil {
				return nil, err
			}
			if !s.Virtual {
				if s.IBAN, err = vocab.NewIBAN(s.Number); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("holders")) {
				s.Holders = append(s.Holders, domain.Holder{Party: domain.PartyID{UUID: c.UUID("party")}, Role: c.String("role_name"), From: c.Date("from_day"),
					Thru: c.Date("thru_day"), Primary: c.Bool("primary_holder")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("uses")) {
				s.Uses = append(s.Uses, domain.Use{Code: c.String("use_code"), From: c.Date("from_day"), Thru: c.Date("thru_day")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteAccount(domain.AccountID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Account]{{
			Name: "holders", Table: "fin_account_holders", ForeignKey: "account_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "party", "role_name", "from_day", "thru_day", "primary_holder", "current_holder"},
			Dehydrate: func(a *domain.Account) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, h := range a.State().Holders {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "party": h.Party, "role_name": h.Role, "from_day": h.From, "thru_day": optDate(h.Thru),
						"primary_holder": h.Primary, "current_holder": h.Current()})
				}
				return out, nil
			},
		}, {
			Name: "uses", Table: "fin_account_uses", ForeignKey: "account_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "use_code", "from_day", "thru_day", "current_use"},
			Dehydrate: func(a *domain.Account) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, u := range a.State().Uses {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "use_code": u.Code, "from_day": u.From, "thru_day": optDate(u.Thru),
						"current_use": u.Thru.IsZero()})
				}
				return out, nil
			},
		}},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("financial: unsupported backend %T", b) }

// AccountRepositoryFactory builds the account repository.
func AccountRepositoryFactory(b hotswap.Backend) (domain.AccountRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, AccountMapping())
	case *memory.Store:
		return memory.NewRepository[domain.AccountID, *domain.Account](db), nil
	}
	return nil, unsupported(b)
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

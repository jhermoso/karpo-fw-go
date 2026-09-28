// Package infrastructure stores the Treasury context: SQL mappings, versioned schema of the five
// engines, hot-swap factories and the adapters to Receivables and Parties.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	parties "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	receivables "github.com/jhermoso/karpo-fw-go/contexts/receivables/contracts"
	"github.com/jhermoso/karpo-fw-go/contexts/treasury/domain"
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
const Context = "treasury"

// Technical tables of the context.
const (
	TableOutbox            = "treasury_outbox"
	TableIntegrationOutbox = "treasury_integration_outbox"
	TableAuditLog          = "treasury_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE trs_accounts (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, owner {uuid} NOT NULL, iban {str:34} NOT NULL,
	bic {str:11}, alias {str:100} NOT NULL, currency {str:3} NOT NULL, for_collections {bool} NOT NULL, for_payments {bool} NOT NULL,
	creditor_suffix {str:3} NOT NULL, opened {date} NOT NULL, closed {date}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_trs_accounts_iban ON trs_accounts (iban)`,
	`CREATE TABLE trs_mandates (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, creditor {uuid} NOT NULL, debtor {uuid} NOT NULL,
	iban {str:34} NOT NULL, reference {str:35} NOT NULL, scheme {int} NOT NULL, signed {date} NOT NULL, last_used {date}, revoked {date}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_trs_mandates_reference ON trs_mandates (creditor, reference)`,
	`CREATE INDEX ix_trs_mandates_debtor ON trs_mandates (debtor)`,
	`CREATE TABLE trs_remittances (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, creditor {uuid} NOT NULL, account_id {uuid} NOT NULL,
	scheme {int} NOT NULL, collection_date {date} NOT NULL, status {int} NOT NULL, generated_at {ts}, creditor_name {str:200},
	creditor_identifier {str:35}, creditor_iban {str:34}, creditor_bic {str:11}, settled {date}, ` + audit + `,
	FOREIGN KEY (account_id) REFERENCES trs_accounts (id))`,
	`CREATE INDEX ix_trs_remittances_creditor ON trs_remittances (creditor, status)`,
	`CREATE TABLE trs_remittance_items (remittance_id {uuid} NOT NULL, end_to_end {str:35} NOT NULL, invoice {uuid} NOT NULL,
	invoice_number {str:40} NOT NULL, installment_no {int} NOT NULL, debtor {uuid} NOT NULL, debtor_name {str:200}, mandate_id {uuid} NOT NULL,
	mandate_reference {str:35} NOT NULL, mandate_signed {date} NOT NULL, iban {str:34} NOT NULL, amount {str:30} NOT NULL, sequence_type {str:4},
	returned {date}, return_reason {str:4}, PRIMARY KEY (remittance_id, end_to_end),
	FOREIGN KEY (remittance_id) REFERENCES trs_remittances (id), FOREIGN KEY (mandate_id) REFERENCES trs_mandates (id))`,
	`CREATE INDEX ix_trs_remittance_items_invoice ON trs_remittance_items (invoice)`,
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
		{Version: 1, Name: "accounts, mandates and remittances", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"trs_remittance_items", "trs_remittances", "trs_mandates", "trs_accounts", TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

func optDate(d vocab.Date) any {
	if d.IsZero() {
		return nil
	}
	return d
}

func ibanOf(r *sqlrepo.Row, col string) (vocab.IBAN, error) {
	if s := r.String(col); s != "" {
		return vocab.NewIBAN(s)
	}
	return vocab.IBAN{}, nil
}

// AccountMapping maps Account to trs_accounts.
func AccountMapping() sqlrepo.Mapping[domain.AccountID, *domain.Account] {
	return sqlrepo.Mapping[domain.AccountID, *domain.Account]{
		Table:   "trs_accounts",
		Columns: sqlrepo.WithAuditColumns("owner", "iban", "bic", "alias", "currency", "for_collections", "for_payments", "creditor_suffix", "opened", "closed"),
		Dehydrate: func(a *domain.Account) (sqlrepo.Values, error) {
			s := a.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"owner": s.Owner, "iban": s.IBAN.String(), "bic": opt(s.BIC), "alias": s.Alias,
				"currency": s.Currency.String(), "for_collections": s.Collections, "for_payments": s.Payments, "creditor_suffix": s.CreditorSuffix,
				"opened": s.Opened, "closed": optDate(s.Closed)}, a.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Account, error) {
			iban, err := ibanOf(r, "iban")
			if err != nil {
				return nil, err
			}
			cur, err := vocab.NewCurrencyCode(r.String("currency"))
			if err != nil {
				return nil, err
			}
			s := domain.AccountState{Owner: domain.OrganizationID{UUID: r.UUID("owner")}, IBAN: iban, BIC: r.String("bic"), Alias: r.String("alias"),
				Currency: cur, Collections: r.Bool("for_collections"), Payments: r.Bool("for_payments"), CreditorSuffix: r.String("creditor_suffix"),
				Opened: r.Date("opened"), Closed: r.Date("closed"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteAccount(domain.AccountID{UUID: r.UUID("id")}, s)
		},
	}
}

// MandateMapping maps Mandate to trs_mandates.
func MandateMapping() sqlrepo.Mapping[domain.MandateID, *domain.Mandate] {
	return sqlrepo.Mapping[domain.MandateID, *domain.Mandate]{
		Table:   "trs_mandates",
		Columns: sqlrepo.WithAuditColumns("creditor", "debtor", "iban", "reference", "scheme", "signed", "last_used", "revoked"),
		Dehydrate: func(m *domain.Mandate) (sqlrepo.Values, error) {
			s := m.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"creditor": s.Creditor, "debtor": s.Debtor, "iban": s.IBAN.String(), "reference": s.Reference,
				"scheme": int64(s.Scheme), "signed": s.Signed, "last_used": optDate(s.LastUsed), "revoked": optDate(s.Revoked)}, m.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Mandate, error) {
			iban, err := ibanOf(r, "iban")
			if err != nil {
				return nil, err
			}
			s := domain.MandateState{Creditor: domain.OrganizationID{UUID: r.UUID("creditor")}, Debtor: domain.PartyID{UUID: r.UUID("debtor")},
				IBAN: iban, Reference: r.String("reference"), Scheme: domain.Scheme(r.Int64("scheme")), Signed: r.Date("signed"),
				LastUsed: r.Date("last_used"), Revoked: r.Date("revoked"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteMandate(domain.MandateID{UUID: r.UUID("id")}, s)
		},
	}
}

// RemittanceMapping maps Remittance to trs_remittances and its items.
func RemittanceMapping() sqlrepo.Mapping[domain.RemittanceID, *domain.Remittance] {
	return sqlrepo.Mapping[domain.RemittanceID, *domain.Remittance]{
		Table: "trs_remittances",
		Columns: sqlrepo.WithAuditColumns("creditor", "account_id", "scheme", "collection_date", "status", "generated_at", "creditor_name",
			"creditor_identifier", "creditor_iban", "creditor_bic", "settled"),
		Dehydrate: func(r *domain.Remittance) (sqlrepo.Values, error) {
			s := r.State()
			v := sqlrepo.Values{"creditor": s.Creditor, "account_id": s.Account, "scheme": int64(s.Scheme), "collection_date": s.CollectionDate,
				"status": int64(s.Status), "generated_at": nil, "creditor_name": opt(s.CreditorName), "creditor_identifier": opt(s.CreditorID),
				"creditor_iban": opt(s.CreditorIBAN.String()), "creditor_bic": opt(s.CreditorBIC), "settled": optDate(s.Settled)}
			if !s.GeneratedAt.IsZero() {
				v["generated_at"] = s.GeneratedAt
			}
			return sqlrepo.AuditStampValues(v, r.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Remittance, error) {
			ciban, err := ibanOf(r, "creditor_iban")
			if err != nil {
				return nil, err
			}
			s := domain.RemittanceState{Creditor: domain.OrganizationID{UUID: r.UUID("creditor")}, Account: domain.AccountID{UUID: r.UUID("account_id")},
				Scheme: domain.Scheme(r.Int64("scheme")), CollectionDate: r.Date("collection_date"), Status: domain.RemittanceStatus(r.Int64("status")),
				CreditorName: r.String("creditor_name"), CreditorID: r.String("creditor_identifier"), CreditorIBAN: ciban, CreditorBIC: r.String("creditor_bic"),
				Settled: r.Date("settled"), Audit: r.AuditStamp()}
			if t := r.NullTime("generated_at"); t != nil {
				s.GeneratedAt = *t
			}
			for _, c := range children.Of("items") {
				iban, err := ibanOf(c, "iban")
				if err != nil {
					return nil, err
				}
				s.Items = append(s.Items, domain.Item{EndToEnd: c.String("end_to_end"), Invoice: domain.InvoiceID{UUID: c.UUID("invoice")},
					Number: c.String("invoice_number"), Installment: int(c.Int64("installment_no")), Debtor: domain.PartyID{UUID: c.UUID("debtor")},
					DebtorName: c.String("debtor_name"), Mandate: domain.MandateID{UUID: c.UUID("mandate_id")}, MandateRef: c.String("mandate_reference"),
					Signed: c.Date("mandate_signed"), IBAN: iban, Amount: c.Decimal("amount"), Sequence: c.String("sequence_type"),
					Returned: c.Date("returned"), Reason: c.String("return_reason")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(s.Items, func(a, b domain.Item) int {
				if a.Number != b.Number {
					if a.Number < b.Number {
						return -1
					}
					return 1
				}
				return a.Installment - b.Installment
			})
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteRemittance(domain.RemittanceID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Remittance]{{
			Name: "items", Table: "trs_remittance_items", ForeignKey: "remittance_id", OrderBy: []string{"end_to_end"},
			Columns: []string{"end_to_end", "invoice", "invoice_number", "installment_no", "debtor", "debtor_name", "mandate_id", "mandate_reference",
				"mandate_signed", "iban", "amount", "sequence_type", "returned", "return_reason"},
			Dehydrate: func(r *domain.Remittance) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, i := range r.State().Items {
					out = append(out, sqlrepo.Values{"end_to_end": i.EndToEnd, "invoice": i.Invoice, "invoice_number": i.Number,
						"installment_no": int64(i.Installment), "debtor": i.Debtor, "debtor_name": opt(i.DebtorName), "mandate_id": i.Mandate,
						"mandate_reference": i.MandateRef, "mandate_signed": i.Signed, "iban": i.IBAN.String(), "amount": i.Amount.StringFixed(2),
						"sequence_type": opt(i.Sequence), "returned": optDate(i.Returned), "return_reason": opt(i.Reason)})
				}
				return out, nil
			},
		}},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("treasury: unsupported backend %T", b) }

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

func MandateRepositoryFactory(b hotswap.Backend) (domain.MandateRepository, error) {
	return repository(b, MandateMapping())
}

func RemittanceRepositoryFactory(b hotswap.Backend) (domain.RemittanceRepository, error) {
	return repository(b, RemittanceMapping())
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

// ReceivablesDueItems adapts the Receivables Collectable contract to the Treasury port (ACL).
type ReceivablesDueItems struct{ Collectable receivables.Collectable }

var _ domain.Receivables = ReceivablesDueItems{}

// DueItems implements domain.Receivables.
func (r ReceivablesDueItems) DueItems(ctx context.Context, seller domain.OrganizationID, dueTo vocab.Date) ([]domain.DueItem, error) {
	items, err := r.Collectable.DueItems(ctx, seller.String(), dueTo.String())
	if err != nil {
		return nil, err
	}
	out := make([]domain.DueItem, 0, len(items))
	for _, i := range items {
		inv, err1 := fw.ParseUUID(i.InvoiceID)
		cust, err2 := fw.ParseUUID(i.Customer)
		due, err3 := vocab.ParseDate(i.Due)
		open, err4 := vocab.ParseDecimal(i.Open)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			return nil, fmt.Errorf("treasury: invalid due item from Receivables: %+v", i)
		}
		out = append(out, domain.DueItem{Invoice: domain.InvoiceID{UUID: inv}, Number: i.Number, Customer: domain.PartyID{UUID: cust},
			Installment: i.Installment, Due: due, Open: open})
	}
	return out, nil
}

// PartiesIdentities adapts the Parties TaxIdentities contract to the Treasury port (ACL).
type PartiesIdentities struct{ TaxIdentities parties.TaxIdentities }

var _ domain.Identities = PartiesIdentities{}

// Identities implements domain.Identities.
func (p PartiesIdentities) Identities(ctx context.Context, ids []domain.PartyID) (map[domain.PartyID]domain.Identity, error) {
	out := map[domain.PartyID]domain.Identity{}
	var req []string
	seen := map[domain.PartyID]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			req = append(req, id.String())
		}
	}
	for start := 0; start < len(req); start += parties.MaxDirectoryBatch {
		m, err := p.TaxIdentities.TaxIdentities(ctx, req[start:min(start+parties.MaxDirectoryBatch, len(req))])
		if err != nil {
			return nil, err
		}
		for id, ti := range m {
			if u, err := fw.ParseUUID(id); err == nil {
				out[domain.PartyID{UUID: u}] = domain.Identity{Name: ti.Name, NIF: vocab.NormalizeDocumentNumber(ti.Number)}
			}
		}
	}
	return out, nil
}

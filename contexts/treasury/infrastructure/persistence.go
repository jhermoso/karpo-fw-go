// Package infrastructure stores the Treasury context: SQL mappings, versioned schema of the five
// engines, hot-swap factories and the adapters to Receivables and Parties.
package infrastructure

import (
	"context"
	"fmt"
	"slices"
	"strings"

	parties "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
	payments "github.com/jhermoso/karpo-fw-go/contexts/payments/contracts"
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

var transfersDDL = []string{
	`CREATE TABLE trs_transfer_orders (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, debtor {uuid} NOT NULL, account_id {uuid} NOT NULL,
	execution_date {date} NOT NULL, status {int} NOT NULL, generated_at {ts}, debtor_name {str:200}, debtor_iban {str:34}, debtor_bic {str:11},
	settled {date}, ` + audit + `, FOREIGN KEY (account_id) REFERENCES trs_accounts (id))`,
	`CREATE INDEX ix_trs_transfer_orders_debtor ON trs_transfer_orders (debtor, status)`,
	`CREATE TABLE trs_transfers (order_id {uuid} NOT NULL, end_to_end {str:35} NOT NULL, payable {uuid} NOT NULL, line_no {int} NOT NULL,
	document {str:60} NOT NULL, payee {str:40} NOT NULL, payee_name {str:200}, iban {str:34} NOT NULL, amount {str:30} NOT NULL, rejected {date},
	reject_reason {str:4}, PRIMARY KEY (order_id, end_to_end), FOREIGN KEY (order_id) REFERENCES trs_transfer_orders (id))`,
	`CREATE INDEX ix_trs_transfers_payable ON trs_transfers (payable)`,
}

// A statement belongs to an account; "pending" counts its movements not reconciled yet.
var statementsDDL = []string{
	`CREATE TABLE trs_statements (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, owner {uuid} NOT NULL, account_id {uuid} NOT NULL,
	period_from {date} NOT NULL, period_to {date} NOT NULL, opening {str:30} NOT NULL, closing {str:30} NOT NULL, pending {int} NOT NULL,
	` + audit + `, FOREIGN KEY (account_id) REFERENCES trs_accounts (id))`,
	`CREATE UNIQUE INDEX ux_trs_statements_period ON trs_statements (account_id, period_from)`,
	`CREATE TABLE trs_statement_lines (statement_id {uuid} NOT NULL, line_no {int} NOT NULL, booked {date} NOT NULL, value_date {date} NOT NULL,
	amount {str:30} NOT NULL, concept {str:500}, reference {str:60}, match_kind {str:20}, match_id {str:64}, match_note {str:200},
	PRIMARY KEY (statement_id, line_no), FOREIGN KEY (statement_id) REFERENCES trs_statements (id))`,
	`CREATE INDEX ix_trs_statement_lines_match ON trs_statement_lines (match_kind, match_id)`,
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
		{Version: 3, Name: "credit transfer orders", Up: sqlrepo.RenderDDLAll(transfersDDL...)},
		{Version: 4, Name: "bank statements", Up: sqlrepo.RenderDDLAll(statementsDDL...)},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"trs_statement_lines", "trs_statements", "trs_transfers", "trs_transfer_orders", "trs_remittance_items", "trs_remittances", "trs_mandates", "trs_accounts", TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

// TransferOrderMapping maps TransferOrder to trs_transfer_orders and its transfers.
func TransferOrderMapping() sqlrepo.Mapping[domain.TransferOrderID, *domain.TransferOrder] {
	return sqlrepo.Mapping[domain.TransferOrderID, *domain.TransferOrder]{
		Table: "trs_transfer_orders",
		Columns: sqlrepo.WithAuditColumns("debtor", "account_id", "execution_date", "status", "generated_at", "debtor_name", "debtor_iban", "debtor_bic",
			"settled"),
		Dehydrate: func(o *domain.TransferOrder) (sqlrepo.Values, error) {
			s := o.State()
			v := sqlrepo.Values{"debtor": s.Debtor, "account_id": s.Account, "execution_date": s.ExecutionDate, "status": int64(s.Status),
				"generated_at": nil, "debtor_name": opt(s.DebtorName), "debtor_iban": opt(s.DebtorIBAN.String()), "debtor_bic": opt(s.DebtorBIC),
				"settled": optDate(s.Settled)}
			if !s.GeneratedAt.IsZero() {
				v["generated_at"] = s.GeneratedAt
			}
			return sqlrepo.AuditStampValues(v, o.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.TransferOrder, error) {
			diban, err := ibanOf(r, "debtor_iban")
			if err != nil {
				return nil, err
			}
			s := domain.TransferOrderState{Debtor: domain.OrganizationID{UUID: r.UUID("debtor")}, Account: domain.AccountID{UUID: r.UUID("account_id")},
				ExecutionDate: r.Date("execution_date"), Status: domain.RemittanceStatus(r.Int64("status")), DebtorName: r.String("debtor_name"),
				DebtorIBAN: diban, DebtorBIC: r.String("debtor_bic"), Settled: r.Date("settled"), Audit: r.AuditStamp()}
			if t := r.NullTime("generated_at"); t != nil {
				s.GeneratedAt = *t
			}
			for _, c := range children.Of("transfers") {
				iban, err := ibanOf(c, "iban")
				if err != nil {
					return nil, err
				}
				s.Transfers = append(s.Transfers, domain.Transfer{EndToEnd: c.String("end_to_end"), Payable: domain.PayableID{UUID: c.UUID("payable")},
					Line: int(c.Int64("line_no")), Document: c.String("document"), Payee: c.String("payee"), PayeeName: c.String("payee_name"), IBAN: iban,
					Amount: c.Decimal("amount"), Rejected: c.Date("rejected"), Reason: c.String("reject_reason")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(s.Transfers, func(a, b domain.Transfer) int { return strings.Compare(a.EndToEnd, b.EndToEnd) })
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteTransferOrder(domain.TransferOrderID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.TransferOrder]{{
			Name: "transfers", Table: "trs_transfers", ForeignKey: "order_id", OrderBy: []string{"end_to_end"},
			Columns: []string{"end_to_end", "payable", "line_no", "document", "payee", "payee_name", "iban", "amount", "rejected", "reject_reason"},
			Dehydrate: func(o *domain.TransferOrder) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, t := range o.State().Transfers {
					out = append(out, sqlrepo.Values{"end_to_end": t.EndToEnd, "payable": t.Payable, "line_no": int64(t.Line), "document": t.Document,
						"payee": t.Payee, "payee_name": opt(t.PayeeName), "iban": t.IBAN.String(), "amount": t.Amount.StringFixed(2),
						"rejected": optDate(t.Rejected), "reject_reason": opt(t.Reason)})
				}
				return out, nil
			},
		}},
	}
}

// StatementMapping maps Statement to trs_statements and its movements.
func StatementMapping() sqlrepo.Mapping[domain.StatementID, *domain.Statement] {
	return sqlrepo.Mapping[domain.StatementID, *domain.Statement]{
		Table:   "trs_statements",
		Columns: sqlrepo.WithAuditColumns("owner", "account_id", "period_from", "period_to", "opening", "closing", "pending"),
		Dehydrate: func(s *domain.Statement) (sqlrepo.Values, error) {
			st := s.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"owner": st.Owner, "account_id": st.Account, "period_from": st.From, "period_to": st.To,
				"opening": st.Opening.StringFixed(2), "closing": st.Closing.StringFixed(2), "pending": int64(s.Pending())}, s.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Statement, error) {
			st := domain.StatementState{Owner: domain.OrganizationID{UUID: r.UUID("owner")}, Account: domain.AccountID{UUID: r.UUID("account_id")},
				From: r.Date("period_from"), To: r.Date("period_to"), Opening: r.Decimal("opening"), Closing: r.Decimal("closing"), Audit: r.AuditStamp()}
			for _, c := range children.Of("lines") {
				st.Lines = append(st.Lines, domain.StatementLine{No: int(c.Int64("line_no")), Date: c.Date("booked"), ValueDate: c.Date("value_date"),
					Amount: c.Decimal("amount"), Concept: c.String("concept"), Reference: c.String("reference"),
					Match: domain.Match{Kind: c.String("match_kind"), ID: c.String("match_id"), Note: c.String("match_note")}})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			// Engines do not agree on the order of the children: movements are sorted here.
			slices.SortFunc(st.Lines, func(a, b domain.StatementLine) int { return a.No - b.No })
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteStatement(domain.StatementID{UUID: r.UUID("id")}, st)
		},
		Children: []sqlrepo.Child[*domain.Statement]{{
			Name: "lines", Table: "trs_statement_lines", ForeignKey: "statement_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "booked", "value_date", "amount", "concept", "reference", "match_kind", "match_id", "match_note"},
			Dehydrate: func(s *domain.Statement) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range s.State().Lines {
					out = append(out, sqlrepo.Values{"line_no": int64(l.No), "booked": l.Date, "value_date": l.ValueDate, "amount": l.Amount.StringFixed(2),
						"concept": opt(l.Concept), "reference": opt(l.Reference), "match_kind": opt(l.Match.Kind), "match_id": opt(l.Match.ID),
						"match_note": opt(l.Match.Note)})
				}
				return out, nil
			},
		}},
	}
}

// StatementRepositoryFactory builds the statement repository.
func StatementRepositoryFactory(b hotswap.Backend) (domain.StatementRepository, error) {
	return repository(b, StatementMapping())
}

// TransferOrderRepositoryFactory builds the transfer order repository.
func TransferOrderRepositoryFactory(b hotswap.Backend) (domain.TransferOrderRepository, error) {
	return repository(b, TransferOrderMapping())
}

// PaymentsPayables adapts the Payments Payable contract to the Treasury port (ACL).
type PaymentsPayables struct{ Payable payments.Payable }

var _ domain.Payables = PaymentsPayables{}

// DueForTransfer implements domain.Payables.
func (p PaymentsPayables) DueForTransfer(ctx context.Context, debtor domain.OrganizationID, dueTo vocab.Date) ([]domain.PayableDue, int, error) {
	items, without, err := p.Payable.DueForTransfer(ctx, debtor.String(), dueTo.String())
	if err != nil {
		return nil, 0, err
	}
	out := make([]domain.PayableDue, 0, len(items))
	for _, i := range items {
		id, err1 := fw.ParseUUID(i.PayableID)
		due, err2 := vocab.ParseDate(i.Due)
		if err1 != nil || err2 != nil {
			return nil, 0, fmt.Errorf("treasury: invalid payable from Payments: %+v", i)
		}
		d := domain.PayableDue{Payable: domain.PayableID{UUID: id}, Kind: i.Kind, Document: i.Document, Payee: i.Payee, Due: due}
		for _, t := range i.PayTo {
			iban, err1 := vocab.NewIBAN(t.IBAN)
			amount, err2 := vocab.ParseDecimal(t.Amount)
			if err1 != nil || err2 != nil {
				return nil, 0, fmt.Errorf("treasury: invalid account from Payments: %+v", t)
			}
			d.PayTo = append(d.PayTo, domain.PayableAccount{IBAN: iban, Amount: amount})
		}
		out = append(out, d)
	}
	return out, without, nil
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

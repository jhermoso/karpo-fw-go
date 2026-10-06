// Package infrastructure stores the Documents context: SQL mapping, versioned schema of the five
// engines and hot-swap factories.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/documents/domain"
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
const Context = "documents"

// Technical tables of the context.
const (
	TableAuditLog = "documents_audit_log"
	TableInbox    = "documents_inbox"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

// A fact has one entry: the unique index is the backstop the C# register lacked.
var schemaDDL = []string{
	`CREATE TABLE doc_documents (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, doc_type {str:20} NOT NULL,
	fact_id {str:64} NOT NULL, doc_number {str:60} NOT NULL, reference {str:60}, doc_date {date} NOT NULL, party {uuid}, total {str:30},
	origin_type {str:20}, origin_id {str:64}, relation {str:20}, cancelled {bool} NOT NULL, cancel_reason {str:200}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_doc_documents_fact ON doc_documents (doc_type, fact_id)`,
	`CREATE INDEX ix_doc_documents_origin ON doc_documents (origin_type, origin_id)`,
	`CREATE INDEX ix_doc_documents_date ON doc_documents (company, doc_date)`,
	`CREATE INDEX ix_doc_documents_party ON doc_documents (company, party)`,
}

func technicalDDL(d string) []string {
	switch d {
	case "sqlite":
		return slices.Concat(sqlite.AuditDDL(TableAuditLog), sqlite.InboxDDL(TableInbox))
	case "postgres":
		return slices.Concat(postgres.AuditDDL(TableAuditLog), postgres.InboxDDL(TableInbox))
	case "sqlserver":
		return slices.Concat(sqlserver.AuditDDL(TableAuditLog), sqlserver.InboxDDL(TableInbox))
	case "oracle":
		return slices.Concat(oracle.AuditDDL(TableAuditLog), oracle.InboxDDL(TableInbox))
	case "mysql":
		return slices.Concat(mysql.AuditDDL(TableAuditLog), mysql.InboxDDL(TableInbox))
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
		{Version: 1, Name: "document register", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "audit log and inbox", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context (drop order).
var Tables = []string{"doc_documents", TableAuditLog, TableInbox}

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

// DocumentMapping maps Document to doc_documents.
func DocumentMapping() sqlrepo.Mapping[domain.DocumentID, *domain.Document] {
	return sqlrepo.Mapping[domain.DocumentID, *domain.Document]{
		Table: "doc_documents",
		Columns: sqlrepo.WithAuditColumns("company", "doc_type", "fact_id", "doc_number", "reference", "doc_date", "party", "total", "origin_type",
			"origin_id", "relation", "cancelled", "cancel_reason"),
		Dehydrate: func(d *domain.Document) (sqlrepo.Values, error) {
			s := d.State()
			v := sqlrepo.Values{"company": s.Company, "doc_type": string(s.Fact.Type), "fact_id": s.Fact.ID, "doc_number": s.Number,
				"reference": opt(s.Reference), "doc_date": s.Date, "party": optUUID(s.Party.UUID), "total": nil, "origin_type": opt(string(s.Origin.Type)),
				"origin_id": opt(s.Origin.ID), "relation": opt(s.Relation), "cancelled": s.Cancelled, "cancel_reason": opt(s.CancelReason)}
			if s.HasTotal {
				v["total"] = s.Total.StringFixed(2)
			}
			return sqlrepo.AuditStampValues(v, d.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Document, error) {
			s := domain.DocumentState{Company: domain.OrganizationID{UUID: r.UUID("company")},
				Fact: domain.Ref{Type: domain.Type(r.String("doc_type")), ID: r.String("fact_id")}, Number: r.String("doc_number"),
				Reference: r.String("reference"), Date: r.Date("doc_date"), Party: domain.PartyID{UUID: r.UUID("party")},
				Origin: domain.Ref{Type: domain.Type(r.String("origin_type")), ID: r.String("origin_id")}, Relation: r.String("relation"),
				Cancelled: r.Bool("cancelled"), CancelReason: r.String("cancel_reason"), Audit: r.AuditStamp()}
			if t := r.String("total"); t != "" {
				s.Total, s.HasTotal = r.Decimal("total"), true
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteDocument(domain.DocumentID{UUID: r.UUID("id")}, s)
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("documents: unsupported backend %T", b) }

// DocumentRepositoryFactory builds the register repository.
func DocumentRepositoryFactory(b hotswap.Backend) (domain.DocumentRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, DocumentMapping())
	case *memory.Store:
		return memory.NewRepository[domain.DocumentID, *domain.Document](db), nil
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

// InboxFactory builds the inbox of the events Documents records.
func InboxFactory(b hotswap.Backend) (application.InboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewInbox(db, TableInbox)
	case *memory.Store:
		return memory.NewInbox(db), nil
	}
	return nil, unsupported(b)
}

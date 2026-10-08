// Package infrastructure stores the Imports context: SQL mappings, versioned schema of the five
// engines and hot-swap factories.
package infrastructure

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/imports/domain"
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
const Context = "imports"

// Technical tables of the context.
const (
	TableOutbox            = "imports_outbox"
	TableIntegrationOutbox = "imports_integration_outbox"
	TableAuditLog          = "imports_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

// message_text holds 1,000 characters. It was declared as {str:4000} while {str:N} counted bytes on
// Oracle; migration 3 gives it its real size.
var schemaDDL = []string{
	`CREATE TABLE imp_runs (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, source_key {str:64} NOT NULL, files {str:1000},
	status {str:30} NOT NULL, started_at {ts} NOT NULL, finished_at {ts}, started_by {str:64}, started_by_name {str:200}, reason {str:500},
	errors {int} NOT NULL, warnings {int} NOT NULL, dropped {int} NOT NULL, ` + audit + `)`,
	`CREATE INDEX ix_imp_runs_started ON imp_runs (started_at)`,
	`CREATE INDEX ix_imp_runs_source ON imp_runs (source_key, status)`,
	`CREATE TABLE imp_run_counts (run_id {uuid} NOT NULL, line_no {int} NOT NULL, record_kind {str:64} NOT NULL, read_n {int} NOT NULL,
	created_n {int} NOT NULL, updated_n {int} NOT NULL, unchanged_n {int} NOT NULL, skipped_n {int} NOT NULL, failed_n {int} NOT NULL,
	PRIMARY KEY (run_id, line_no), FOREIGN KEY (run_id) REFERENCES imp_runs (id))`,
	`CREATE TABLE imp_run_messages (run_id {uuid} NOT NULL, line_no {int} NOT NULL, severity {str:16} NOT NULL, code {str:100},
	message_text {str:4000}, file_name {str:200}, source_line {int} NOT NULL, record_kind {str:64}, legacy_key {str:256}, entity_id {str:128},
	PRIMARY KEY (run_id, line_no), FOREIGN KEY (run_id) REFERENCES imp_runs (id))`,
	`CREATE TABLE imp_references (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, source_key {str:64} NOT NULL, record_kind {str:64} NOT NULL,
	scope_key {str:128} NOT NULL, legacy_key {str:256} NOT NULL, entity_type {str:128} NOT NULL, entity_id {str:128} NOT NULL,
	first_run {uuid} NOT NULL, last_run {uuid} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_imp_references_key ON imp_references (source_key, record_kind, scope_key, legacy_key)`,
	`CREATE INDEX ix_imp_references_entity ON imp_references (entity_id)`,
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

// Changing the size of a column has no portable form. SQLite stores {str:N} as TEXT, without a
// size: there is nothing to change.
var messageTextDDL = map[string]string{
	"postgres":  `ALTER TABLE imp_run_messages ALTER COLUMN message_text TYPE {str:1000}`,
	"sqlserver": `ALTER TABLE imp_run_messages ALTER COLUMN message_text {str:1000} NULL`,
	"oracle":    `ALTER TABLE imp_run_messages MODIFY (message_text {str:1000})`,
	"mysql":     `ALTER TABLE imp_run_messages MODIFY COLUMN message_text {str:1000} NULL`,
}

// Migrations is the versioned schema of the context.
func Migrations() sqlrepo.MigrationSet {
	technical, messageText := map[string][]string{}, map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
		messageText[d] = []string{}
		if s, ok := messageTextDDL[d]; ok {
			messageText[d] = []string{sqlrepo.RenderDDL(d, s)}
		}
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "runs and references", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
		{Version: 3, Name: "message text of 1,000 characters", Up: messageText},
	}}
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"imp_run_messages", "imp_run_counts", "imp_runs", "imp_references", TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

// Engines do not agree on the order of the children: they are sorted here.
func byNo(rows []*sqlrepo.Row) []*sqlrepo.Row {
	out := slices.Clone(rows)
	slices.SortFunc(out, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
	return out
}

// RunMapping maps Run to imp_runs, its counts and its messages.
func RunMapping() sqlrepo.Mapping[domain.RunID, *domain.Run] {
	return sqlrepo.Mapping[domain.RunID, *domain.Run]{
		Table: "imp_runs",
		Columns: sqlrepo.WithAuditColumns("source_key", "files", "status", "started_at", "finished_at", "started_by", "started_by_name", "reason", "errors",
			"warnings", "dropped"),
		Dehydrate: func(x *domain.Run) (sqlrepo.Values, error) {
			s := x.State()
			var finished any
			if !s.FinishedAt.IsZero() {
				finished = s.FinishedAt
			}
			return sqlrepo.AuditStampValues(sqlrepo.Values{"source_key": s.Source, "files": opt(strings.Join(s.Files, "|")), "status": string(s.Status),
				"started_at": s.StartedAt, "finished_at": finished, "started_by": opt(s.StartedBy), "started_by_name": opt(s.StartedByName),
				"reason": opt(s.Reason), "errors": int64(s.Errors), "warnings": int64(s.Warnings), "dropped": int64(s.Dropped)}, x.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Run, error) {
			s := domain.RunState{Source: r.String("source_key"), Status: domain.Status(r.String("status")), StartedAt: r.Time("started_at").UTC(),
				StartedBy: r.String("started_by"), StartedByName: r.String("started_by_name"), Reason: r.String("reason"), Errors: int(r.Int64("errors")),
				Warnings: int(r.Int64("warnings")), Dropped: int(r.Int64("dropped")), Audit: r.AuditStamp()}
			if f := r.String("files"); f != "" {
				s.Files = strings.Split(f, "|")
			}
			if t := r.NullTime("finished_at"); t != nil {
				s.FinishedAt = t.UTC()
			}
			for _, c := range byNo(children.Of("counts")) {
				s.Counts = append(s.Counts, domain.Count{Kind: c.String("record_kind"), Read: int(c.Int64("read_n")), Created: int(c.Int64("created_n")),
					Updated: int(c.Int64("updated_n")), Unchanged: int(c.Int64("unchanged_n")), Skipped: int(c.Int64("skipped_n")), Failed: int(c.Int64("failed_n"))})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("messages")) {
				s.Messages = append(s.Messages, domain.Message{No: int(c.Int64("line_no")), Severity: c.String("severity"), Code: c.String("code"),
					Text: c.String("message_text"), File: c.String("file_name"), Line: int(c.Int64("source_line")), Kind: c.String("record_kind"),
					Key: c.String("legacy_key"), Entity: c.String("entity_id")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteRun(domain.RunID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Run]{{
			Name: "counts", Table: "imp_run_counts", ForeignKey: "run_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "record_kind", "read_n", "created_n", "updated_n", "unchanged_n", "skipped_n", "failed_n"},
			Dehydrate: func(x *domain.Run) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, c := range x.State().Counts {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "record_kind": c.Kind, "read_n": int64(c.Read), "created_n": int64(c.Created),
						"updated_n": int64(c.Updated), "unchanged_n": int64(c.Unchanged), "skipped_n": int64(c.Skipped), "failed_n": int64(c.Failed)})
				}
				return out, nil
			},
		}, {
			Name: "messages", Table: "imp_run_messages", ForeignKey: "run_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "severity", "code", "message_text", "file_name", "source_line", "record_kind", "legacy_key", "entity_id"},
			Dehydrate: func(x *domain.Run) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, m := range x.State().Messages {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "severity": m.Severity, "code": opt(m.Code), "message_text": opt(m.Text),
						"file_name": opt(m.File), "source_line": int64(m.Line), "record_kind": opt(m.Kind), "legacy_key": opt(m.Key), "entity_id": opt(m.Entity)})
				}
				return out, nil
			},
		}},
	}
}

// ReferenceMapping maps Reference to imp_references.
func ReferenceMapping() sqlrepo.Mapping[domain.ReferenceID, *domain.Reference] {
	return sqlrepo.Mapping[domain.ReferenceID, *domain.Reference]{
		Table:   "imp_references",
		Columns: sqlrepo.WithAuditColumns("source_key", "record_kind", "scope_key", "legacy_key", "entity_type", "entity_id", "first_run", "last_run"),
		Dehydrate: func(x *domain.Reference) (sqlrepo.Values, error) {
			s := x.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"source_key": s.Source, "record_kind": s.Kind, "scope_key": s.Scope, "legacy_key": s.Key,
				"entity_type": s.EntityType, "entity_id": s.EntityID, "first_run": s.FirstRun, "last_run": s.LastRun}, x.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Reference, error) {
			s := domain.ReferenceState{Source: r.String("source_key"), Kind: r.String("record_kind"), Scope: r.String("scope_key"), Key: r.String("legacy_key"),
				EntityType: r.String("entity_type"), EntityID: r.String("entity_id"), FirstRun: domain.RunID{UUID: r.UUID("first_run")},
				LastRun: domain.RunID{UUID: r.UUID("last_run")}, Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteReference(domain.ReferenceID{UUID: r.UUID("id")}, s)
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("imports: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// RunRepositoryFactory builds the run repository.
func RunRepositoryFactory(b hotswap.Backend) (domain.RunRepository, error) {
	return repository(b, RunMapping())
}

// ReferenceRepositoryFactory builds the reference repository.
func ReferenceRepositoryFactory(b hotswap.Backend) (domain.ReferenceRepository, error) {
	return repository(b, ReferenceMapping())
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

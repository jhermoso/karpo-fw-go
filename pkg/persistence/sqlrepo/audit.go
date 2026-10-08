package sqlrepo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// DefaultAuditTable is the default audit log table name. Each dialect package provides the DDL
// (AuditDDL) for its engine.
const DefaultAuditTable = "audit_log"

var auditColumns = []string{
	"id", "aggregate_type", "aggregate_id", "aggregate_version", "operation", "actor_id", "actor_name",
	"channel", "import_source", "import_run_id", "import_file", "correlation_id", "occurred_at", "changes", "events",
}

// AuditLog is the SQL implementation of application.AuditLog (append-only table, the Go
// counterpart of the C# AuditLogEntry). Append joins the unit of work in ctx.
type AuditLog struct {
	db    *DB
	table string
}

// NewAuditLog creates an audit log on db using table (DefaultAuditTable when empty).
func NewAuditLog(db *DB, table string) (*AuditLog, error) {
	if table == "" {
		table = DefaultAuditTable
	}
	if err := checkIdent("table", table); err != nil {
		return nil, err
	}
	return &AuditLog{db: db, table: table}, nil
}

func (l *AuditLog) q(s string) string { return l.db.d.Quote(s) }

// Append inserts the records in the current unit of work.
func (l *AuditLog) Append(ctx context.Context, records ...application.AuditRecord) error {
	if len(records) == 0 {
		return nil
	}
	cols := make([]string, len(auditColumns))
	for i, c := range auditColumns {
		cols[i] = l.q(c)
	}
	return l.db.Do(ctx, func(ctx context.Context) error {
		for _, r := range records {
			changes, err := json.Marshal(r.Changes)
			if err != nil {
				return fmt.Errorf("sqlrepo: audit changes: %w", err)
			}
			var src, run, file string
			if r.Import != nil {
				src, file = r.Import.SourceKey, r.Import.SourceFile
				run = r.Import.RunID.String()
			}
			actorID := ""
			if !r.Actor.IsSystem() {
				actorID = r.Actor.PartyID.String()
			}
			vals := []any{
				r.ID, r.AggregateType, r.AggregateID, r.AggregateVersion, r.Operation, actorID, r.Actor.Name,
				r.Channel, src, run, file, r.CorrelationID, r.At, string(changes), strings.Join(r.Events, ","),
			}
			b := &Builder{d: l.db.d}
			phs := make([]string, len(vals))
			for i, v := range vals {
				if phs[i], err = b.Arg(v); err != nil {
					return err
				}
			}
			query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", l.q(l.table), strings.Join(cols, ", "), strings.Join(phs, ", "))
			if _, err := l.db.executor(ctx).ExecContext(ctx, query, b.args...); err != nil {
				return fmt.Errorf("sqlrepo: audit append: %w", err)
			}
		}
		return nil
	})
}

// Trail returns the records of one aggregate, oldest first.
func (l *AuditLog) Trail(ctx context.Context, aggregateType, aggregateID string) ([]application.AuditRecord, error) {
	b := &Builder{d: l.db.d}
	tp, _ := b.Arg(aggregateType)
	ip, _ := b.Arg(aggregateID)
	sel := make([]string, len(auditColumns))
	for i, c := range auditColumns {
		sel[i] = "a." + l.q(c)
	}
	query := fmt.Sprintf("SELECT %s FROM %s a WHERE a.%s = %s AND a.%s = %s ORDER BY a.%s, a.%s",
		strings.Join(sel, ", "), l.q(l.table), l.q("aggregate_type"), tp, l.q("aggregate_id"), ip,
		l.q("occurred_at"), l.q("id"))
	rows, err := l.db.executor(ctx).QueryContext(ctx, query, b.args...)
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: audit trail: %w", err)
	}
	scanned, err := scanAll(l.db.d, rows, auditColumns)
	if err != nil {
		return nil, fmt.Errorf("sqlrepo: audit scan: %w", err)
	}
	out := make([]application.AuditRecord, 0, len(scanned))
	for _, row := range scanned {
		r := application.AuditRecord{
			ID:               row.String("id"),
			AggregateType:    row.String("aggregate_type"),
			AggregateID:      row.String("aggregate_id"),
			AggregateVersion: row.Int64("aggregate_version"),
			Operation:        row.String("operation"),
			Channel:          row.String("channel"),
			CorrelationID:    row.String("correlation_id"),
			At:               row.Time("occurred_at"),
		}
		r.Actor = vocab.SystemActor
		if id := row.String("actor_id"); id != "" {
			u, err := domain.ParseUUID(id)
			if err != nil {
				return nil, err
			}
			r.Actor = vocab.Actor{PartyID: u, Name: row.String("actor_name")}
		}
		if src := row.String("import_source"); src != "" {
			run, _ := domain.ParseUUID(row.String("import_run_id"))
			r.Import = &application.ImportProvenance{SourceKey: src, RunID: run, SourceFile: row.String("import_file")}
		}
		if raw := row.Bytes("changes"); len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &r.Changes); err != nil {
				return nil, fmt.Errorf("sqlrepo: audit changes: %w", err)
			}
		}
		if ev := row.String("events"); ev != "" {
			r.Events = strings.Split(ev, ",")
		}
		if err := row.Err(); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

var _ application.AuditLog = (*AuditLog)(nil)

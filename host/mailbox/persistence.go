package mailbox

import (
	"context"
	"fmt"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/persistence/hotswap"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// Context is the name the schema of the mailboxes is versioned under.
const Context = "host"

// Table is where the deliveries are kept.
const Table = "host_deliveries"

var ddl = []string{
	`CREATE TABLE host_deliveries (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, consumer {str:100} NOT NULL, event_type {str:100} NOT NULL,
	envelope_id {str:64} NOT NULL, source_name {str:50}, subject_id {str:100}, occurred_at {ts}, correlation_id {str:100}, causation_id {str:100},
	payload {text} NOT NULL, code {str:100}, reason {str:500}, received_at {ts} NOT NULL, attempts {int} NOT NULL, next_attempt {ts},
	status {str:20} NOT NULL, resolved_at {ts}, note {str:500}, created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts},
	modified_by_id {str:64}, modified_by_name {str:200})`,
	`CREATE UNIQUE INDEX ux_host_deliveries_envelope ON host_deliveries (consumer, envelope_id)`,
	`CREATE INDEX ix_host_deliveries_subject ON host_deliveries (consumer, subject_id, status)`,
	`CREATE INDEX ix_host_deliveries_status ON host_deliveries (status)`,
}

// Migrations is the versioned schema of the mailboxes.
func Migrations() sqlrepo.MigrationSet {
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "messages kept for the listeners that could not take them", Up: sqlrepo.RenderDDLAll(ddl...)},
	}}
}

// DropAll removes the table and its migration history (tests only).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	_, _ = db.ExecContext(ctx, "DROP TABLE "+Table)
	_, _ = db.ExecContext(ctx, "DELETE FROM "+sqlrepo.DefaultMigrationsTable+" WHERE context = '"+Context+"'")
}

// Mapping maps Delivery to host_deliveries.
func Mapping() sqlrepo.Mapping[ID, *Delivery] {
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
	return sqlrepo.Mapping[ID, *Delivery]{
		Table: Table,
		Columns: sqlrepo.WithAuditColumns("consumer", "event_type", "envelope_id", "source_name", "subject_id", "occurred_at", "correlation_id",
			"causation_id", "payload", "code", "reason", "received_at", "attempts", "next_attempt", "status", "resolved_at", "note"),
		Dehydrate: func(d *Delivery) (sqlrepo.Values, error) {
			s := d.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"consumer": s.Consumer, "event_type": s.EventType, "envelope_id": s.EnvelopeID,
				"source_name": text(s.Source), "subject_id": text(s.Subject), "occurred_at": moment(s.OccurredAt), "correlation_id": text(s.CorrelationID),
				"causation_id": text(s.CausationID), "payload": s.Data, "code": text(s.Code), "reason": text(s.Reason), "received_at": s.ReceivedAt,
				"attempts": int64(s.Attempts), "next_attempt": moment(s.NextAttempt), "status": string(s.Status), "resolved_at": moment(s.ResolvedAt),
				"note": text(s.Note)}, d.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*Delivery, error) {
			s := State{Consumer: r.String("consumer"), EventType: r.String("event_type"), EnvelopeID: r.String("envelope_id"),
				Source: r.String("source_name"), Subject: r.String("subject_id"), OccurredAt: at(r, "occurred_at"), CorrelationID: r.String("correlation_id"),
				CausationID: r.String("causation_id"), Data: r.String("payload"), Code: r.String("code"), Reason: r.String("reason"),
				ReceivedAt: r.Time("received_at").UTC(), Attempts: int(r.Int64("attempts")), NextAttempt: at(r, "next_attempt"),
				Status: Status(r.String("status")), ResolvedAt: at(r, "resolved_at"), Note: r.String("note"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return Reconstitute(ID{UUID: r.UUID("id")}, s)
		},
	}
}

// RepositoryFactory builds the repository of the deliveries on a backend.
func RepositoryFactory(b hotswap.Backend) (Repository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, Mapping())
	case *memory.Store:
		return memory.NewRepository[ID, *Delivery](db), nil
	}
	return nil, fmt.Errorf("mailbox: unsupported backend %T", b)
}

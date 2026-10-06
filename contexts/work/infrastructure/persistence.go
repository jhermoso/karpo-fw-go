// Package infrastructure stores the Work context: SQL mappings, versioned schema of the five
// engines and hot-swap factories.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/work/domain"
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
const Context = "work"

// Technical tables of the context.
const (
	TableOutbox            = "work_outbox"
	TableIntegrationOutbox = "work_integration_outbox"
	TableAuditLog          = "work_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE wrk_works (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, code {str:20} NOT NULL,
	kind {str:20} NOT NULL, parent {uuid}, name {str:200} NOT NULL, description {str:1000}, purpose {str:20}, customer {uuid}, facility {uuid},
	asset {uuid}, planned_start {date}, planned_end {date}, estimated_hours {str:30} NOT NULL, budget {str:30} NOT NULL, status {str:20} NOT NULL,
	started {date}, finished {date}, cancel_reason {str:200}, hours {str:30} NOT NULL, cost {str:30} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_wrk_works_code ON wrk_works (company, code)`,
	`CREATE INDEX ix_wrk_works_parent ON wrk_works (parent)`,
	`CREATE TABLE wrk_work_assignments (work_id {uuid} NOT NULL, line_no {int} NOT NULL, person {uuid} NOT NULL, role_name {str:60},
	rate {str:30} NOT NULL, from_day {date} NOT NULL, until_day {date}, PRIMARY KEY (work_id, line_no),
	FOREIGN KEY (work_id) REFERENCES wrk_works (id))`,
	`CREATE TABLE wrk_work_history (work_id {uuid} NOT NULL, line_no {int} NOT NULL, status {str:20} NOT NULL, changed_on {date} NOT NULL,
	PRIMARY KEY (work_id, line_no), FOREIGN KEY (work_id) REFERENCES wrk_works (id))`,
	`CREATE TABLE wrk_time_entries (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, work_id {uuid} NOT NULL,
	person {uuid} NOT NULL, work_date {date} NOT NULL, hours {str:30} NOT NULL, rate {str:30} NOT NULL, billable {bool} NOT NULL,
	comment_text {str:500}, approved {bool} NOT NULL, ` + audit + `)`,
	`CREATE INDEX ix_wrk_time_work ON wrk_time_entries (work_id)`,
	`CREATE INDEX ix_wrk_time_person ON wrk_time_entries (company, person, work_date)`,
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
		{Version: 1, Name: "work, assignments, status history and time entries", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"wrk_time_entries", "wrk_work_history", "wrk_work_assignments", "wrk_works", TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

// WorkMapping maps Work to wrk_works, its assignments and its history.
func WorkMapping() sqlrepo.Mapping[domain.WorkID, *domain.Work] {
	return sqlrepo.Mapping[domain.WorkID, *domain.Work]{
		Table: "wrk_works",
		Columns: sqlrepo.WithAuditColumns("company", "code", "kind", "parent", "name", "description", "purpose", "customer", "facility", "asset",
			"planned_start", "planned_end", "estimated_hours", "budget", "status", "started", "finished", "cancel_reason", "hours", "cost"),
		Dehydrate: func(w *domain.Work) (sqlrepo.Values, error) {
			s := w.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "code": s.Code, "kind": string(s.Kind), "parent": optUUID(s.Parent.UUID),
				"name": s.Name, "description": opt(s.Description), "purpose": opt(s.Purpose), "customer": optUUID(s.Customer.UUID),
				"facility": optUUID(s.Facility), "asset": optUUID(s.Asset), "planned_start": optDate(s.PlannedStart), "planned_end": optDate(s.PlannedEnd),
				"estimated_hours": s.EstimatedHours.StringFixed(2), "budget": s.Budget.StringFixed(2), "status": string(s.Status),
				"started": optDate(s.Started), "finished": optDate(s.Finished), "cancel_reason": opt(s.CancelReason), "hours": s.Hours.StringFixed(2),
				"cost": s.Cost.StringFixed(2)}, w.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Work, error) {
			s := domain.WorkState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("code"), Kind: domain.Kind(r.String("kind")),
				Parent: domain.WorkID{UUID: r.UUID("parent")},
				Plan: domain.Plan{Name: r.String("name"), Description: r.String("description"), Purpose: r.String("purpose"),
					Customer: domain.PartyID{UUID: r.UUID("customer")}, Facility: r.UUID("facility"), Asset: r.UUID("asset"),
					PlannedStart: r.Date("planned_start"), PlannedEnd: r.Date("planned_end"), EstimatedHours: r.Decimal("estimated_hours"),
					Budget: r.Decimal("budget")},
				Status: domain.Status(r.String("status")), Started: r.Date("started"), Finished: r.Date("finished"), CancelReason: r.String("cancel_reason"),
				Hours: r.Decimal("hours"), Cost: r.Decimal("cost"), Audit: r.AuditStamp()}
			for _, c := range byNo(children.Of("assignments")) {
				s.Assignments = append(s.Assignments, domain.Assignment{Person: domain.PartyID{UUID: c.UUID("person")}, Role: c.String("role_name"),
					Rate: c.Decimal("rate"), From: c.Date("from_day"), Until: c.Date("until_day")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range byNo(children.Of("history")) {
				s.History = append(s.History, domain.Step{Status: domain.Status(c.String("status")), On: c.Date("changed_on")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteWork(domain.WorkID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Work]{{
			Name: "assignments", Table: "wrk_work_assignments", ForeignKey: "work_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "person", "role_name", "rate", "from_day", "until_day"},
			Dehydrate: func(w *domain.Work) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, a := range w.State().Assignments {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "person": a.Person, "role_name": opt(a.Role), "rate": a.Rate.StringFixed(2),
						"from_day": a.From, "until_day": optDate(a.Until)})
				}
				return out, nil
			},
		}, {
			Name: "history", Table: "wrk_work_history", ForeignKey: "work_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "status", "changed_on"},
			Dehydrate: func(w *domain.Work) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, h := range w.State().History {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "status": string(h.Status), "changed_on": h.On})
				}
				return out, nil
			},
		}},
	}
}

// TimeEntryMapping maps TimeEntry to wrk_time_entries.
func TimeEntryMapping() sqlrepo.Mapping[domain.TimeEntryID, *domain.TimeEntry] {
	return sqlrepo.Mapping[domain.TimeEntryID, *domain.TimeEntry]{
		Table:   "wrk_time_entries",
		Columns: sqlrepo.WithAuditColumns("company", "work_id", "person", "work_date", "hours", "rate", "billable", "comment_text", "approved"),
		Dehydrate: func(t *domain.TimeEntry) (sqlrepo.Values, error) {
			s := t.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"company": s.Company, "work_id": s.Work, "person": s.Person, "work_date": s.Date,
				"hours": s.Hours.StringFixed(2), "rate": s.Rate.StringFixed(2), "billable": s.Billable, "comment_text": opt(s.Comment),
				"approved": s.Approved}, t.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.TimeEntry, error) {
			s := domain.TimeEntryState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Work: domain.WorkID{UUID: r.UUID("work_id")},
				Person: domain.PartyID{UUID: r.UUID("person")}, Date: r.Date("work_date"), Hours: r.Decimal("hours"), Rate: r.Decimal("rate"),
				Billable: r.Bool("billable"), Comment: r.String("comment_text"), Approved: r.Bool("approved"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteTimeEntry(domain.TimeEntryID{UUID: r.UUID("id")}, s)
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("work: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// WorkRepositoryFactory builds the work repository.
func WorkRepositoryFactory(b hotswap.Backend) (domain.WorkRepository, error) {
	return repository(b, WorkMapping())
}

// TimeEntryRepositoryFactory builds the time entry repository.
func TimeEntryRepositoryFactory(b hotswap.Backend) (domain.TimeEntryRepository, error) {
	return repository(b, TimeEntryMapping())
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

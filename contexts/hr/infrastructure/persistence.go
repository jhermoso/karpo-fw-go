// Package infrastructure stores the Human Resources context: SQL mappings, versioned schema of the
// five engines (with the catalog seed of the C# WellKnownRRHHCatalog), catalogs, hot-swap
// factories and the adapters to Parties and Facilities.
package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/hr/domain"
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
const Context = "hr"

// Technical tables of the context.
const (
	TableOutbox            = "hr_outbox"
	TableIntegrationOutbox = "hr_integration_outbox"
	TableAuditLog          = "hr_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var catalogDDL = []string{
	`CREATE TABLE hr_position_statuses (id {uuid} NOT NULL PRIMARY KEY, name {str:100} NOT NULL, description {str:500}, active {bool} NOT NULL)`,
	`CREATE TABLE hr_position_classes (id {uuid} NOT NULL PRIMARY KEY, name {str:100} NOT NULL, active {bool} NOT NULL)`,
	`CREATE TABLE hr_position_types (id {uuid} NOT NULL PRIMARY KEY, title {str:200} NOT NULL, description {str:500}, active {bool} NOT NULL)`,
	`CREATE TABLE hr_position_type_classes (position_type {uuid} NOT NULL, position_class {uuid} NOT NULL, standard_weekly_hours {str:20},
	PRIMARY KEY (position_type, position_class), FOREIGN KEY (position_type) REFERENCES hr_position_types (id),
	FOREIGN KEY (position_class) REFERENCES hr_position_classes (id))`,
	`CREATE TABLE hr_agreements (id {uuid} NOT NULL PRIMARY KEY, code {str:60} NOT NULL, name {str:200} NOT NULL, scope {int} NOT NULL,
	organization {uuid}, territorial_code {str:20}, sectoral_code {str:20}, start_date {date} NOT NULL, end_date {date}, active {bool} NOT NULL)`,
}

var schemaDDL = []string{
	`CREATE TABLE hr_positions (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, unit {uuid} NOT NULL, organization {uuid} NOT NULL,
	position_type {uuid} NOT NULL, status {uuid} NOT NULL, planned_from {ts} NOT NULL, planned_to {ts}, is_salaried {bool} NOT NULL,
	is_exempt {bool} NOT NULL, is_full_time {bool} NOT NULL, is_temporary {bool} NOT NULL, ` + audit + `,
	FOREIGN KEY (position_type) REFERENCES hr_position_types (id), FOREIGN KEY (status) REFERENCES hr_position_statuses (id))`,
	`CREATE INDEX ix_hr_positions_org ON hr_positions (organization)`,
	`CREATE INDEX ix_hr_positions_unit ON hr_positions (unit)`,
	`CREATE TABLE hr_position_holders (id {uuid} NOT NULL PRIMARY KEY, position_id {uuid} NOT NULL, holder {uuid} NOT NULL,
	valid_from {ts} NOT NULL, valid_to {ts}, FOREIGN KEY (position_id) REFERENCES hr_positions (id))`,
	`CREATE INDEX ix_hr_position_holders_pos ON hr_position_holders (position_id)`,
	`CREATE INDEX ix_hr_position_holders_holder ON hr_position_holders (holder)`,
	`CREATE TABLE hr_position_reporting (id {uuid} NOT NULL PRIMARY KEY, position_id {uuid} NOT NULL, supervisor {uuid} NOT NULL,
	is_primary {bool} NOT NULL, valid_from {ts} NOT NULL, valid_to {ts}, FOREIGN KEY (position_id) REFERENCES hr_positions (id),
	FOREIGN KEY (supervisor) REFERENCES hr_positions (id))`,
	`CREATE INDEX ix_hr_position_reporting_pos ON hr_position_reporting (position_id)`,
	`CREATE INDEX ix_hr_position_reporting_sup ON hr_position_reporting (supervisor)`,
	`CREATE TABLE hr_work_centers (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, employer {uuid} NOT NULL, facility {uuid} NOT NULL,
	code {str:60} NOT NULL, headquarters {bool} NOT NULL, opened_on {date} NOT NULL, closed_on {date}, open_flag {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_hr_work_centers_code ON hr_work_centers (employer, code)`,
	`CREATE TABLE hr_employments (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, person {uuid} NOT NULL, employer {uuid} NOT NULL,
	employee_number {str:50}, hired_on {date} NOT NULL, terminated_on {date}, terminated_flag {bool} NOT NULL, termination_reason {str:30},
	job_category {str:20}, wage_group {str:20}, ` + audit + `)`,
	`CREATE INDEX ix_hr_employments_person ON hr_employments (person)`,
	`CREATE INDEX ix_hr_employments_employer ON hr_employments (employer)`,
	`CREATE TABLE hr_contracts (id {uuid} NOT NULL PRIMARY KEY, employment_id {uuid} NOT NULL, type_code {str:20} NOT NULL,
	contract_number {str:30}, start_date {date} NOT NULL, end_date {date}, agreement {uuid} NOT NULL, work_center {uuid} NOT NULL,
	weekly_hours {str:20}, bonified {bool} NOT NULL, is_primary {bool} NOT NULL, termination_reason {str:30},
	FOREIGN KEY (employment_id) REFERENCES hr_employments (id), FOREIGN KEY (agreement) REFERENCES hr_agreements (id),
	FOREIGN KEY (work_center) REFERENCES hr_work_centers (id))`,
	`CREATE INDEX ix_hr_contracts_employment ON hr_contracts (employment_id)`,
	`CREATE INDEX ix_hr_contracts_work_center ON hr_contracts (work_center)`,
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

// Migrations is the versioned schema of the context. The catalogs are seeded with the C# GUIDs;
// positions, employments and work centers are business data.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "catalogs", Up: sqlrepo.RenderDDLAll(catalogDDL...)},
		{Version: 2, Name: "positions, work centers and employments", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 3, Name: "outboxes and audit log", Up: technical},
		{Version: 4, Name: "catalog seed", Run: seed},
	}}
}

func seed(ctx context.Context, db *sqlrepo.DB) error {
	var statuses, classes, types, typeClasses, agreements [][]any
	for _, s := range domain.WellKnownPositionStatuses() {
		statuses = append(statuses, []any{s.ID, s.Name, opt(s.Description), s.Active})
	}
	for _, c := range domain.WellKnownPositionClasses() {
		classes = append(classes, []any{c.ID, c.Name, c.Active})
	}
	for _, t := range domain.WellKnownPositionTypes() {
		types = append(types, []any{t.ID, t.Title, opt(t.Description), t.Active})
		for _, c := range t.Classes {
			typeClasses = append(typeClasses, []any{t.ID, c.Class, optDecimal(c.StandardWeeklyHours)})
		}
	}
	for _, a := range domain.WellKnownAgreements() {
		agreements = append(agreements, []any{a.ID, a.Code, a.Name, int64(a.Scope), optUUID(a.Organization.UUID), opt(a.TerritorialCode),
			opt(a.SectoralCode), a.Start, optDate(a.End), a.Active})
	}
	for _, t := range []struct {
		table string
		cols  []string
		rows  [][]any
	}{
		{"hr_position_statuses", []string{"id", "name", "description", "active"}, statuses},
		{"hr_position_classes", []string{"id", "name", "active"}, classes},
		{"hr_position_types", []string{"id", "title", "description", "active"}, types},
		{"hr_position_type_classes", []string{"position_type", "position_class", "standard_weekly_hours"}, typeClasses},
		{"hr_agreements", []string{"id", "code", "name", "scope", "organization", "territorial_code", "sectoral_code", "start_date",
			"end_date", "active"}, agreements},
	} {
		if err := db.InsertMany(ctx, t.table, t.cols, t.rows); err != nil {
			return err
		}
	}
	return nil
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"hr_contracts", "hr_employments", "hr_work_centers", "hr_position_reporting", "hr_position_holders", "hr_positions",
	"hr_agreements", "hr_position_type_classes", "hr_position_types", "hr_position_classes", "hr_position_statuses",
	TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

func optDecimal(d vocab.Decimal) any {
	if d.IsZero() {
		return nil
	}
	return d.String()
}

func optTime(p vocab.ValidPeriod) any {
	if t, ok := p.To(); ok {
		return t
	}
	return nil
}

// byStart orders children by start and then identity, the same on every engine (SQL Server sorts
// UNIQUEIDENTIFIER by byte groups).
func byStart(a, b time.Time, ia, ib []byte) int {
	if c := a.Compare(b); c != 0 {
		return c
	}
	return bytes.Compare(ia, ib)
}

// PositionMapping maps Position to hr_positions and its children.
func PositionMapping() sqlrepo.Mapping[domain.PositionID, *domain.Position] {
	return sqlrepo.Mapping[domain.PositionID, *domain.Position]{
		Table: "hr_positions",
		Columns: sqlrepo.WithAuditColumns("unit", "organization", "position_type", "status", "planned_from", "planned_to", "is_salaried",
			"is_exempt", "is_full_time", "is_temporary"),
		Dehydrate: func(p *domain.Position) (sqlrepo.Values, error) {
			f := p.Flags()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"unit": p.Unit(), "organization": p.Organization(), "position_type": p.Type(),
				"status": p.Status(), "planned_from": p.Planned().From(), "planned_to": optTime(p.Planned()), "is_salaried": f.Salaried,
				"is_exempt": f.Exempt, "is_full_time": f.FullTime, "is_temporary": f.Temporary}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Position, error) {
			planned, err := vocab.NewValidPeriod(r.Time("planned_from"), r.NullTime("planned_to"))
			if err != nil {
				return nil, err
			}
			s := domain.PositionState{Unit: domain.OrganizationID{UUID: r.UUID("unit")}, Organization: domain.OrganizationID{UUID: r.UUID("organization")},
				Type: domain.PositionTypeID{UUID: r.UUID("position_type")}, Status: domain.PositionStatusID{UUID: r.UUID("status")},
				Planned: planned, Flags: domain.Flags{Salaried: r.Bool("is_salaried"), Exempt: r.Bool("is_exempt"),
					FullTime: r.Bool("is_full_time"), Temporary: r.Bool("is_temporary")}, Audit: r.AuditStamp()}
			for _, c := range children.Of("holders") {
				period, err := vocab.NewValidPeriod(c.Time("valid_from"), c.NullTime("valid_to"))
				if err != nil {
					return nil, err
				}
				s.Holders = append(s.Holders, domain.Fulfillment{ID: c.UUID("id"), Holder: domain.PersonID{UUID: c.UUID("holder")}, Period: period})
			}
			for _, c := range children.Of("reports_to") {
				period, err := vocab.NewValidPeriod(c.Time("valid_from"), c.NullTime("valid_to"))
				if err != nil {
					return nil, err
				}
				s.ReportsTo = append(s.ReportsTo, domain.ReportingLine{ID: c.UUID("id"), Supervisor: domain.PositionID{UUID: c.UUID("supervisor")},
					Primary: c.Bool("is_primary"), Period: period})
			}
			slices.SortFunc(s.Holders, func(a, b domain.Fulfillment) int {
				return byStart(a.Period.From(), b.Period.From(), a.ID.Bytes(), b.ID.Bytes())
			})
			slices.SortFunc(s.ReportsTo, func(a, b domain.ReportingLine) int {
				return byStart(a.Period.From(), b.Period.From(), a.ID.Bytes(), b.ID.Bytes())
			})
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstitutePosition(domain.PositionID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Position]{{
			Name: "holders", Table: "hr_position_holders", ForeignKey: "position_id",
			Columns: []string{"id", "holder", "valid_from", "valid_to"}, OrderBy: []string{"valid_from", "id"},
			Dehydrate: func(p *domain.Position) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, h := range p.Holders() {
					out = append(out, sqlrepo.Values{"id": h.ID, "holder": h.Holder, "valid_from": h.Period.From(), "valid_to": optTime(h.Period)})
				}
				return out, nil
			},
		}, {
			Name: "reports_to", Table: "hr_position_reporting", ForeignKey: "position_id",
			Columns: []string{"id", "supervisor", "is_primary", "valid_from", "valid_to"}, OrderBy: []string{"valid_from", "id"},
			Dehydrate: func(p *domain.Position) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range p.ReportsTo() {
					out = append(out, sqlrepo.Values{"id": l.ID, "supervisor": l.Supervisor, "is_primary": l.Primary,
						"valid_from": l.Period.From(), "valid_to": optTime(l.Period)})
				}
				return out, nil
			},
		}},
	}
}

// EmploymentMapping maps Employment to hr_employments and its contracts.
func EmploymentMapping() sqlrepo.Mapping[domain.EmploymentID, *domain.Employment] {
	return sqlrepo.Mapping[domain.EmploymentID, *domain.Employment]{
		Table: "hr_employments",
		Columns: sqlrepo.WithAuditColumns("person", "employer", "employee_number", "hired_on", "terminated_on", "terminated_flag",
			"termination_reason", "job_category", "wage_group"),
		Dehydrate: func(e *domain.Employment) (sqlrepo.Values, error) {
			return sqlrepo.AuditStampValues(sqlrepo.Values{"person": e.Person(), "employer": e.Employer(), "employee_number": opt(e.Number()),
				"hired_on": e.Hired(), "terminated_on": optDate(e.Terminated()), "terminated_flag": !e.Terminated().IsZero(),
				"termination_reason": opt(e.TerminationReason()), "job_category": opt(e.JobCategory()), "wage_group": opt(e.WageGroup())},
				e.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Employment, error) {
			s := domain.EmploymentState{Person: domain.PersonID{UUID: r.UUID("person")}, Employer: domain.OrganizationID{UUID: r.UUID("employer")},
				Number: r.String("employee_number"), Hired: r.Date("hired_on"), Terminated: r.Date("terminated_on"),
				Reason: r.String("termination_reason"), JobCategory: r.String("job_category"), WageGroup: r.String("wage_group"), Audit: r.AuditStamp()}
			for _, c := range children.Of("contracts") {
				s.Contracts = append(s.Contracts, domain.Contract{ID: domain.ContractID{UUID: c.UUID("id")}, TypeCode: c.String("type_code"),
					Number: c.String("contract_number"), Start: c.Date("start_date"), End: c.Date("end_date"),
					Agreement: domain.AgreementID{UUID: c.UUID("agreement")}, WorkCenter: domain.WorkCenterID{UUID: c.UUID("work_center")},
					WeeklyHours: c.Decimal("weekly_hours"), Bonified: c.Bool("bonified"), Primary: c.Bool("is_primary"),
					TerminationReason: c.String("termination_reason")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(s.Contracts, func(a, b domain.Contract) int {
				return byStart(a.Start.BaseTime(), b.Start.BaseTime(), a.ID.Bytes(), b.ID.Bytes())
			})
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteEmployment(domain.EmploymentID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Employment]{{
			Name: "contracts", Table: "hr_contracts", ForeignKey: "employment_id",
			Columns: []string{"id", "type_code", "contract_number", "start_date", "end_date", "agreement", "work_center", "weekly_hours",
				"bonified", "is_primary", "termination_reason"},
			OrderBy: []string{"start_date", "id"},
			Dehydrate: func(e *domain.Employment) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, c := range e.Contracts() {
					out = append(out, sqlrepo.Values{"id": c.ID, "type_code": c.TypeCode, "contract_number": opt(c.Number), "start_date": c.Start,
						"end_date": optDate(c.End), "agreement": c.Agreement, "work_center": c.WorkCenter, "weekly_hours": optDecimal(c.WeeklyHours),
						"bonified": c.Bonified, "is_primary": c.Primary, "termination_reason": opt(c.TerminationReason)})
				}
				return out, nil
			},
		}},
	}
}

// WorkCenterMapping maps WorkCenter to hr_work_centers.
func WorkCenterMapping() sqlrepo.Mapping[domain.WorkCenterID, *domain.WorkCenter] {
	return sqlrepo.Mapping[domain.WorkCenterID, *domain.WorkCenter]{
		Table:   "hr_work_centers",
		Columns: sqlrepo.WithAuditColumns("employer", "facility", "code", "headquarters", "opened_on", "closed_on", "open_flag"),
		Dehydrate: func(w *domain.WorkCenter) (sqlrepo.Values, error) {
			return sqlrepo.AuditStampValues(sqlrepo.Values{"employer": w.Employer(), "facility": w.Facility(), "code": w.Code(),
				"headquarters": w.IsHeadquarters(), "opened_on": w.Opened(), "closed_on": optDate(w.Closed()), "open_flag": w.IsOpen()},
				w.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.WorkCenter, error) {
			s := domain.WorkCenterState{Employer: domain.OrganizationID{UUID: r.UUID("employer")}, Facility: domain.FacilityID{UUID: r.UUID("facility")},
				Code: r.String("code"), Headquarters: r.Bool("headquarters"), Opened: r.Date("opened_on"), Closed: r.Date("closed_on"),
				Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteWorkCenter(domain.WorkCenterID{UUID: r.UUID("id")}, s)
		},
	}
}

// SQLCatalogs reads the catalogs from the database.
type SQLCatalogs struct{ db *sqlrepo.DB }

// PositionStatuses implements domain.Catalogs.
func (c SQLCatalogs) PositionStatuses(ctx context.Context) ([]domain.PositionStatus, error) {
	rows, err := c.db.Select(ctx, "hr_position_statuses", []string{"id", "name", "description", "active"}, "id")
	if err != nil {
		return nil, err
	}
	out := make([]domain.PositionStatus, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.PositionStatus{ID: domain.PositionStatusID{UUID: r.UUID("id")}, Name: r.String("name"),
			Description: r.String("description"), Active: r.Bool("active")})
		if err := r.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PositionClasses implements domain.Catalogs.
func (c SQLCatalogs) PositionClasses(ctx context.Context) ([]domain.PositionClass, error) {
	rows, err := c.db.Select(ctx, "hr_position_classes", []string{"id", "name", "active"}, "name")
	if err != nil {
		return nil, err
	}
	out := make([]domain.PositionClass, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.PositionClass{ID: domain.PositionClassID{UUID: r.UUID("id")}, Name: r.String("name"), Active: r.Bool("active")})
		if err := r.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PositionTypes implements domain.Catalogs.
func (c SQLCatalogs) PositionTypes(ctx context.Context) ([]domain.PositionType, error) {
	rows, err := c.db.Select(ctx, "hr_position_types", []string{"id", "title", "description", "active"}, "title")
	if err != nil {
		return nil, err
	}
	links, err := c.db.Select(ctx, "hr_position_type_classes", []string{"position_type", "position_class", "standard_weekly_hours"}, "position_type")
	if err != nil {
		return nil, err
	}
	classes := map[fw.UUID][]domain.PositionTypeClass{}
	for _, l := range links {
		t := l.UUID("position_type")
		classes[t] = append(classes[t], domain.PositionTypeClass{Class: domain.PositionClassID{UUID: l.UUID("position_class")},
			StandardWeeklyHours: l.Decimal("standard_weekly_hours")})
		if err := l.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]domain.PositionType, 0, len(rows))
	for _, r := range rows {
		id := r.UUID("id")
		cs := classes[id]
		slices.SortFunc(cs, func(a, b domain.PositionTypeClass) int { return bytes.Compare(a.Class.Bytes(), b.Class.Bytes()) })
		out = append(out, domain.PositionType{ID: domain.PositionTypeID{UUID: id}, Title: r.String("title"), Description: r.String("description"),
			Active: r.Bool("active"), Classes: append([]domain.PositionTypeClass{}, cs...)})
		if err := r.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Agreements implements domain.Catalogs.
func (c SQLCatalogs) Agreements(ctx context.Context) ([]domain.Agreement, error) {
	rows, err := c.db.Select(ctx, "hr_agreements", []string{"id", "code", "name", "scope", "organization", "territorial_code", "sectoral_code",
		"start_date", "end_date", "active"}, "code")
	if err != nil {
		return nil, err
	}
	out := make([]domain.Agreement, 0, len(rows))
	for _, r := range rows {
		a := domain.Agreement{ID: domain.AgreementID{UUID: r.UUID("id")}, Code: r.String("code"), Name: r.String("name"),
			Scope: domain.AgreementScope(r.Int64("scope")), TerritorialCode: r.String("territorial_code"), SectoralCode: r.String("sectoral_code"),
			Start: r.Date("start_date"), End: r.Date("end_date"), Active: r.Bool("active")}
		if !r.IsNull("organization") {
			a.Organization = domain.OrganizationID{UUID: r.UUID("organization")}
		}
		if err := r.Err(); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// WellKnownCatalogs serves the seed (in-memory backend).
type WellKnownCatalogs struct{}

// PositionStatuses implements domain.Catalogs.
func (WellKnownCatalogs) PositionStatuses(context.Context) ([]domain.PositionStatus, error) {
	return domain.WellKnownPositionStatuses(), nil
}

// PositionClasses implements domain.Catalogs.
func (WellKnownCatalogs) PositionClasses(context.Context) ([]domain.PositionClass, error) {
	return domain.WellKnownPositionClasses(), nil
}

// PositionTypes implements domain.Catalogs.
func (WellKnownCatalogs) PositionTypes(context.Context) ([]domain.PositionType, error) {
	return domain.WellKnownPositionTypes(), nil
}

// Agreements implements domain.Catalogs.
func (WellKnownCatalogs) Agreements(context.Context) ([]domain.Agreement, error) {
	return domain.WellKnownAgreements(), nil
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("hr: unsupported backend %T", b) }

// PositionRepositoryFactory builds the position repository.
func PositionRepositoryFactory(b hotswap.Backend) (domain.PositionRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, PositionMapping())
	case *memory.Store:
		return memory.NewRepository[domain.PositionID, *domain.Position](db), nil
	}
	return nil, unsupported(b)
}

// EmploymentRepositoryFactory builds the employment repository.
func EmploymentRepositoryFactory(b hotswap.Backend) (domain.EmploymentRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, EmploymentMapping())
	case *memory.Store:
		return memory.NewRepository[domain.EmploymentID, *domain.Employment](db), nil
	}
	return nil, unsupported(b)
}

// WorkCenterRepositoryFactory builds the work center repository.
func WorkCenterRepositoryFactory(b hotswap.Backend) (domain.WorkCenterRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, WorkCenterMapping())
	case *memory.Store:
		return memory.NewRepository[domain.WorkCenterID, *domain.WorkCenter](db), nil
	}
	return nil, unsupported(b)
}

// CatalogsFor returns the catalog reader of a backend.
func CatalogsFor(b hotswap.Backend) (domain.Catalogs, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return SQLCatalogs{db: db}, nil
	case *memory.Store:
		return WellKnownCatalogs{}, nil
	}
	return nil, unsupported(b)
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

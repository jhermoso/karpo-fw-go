// Package infrastructure stores the Payroll context: SQL mappings, versioned schema of the five
// engines (with the pay concept seed), the catalog reader, hot-swap factories and the HR adapter.
package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	hr "github.com/jhermoso/karpo-fw-go/contexts/hr/contracts"
	papp "github.com/jhermoso/karpo-fw-go/contexts/payroll/application"
	"github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
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
const Context = "payroll"

// Technical tables of the context.
const (
	TableOutbox            = "payroll_outbox"
	TableIntegrationOutbox = "payroll_integration_outbox"
	TableAuditLog          = "payroll_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE pay_concepts (id {uuid} NOT NULL PRIMARY KEY, code {str:20} NOT NULL, name {str:100} NOT NULL, kind {int} NOT NULL,
	contributable {bool} NOT NULL, taxable {bool} NOT NULL, perception_key {str:1}, active {bool} NOT NULL)`,
	`CREATE UNIQUE INDEX ux_pay_concepts_code ON pay_concepts (code)`,
	`CREATE TABLE pay_employer_accounts (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, employer {uuid} NOT NULL,
	code {str:11} NOT NULL, regime {str:4} NOT NULL, activity {str:5}, method {int} NOT NULL, iban {str:34}, active {bool} NOT NULL, ` + audit + `)`,
	`CREATE INDEX ix_pay_employer_accounts_employer ON pay_employer_accounts (employer)`,
	`CREATE INDEX ix_pay_employer_accounts_code ON pay_employer_accounts (code)`,
	`CREATE TABLE pay_profiles (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, employment {uuid} NOT NULL, person {uuid} NOT NULL,
	employer {uuid} NOT NULL, contribution_group {int} NOT NULL, income_tax_rate {str:20} NOT NULL, salary_amount {str:30},
	salary_periodicity {int}, payments_per_year {int}, employer_account {uuid}, ` + audit + `,
	FOREIGN KEY (employer_account) REFERENCES pay_employer_accounts (id))`,
	`CREATE UNIQUE INDEX ux_pay_profiles_employment ON pay_profiles (employment)`,
	`CREATE INDEX ix_pay_profiles_employer ON pay_profiles (employer)`,
	`CREATE TABLE pay_profile_splits (id {uuid} NOT NULL PRIMARY KEY, profile_id {uuid} NOT NULL, iban {str:34} NOT NULL,
	split_percent {str:20}, split_amount {str:30}, is_residual {bool} NOT NULL, split_priority {int} NOT NULL, garnishment {bool} NOT NULL,
	valid_from {date} NOT NULL, valid_to {date}, FOREIGN KEY (profile_id) REFERENCES pay_profiles (id))`,
	`CREATE INDEX ix_pay_profile_splits_profile ON pay_profile_splits (profile_id)`,
	`CREATE TABLE pay_payslips (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, employment {uuid} NOT NULL, person {uuid} NOT NULL,
	employer {uuid} NOT NULL, kind {int} NOT NULL, period_start {date} NOT NULL, period_end {date} NOT NULL, payment_date {date} NOT NULL,
	status {int} NOT NULL, employee_number {str:50}, agreement {uuid}, work_center {uuid}, contribution_group {int} NOT NULL,
	income_tax_rate {str:20}, employer_account {uuid}, gross {str:30} NOT NULL, net {str:30} NOT NULL, cancel_reason {str:200}, ` + audit + `)`,
	`CREATE INDEX ix_pay_payslips_employment ON pay_payslips (employment, kind, period_start)`,
	`CREATE INDEX ix_pay_payslips_employer ON pay_payslips (employer, period_start)`,
	`CREATE INDEX ix_pay_payslips_person ON pay_payslips (person)`,
	`CREATE TABLE pay_payslip_lines (id {uuid} NOT NULL PRIMARY KEY, payslip_id {uuid} NOT NULL, concept {uuid} NOT NULL, code {str:20} NOT NULL,
	kind {int} NOT NULL, contributable {bool} NOT NULL, taxable {bool} NOT NULL, perception_key {str:1}, description {str:200} NOT NULL,
	quantity {str:30}, unit_amount {str:30}, base_amount {str:30}, rate_percent {str:20}, amount {str:30} NOT NULL,
	FOREIGN KEY (payslip_id) REFERENCES pay_payslips (id), FOREIGN KEY (concept) REFERENCES pay_concepts (id))`,
	`CREATE INDEX ix_pay_payslip_lines_payslip ON pay_payslip_lines (payslip_id)`,
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
		{Version: 1, Name: "concepts, employer accounts, profiles and payslips", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
		{Version: 3, Name: "pay concept seed", Run: func(ctx context.Context, db *sqlrepo.DB) error {
			var rows [][]any
			for _, c := range domain.WellKnownConcepts() {
				rows = append(rows, []any{c.ID, c.Code, c.Name, int64(c.Kind), c.Contributable, c.Taxable, opt(c.PerceptionKey), c.Active})
			}
			return db.InsertMany(ctx, "pay_concepts", []string{"id", "code", "name", "kind", "contributable", "taxable", "perception_key", "active"}, rows)
		}},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"pay_payslip_lines", "pay_payslips", "pay_profile_splits", "pay_profiles", "pay_employer_accounts", "pay_concepts",
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

func iban(r *sqlrepo.Row, col string) (vocab.IBAN, error) {
	if s := r.String(col); s != "" {
		return vocab.NewIBAN(s)
	}
	return vocab.IBAN{}, nil
}

// PayslipMapping maps Payslip to pay_payslips and its lines.
func PayslipMapping() sqlrepo.Mapping[domain.PayslipID, *domain.Payslip] {
	return sqlrepo.Mapping[domain.PayslipID, *domain.Payslip]{
		Table: "pay_payslips",
		Columns: sqlrepo.WithAuditColumns("employment", "person", "employer", "kind", "period_start", "period_end", "payment_date", "status",
			"employee_number", "agreement", "work_center", "contribution_group", "income_tax_rate", "employer_account", "gross", "net", "cancel_reason"),
		Dehydrate: func(p *domain.Payslip) (sqlrepo.Values, error) {
			start, end := p.Period()
			sn := p.Snapshot()
			t := p.Totals()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"employment": p.Employment(), "person": p.Person(), "employer": p.Employer(),
				"kind": int64(p.Kind()), "period_start": start, "period_end": end, "payment_date": p.PaymentDate(), "status": int64(p.Status()),
				"employee_number": opt(sn.EmployeeNumber), "agreement": optUUID(sn.Agreement.UUID), "work_center": optUUID(sn.WorkCenter.UUID),
				"contribution_group": int64(sn.ContributionGroup), "income_tax_rate": optDecimal(sn.IncomeTaxRate),
				"employer_account": optUUID(sn.EmployerAccount.UUID), "gross": t.Gross.StringFixed(2), "net": t.Net.StringFixed(2),
				"cancel_reason": opt(p.CancelReason())}, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Payslip, error) {
			s := domain.PayslipState{Employment: domain.EmploymentID{UUID: r.UUID("employment")}, Person: domain.PersonID{UUID: r.UUID("person")},
				Employer: domain.OrganizationID{UUID: r.UUID("employer")}, Kind: domain.Kind(r.Int64("kind")), Start: r.Date("period_start"),
				End: r.Date("period_end"), PaymentDate: r.Date("payment_date"), Status: domain.Status(r.Int64("status")),
				CancelReason: r.String("cancel_reason"), Audit: r.AuditStamp(),
				Snapshot: domain.Snapshot{EmployeeNumber: r.String("employee_number"), Agreement: domain.AgreementID{UUID: r.UUID("agreement")},
					WorkCenter: domain.WorkCenterID{UUID: r.UUID("work_center")}, ContributionGroup: int(r.Int64("contribution_group")),
					IncomeTaxRate: r.Decimal("income_tax_rate"), EmployerAccount: domain.EmployerAccountID{UUID: r.UUID("employer_account")}}}
			for _, c := range children.Of("lines") {
				s.Lines = append(s.Lines, domain.Line{ID: domain.LineID{UUID: c.UUID("id")}, Concept: domain.ConceptID{UUID: c.UUID("concept")},
					Code: c.String("code"), Kind: domain.ConceptKind(c.Int64("kind")), Contributable: c.Bool("contributable"), Taxable: c.Bool("taxable"),
					PerceptionKey: c.String("perception_key"), Description: c.String("description"), Quantity: c.Decimal("quantity"),
					UnitAmount: c.Decimal("unit_amount"), Base: c.Decimal("base_amount"), Percent: c.Decimal("rate_percent"), Amount: c.Decimal("amount")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			// UUID v7 ids are chronological: the byte order is the order the lines were added,
			// on every engine (SQL Server sorts UNIQUEIDENTIFIER by byte groups).
			slices.SortFunc(s.Lines, func(a, b domain.Line) int { return bytes.Compare(a.ID.Bytes(), b.ID.Bytes()) })
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstitutePayslip(domain.PayslipID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Payslip]{{
			Name: "lines", Table: "pay_payslip_lines", ForeignKey: "payslip_id",
			Columns: []string{"id", "concept", "code", "kind", "contributable", "taxable", "perception_key", "description", "quantity",
				"unit_amount", "base_amount", "rate_percent", "amount"},
			OrderBy: []string{"id"},
			Dehydrate: func(p *domain.Payslip) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, l := range p.Lines() {
					out = append(out, sqlrepo.Values{"id": l.ID, "concept": l.Concept, "code": l.Code, "kind": int64(l.Kind),
						"contributable": l.Contributable, "taxable": l.Taxable, "perception_key": opt(l.PerceptionKey), "description": l.Description,
						"quantity": optDecimal(l.Quantity), "unit_amount": optDecimal(l.UnitAmount), "base_amount": optDecimal(l.Base),
						"rate_percent": optDecimal(l.Percent), "amount": l.Amount.StringFixed(2)})
				}
				return out, nil
			},
		}},
	}
}

// ProfileMapping maps Profile to pay_profiles and its splits.
func ProfileMapping() sqlrepo.Mapping[domain.ProfileID, *domain.Profile] {
	return sqlrepo.Mapping[domain.ProfileID, *domain.Profile]{
		Table: "pay_profiles",
		Columns: sqlrepo.WithAuditColumns("employment", "person", "employer", "contribution_group", "income_tax_rate", "salary_amount",
			"salary_periodicity", "payments_per_year", "employer_account"),
		Dehydrate: func(p *domain.Profile) (sqlrepo.Values, error) {
			t := p.Terms()
			v := sqlrepo.Values{"employment": p.Employment(), "person": p.Person(), "employer": p.Employer(),
				"contribution_group": int64(t.ContributionGroup), "income_tax_rate": t.IncomeTaxRate.String(), "salary_amount": nil,
				"salary_periodicity": nil, "payments_per_year": nil, "employer_account": optUUID(t.Account.UUID)}
			if !t.Salary.IsZero() {
				v["salary_amount"], v["salary_periodicity"], v["payments_per_year"] = t.Salary.Amount.String(), int64(t.Salary.Periodicity),
					int64(t.Salary.PaymentsPerYear)
			}
			return sqlrepo.AuditStampValues(v, p.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Profile, error) {
			s := domain.ProfileState{Employment: domain.EmploymentID{UUID: r.UUID("employment")}, Person: domain.PersonID{UUID: r.UUID("person")},
				Employer: domain.OrganizationID{UUID: r.UUID("employer")}, ContributionGroup: int(r.Int64("contribution_group")),
				IncomeTaxRate: r.Decimal("income_tax_rate"), Account: domain.EmployerAccountID{UUID: r.UUID("employer_account")}, Audit: r.AuditStamp()}
			if !r.IsNull("salary_amount") {
				s.Salary = domain.Salary{Amount: r.Decimal("salary_amount"), Periodicity: domain.Periodicity(r.Int64("salary_periodicity")),
					PaymentsPerYear: int(r.Int64("payments_per_year"))}
			}
			for _, c := range children.Of("splits") {
				i, err := iban(c, "iban")
				if err != nil {
					return nil, err
				}
				s.Splits = append(s.Splits, domain.Split{ID: domain.SplitID{UUID: c.UUID("id")}, IBAN: i, Percent: c.Decimal("split_percent"),
					Amount: c.Decimal("split_amount"), Residual: c.Bool("is_residual"), Priority: int(c.Int64("split_priority")),
					Garnishment: c.Bool("garnishment"), From: c.Date("valid_from"), Until: c.Date("valid_to")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(s.Splits, func(a, b domain.Split) int { return bytes.Compare(a.ID.Bytes(), b.ID.Bytes()) })
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteProfile(domain.ProfileID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Profile]{{
			Name: "splits", Table: "pay_profile_splits", ForeignKey: "profile_id",
			Columns: []string{"id", "iban", "split_percent", "split_amount", "is_residual", "split_priority", "garnishment", "valid_from", "valid_to"},
			OrderBy: []string{"id"},
			Dehydrate: func(p *domain.Profile) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, s := range p.Splits() {
					out = append(out, sqlrepo.Values{"id": s.ID, "iban": s.IBAN.String(), "split_percent": optDecimal(s.Percent),
						"split_amount": optDecimal(s.Amount), "is_residual": s.Residual, "split_priority": int64(s.Priority),
						"garnishment": s.Garnishment, "valid_from": s.From, "valid_to": optDate(s.Until)})
				}
				return out, nil
			},
		}},
	}
}

// AccountMapping maps EmployerAccount to pay_employer_accounts.
func AccountMapping() sqlrepo.Mapping[domain.EmployerAccountID, *domain.EmployerAccount] {
	return sqlrepo.Mapping[domain.EmployerAccountID, *domain.EmployerAccount]{
		Table:   "pay_employer_accounts",
		Columns: sqlrepo.WithAuditColumns("employer", "code", "regime", "activity", "method", "iban", "active"),
		Dehydrate: func(a *domain.EmployerAccount) (sqlrepo.Values, error) {
			return sqlrepo.AuditStampValues(sqlrepo.Values{"employer": a.Employer(), "code": a.Code(), "regime": a.Regime(),
				"activity": opt(a.Activity()), "method": int64(a.Method()), "iban": opt(a.IBAN().String()), "active": a.IsActive()}, a.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.EmployerAccount, error) {
			i, err := iban(r, "iban")
			if err != nil {
				return nil, err
			}
			s := domain.EmployerAccountState{Employer: domain.OrganizationID{UUID: r.UUID("employer")}, Code: r.String("code"),
				Regime: r.String("regime"), Activity: r.String("activity"), Method: domain.PaymentMethod(r.Int64("method")), IBAN: i,
				Active: r.Bool("active"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteEmployerAccount(domain.EmployerAccountID{UUID: r.UUID("id")}, s)
		},
	}
}

// SQLCatalogs reads the concept catalog from the database.
type SQLCatalogs struct{ db *sqlrepo.DB }

// Concepts implements domain.Catalogs.
func (c SQLCatalogs) Concepts(ctx context.Context) ([]domain.Concept, error) {
	rows, err := c.db.Select(ctx, "pay_concepts", []string{"id", "code", "name", "kind", "contributable", "taxable", "perception_key", "active"}, "code")
	if err != nil {
		return nil, err
	}
	out := make([]domain.Concept, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.Concept{ID: domain.ConceptID{UUID: r.UUID("id")}, Code: r.String("code"), Name: r.String("name"),
			Kind: domain.ConceptKind(r.Int64("kind")), Contributable: r.Bool("contributable"), Taxable: r.Bool("taxable"),
			PerceptionKey: r.String("perception_key"), Active: r.Bool("active")})
		if err := r.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// WellKnownCatalogs serves the seed (in-memory backend).
type WellKnownCatalogs struct{}

// Concepts implements domain.Catalogs.
func (WellKnownCatalogs) Concepts(context.Context) ([]domain.Concept, error) {
	return domain.WellKnownConcepts(), nil
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("payroll: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// PayslipRepositoryFactory builds the payslip repository.
func PayslipRepositoryFactory(b hotswap.Backend) (domain.PayslipRepository, error) {
	return repository(b, PayslipMapping())
}

// ProfileRepositoryFactory builds the profile repository.
func ProfileRepositoryFactory(b hotswap.Backend) (domain.ProfileRepository, error) {
	return repository(b, ProfileMapping())
}

// AccountRepositoryFactory builds the employer account repository.
func AccountRepositoryFactory(b hotswap.Backend) (domain.EmployerAccountRepository, error) {
	return repository(b, AccountMapping())
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

// HRStaff adapts the HR Staff contract to the Payroll Employments port (ACL).
type HRStaff struct{ Staff hr.Staff }

var _ papp.Employments = HRStaff{}

// EmploymentOn implements papp.Employments.
func (h HRStaff) EmploymentOn(ctx context.Context, person domain.PersonID, employer domain.OrganizationID, on vocab.Date) (papp.EmploymentInfo, bool, error) {
	m, err := h.Staff.EmploymentsOn(ctx, []string{person.String()}, on.String())
	if err != nil {
		return papp.EmploymentInfo{}, false, err
	}
	for _, e := range m[person.String()] {
		if e.Employer != employer.String() {
			continue
		}
		id, err := fw.ParseUUID(e.ID)
		if err != nil {
			return papp.EmploymentInfo{}, false, err
		}
		info := papp.EmploymentInfo{ID: domain.EmploymentID{UUID: id}, Number: e.Number}
		if c := e.Contract; c != nil {
			if u, err := fw.ParseUUID(c.Agreement); err == nil {
				info.Agreement = domain.AgreementID{UUID: u}
			}
			if u, err := fw.ParseUUID(c.WorkCenter); err == nil {
				info.WorkCenter = domain.WorkCenterID{UUID: u}
			}
		}
		return info, true, nil
	}
	return papp.EmploymentInfo{}, false, nil
}

// Package infrastructure stores the Fiscal context: SQL mappings, versioned schema of the five
// engines, hot-swap factories and the Parties adapter. Nothing is seeded: the C# seeded no tax
// rates, and legal data is not invented here.
package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/fiscal/domain"
	parties "github.com/jhermoso/karpo-fw-go/contexts/parties/contracts"
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
const Context = "fiscal"

// Technical tables of the context.
const (
	TableOutbox            = "fiscal_outbox"
	TableIntegrationOutbox = "fiscal_integration_outbox"
	TableAuditLog          = "fiscal_audit_log"
	TableInbox             = "fiscal_inbox"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE fis_tax_rates (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, tax_type {int} NOT NULL, territory {int} NOT NULL,
	code {str:5} NOT NULL, description {str:100} NOT NULL, rate {str:20} NOT NULL, surcharge {str:20}, valid_from {date} NOT NULL, valid_to {date}, ` + audit + `)`,
	`CREATE INDEX ix_fis_tax_rates_code ON fis_tax_rates (tax_type, territory, code)`,
	`CREATE TABLE fis_treatments (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, territory {int} NOT NULL, code {str:5} NOT NULL,
	description {str:100} NOT NULL, kind {int} NOT NULL, active {bool} NOT NULL, ` + audit + `)`,
	`CREATE TABLE fis_taxpayers (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, organization {uuid} NOT NULL, territory {int} NOT NULL,
	fy_start_month {int} NOT NULL, general_prorata {str:20}, differentiated_sectors {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_fis_taxpayers_org ON fis_taxpayers (organization)`,
	`CREATE TABLE fis_activities (id {uuid} NOT NULL PRIMARY KEY, taxpayer_id {uuid} NOT NULL, code {str:20} NOT NULL, iae {str:10},
	category {int} NOT NULL, description {str:200} NOT NULL, valid_from {date} NOT NULL, valid_to {date}, is_primary {bool} NOT NULL,
	vat_deductible {bool} NOT NULL, estimation_regime {str:30}, FOREIGN KEY (taxpayer_id) REFERENCES fis_taxpayers (id))`,
	`CREATE TABLE fis_obligations (id {uuid} NOT NULL PRIMARY KEY, taxpayer_id {uuid} NOT NULL, form_code {str:3} NOT NULL,
	periodicity {int} NOT NULL, from_year {int} NOT NULL, until_year {int}, FOREIGN KEY (taxpayer_id) REFERENCES fis_taxpayers (id))`,
	`CREATE TABLE fis_filings (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, declarant {uuid} NOT NULL, form_code {str:3} NOT NULL,
	fiscal_year {int} NOT NULL, filing_period {str:2} NOT NULL, status {int} NOT NULL, declarant_nif {str:20}, declarant_name {str:200},
	filing_number {bigint}, reference {str:64}, submitted_at {ts}, revert_reason {str:200}, ` + audit + `)`,
	`CREATE INDEX ix_fis_filings_slot ON fis_filings (declarant, form_code, fiscal_year, filing_period)`,
	`CREATE TABLE fis_filing_lines (filing_id {uuid} NOT NULL, line_no {int} NOT NULL, party {uuid} NOT NULL, nif {str:20},
	recipient_name {str:200}, province {str:2}, perception_key {str:1} NOT NULL, payments {int} NOT NULL, perceptions {str:30} NOT NULL,
	withheld {str:30} NOT NULL, PRIMARY KEY (filing_id, line_no), FOREIGN KEY (filing_id) REFERENCES fis_filings (id))`,
	`CREATE TABLE fis_counters (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, declarant {uuid} NOT NULL, form_code {str:3} NOT NULL,
	fiscal_year {int} NOT NULL, last_number {bigint} NOT NULL)`,
	`CREATE UNIQUE INDEX ux_fis_counters_key ON fis_counters (declarant, form_code, fiscal_year)`,
	`CREATE TABLE fis_withholdings (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, payer {uuid} NOT NULL, recipient {uuid} NOT NULL,
	payment_date {date} NOT NULL, perception_key {str:1} NOT NULL, perceptions {str:30} NOT NULL, withheld {str:30} NOT NULL, cancelled {bool} NOT NULL)`,
	`CREATE INDEX ix_fis_withholdings_payer ON fis_withholdings (payer, payment_date)`,
}

func technicalDDL(d string) []string {
	switch d {
	case "sqlite":
		return slices.Concat(sqlite.OutboxDDL(TableOutbox), sqlite.OutboxDDL(TableIntegrationOutbox), sqlite.AuditDDL(TableAuditLog), sqlite.InboxDDL(TableInbox))
	case "postgres":
		return slices.Concat(postgres.OutboxDDL(TableOutbox), postgres.OutboxDDL(TableIntegrationOutbox), postgres.AuditDDL(TableAuditLog), postgres.InboxDDL(TableInbox))
	case "sqlserver":
		return slices.Concat(sqlserver.OutboxDDL(TableOutbox), sqlserver.OutboxDDL(TableIntegrationOutbox), sqlserver.AuditDDL(TableAuditLog), sqlserver.InboxDDL(TableInbox))
	case "oracle":
		return slices.Concat(oracle.OutboxDDL(TableOutbox), oracle.OutboxDDL(TableIntegrationOutbox), oracle.AuditDDL(TableAuditLog), oracle.InboxDDL(TableInbox))
	case "mysql":
		return slices.Concat(mysql.OutboxDDL(TableOutbox), mysql.OutboxDDL(TableIntegrationOutbox), mysql.AuditDDL(TableAuditLog), mysql.InboxDDL(TableInbox))
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
		{Version: 1, Name: "tax catalogs, taxpayers, filings and withholdings", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes, audit log and inbox", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"fis_filing_lines", "fis_filings", "fis_counters", "fis_withholdings", "fis_activities", "fis_obligations", "fis_taxpayers",
	"fis_treatments", "fis_tax_rates", TableOutbox, TableIntegrationOutbox, TableAuditLog, TableInbox}

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

func optDecimal(d vocab.Decimal) any {
	if d.IsZero() {
		return nil
	}
	return d.String()
}

func optInt(n int) any {
	if n == 0 {
		return nil
	}
	return int64(n)
}

// TaxRateMapping maps TaxRate to fis_tax_rates.
func TaxRateMapping() sqlrepo.Mapping[domain.TaxRateID, *domain.TaxRate] {
	return sqlrepo.Mapping[domain.TaxRateID, *domain.TaxRate]{
		Table:   "fis_tax_rates",
		Columns: sqlrepo.WithAuditColumns("tax_type", "territory", "code", "description", "rate", "surcharge", "valid_from", "valid_to"),
		Dehydrate: func(r *domain.TaxRate) (sqlrepo.Values, error) {
			s := r.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"tax_type": int64(s.Type), "territory": int64(s.Territory), "code": s.Code,
				"description": s.Description, "rate": s.Rate.String(), "surcharge": optDecimal(s.Surcharge), "valid_from": s.From,
				"valid_to": optDate(s.Until)}, r.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.TaxRate, error) {
			s := domain.TaxRateState{Type: domain.TaxType(r.Int64("tax_type")), Territory: domain.Territory(r.Int64("territory")), Code: r.String("code"),
				Description: r.String("description"), Rate: r.Decimal("rate"), Surcharge: r.Decimal("surcharge"), From: r.Date("valid_from"),
				Until: r.Date("valid_to"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteTaxRate(domain.TaxRateID{UUID: r.UUID("id")}, s)
		},
	}
}

// TreatmentMapping maps Treatment to fis_treatments.
func TreatmentMapping() sqlrepo.Mapping[domain.TreatmentID, *domain.Treatment] {
	return sqlrepo.Mapping[domain.TreatmentID, *domain.Treatment]{
		Table:   "fis_treatments",
		Columns: sqlrepo.WithAuditColumns("territory", "code", "description", "kind", "active"),
		Dehydrate: func(t *domain.Treatment) (sqlrepo.Values, error) {
			s := t.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"territory": int64(s.Territory), "code": s.Code, "description": s.Description,
				"kind": int64(s.Kind), "active": s.Active}, t.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Treatment, error) {
			s := domain.TreatmentState{Territory: domain.Territory(r.Int64("territory")), Code: r.String("code"), Description: r.String("description"),
				Kind: domain.TreatmentKind(r.Int64("kind")), Active: r.Bool("active"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteTreatment(domain.TreatmentID{UUID: r.UUID("id")}, s)
		},
	}
}

// TaxpayerMapping maps Taxpayer to fis_taxpayers, its activities and obligations.
func TaxpayerMapping() sqlrepo.Mapping[domain.TaxpayerID, *domain.Taxpayer] {
	return sqlrepo.Mapping[domain.TaxpayerID, *domain.Taxpayer]{
		Table:   "fis_taxpayers",
		Columns: sqlrepo.WithAuditColumns("organization", "territory", "fy_start_month", "general_prorata", "differentiated_sectors"),
		Dehydrate: func(t *domain.Taxpayer) (sqlrepo.Values, error) {
			tm := t.Terms()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"organization": t.Organization(), "territory": int64(tm.Territory),
				"fy_start_month": int64(tm.FiscalYearStartMonth), "general_prorata": optDecimal(tm.GeneralProrata),
				"differentiated_sectors": tm.DifferentiatedSectors}, t.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Taxpayer, error) {
			s := domain.TaxpayerState{Organization: domain.OrganizationID{UUID: r.UUID("organization")}, Audit: r.AuditStamp(),
				Terms: domain.TaxpayerTerms{Territory: domain.Territory(r.Int64("territory")), FiscalYearStartMonth: int(r.Int64("fy_start_month")),
					GeneralProrata: r.Decimal("general_prorata"), DifferentiatedSectors: r.Bool("differentiated_sectors")}}
			for _, c := range children.Of("activities") {
				s.Activities = append(s.Activities, domain.Activity{ID: domain.ActivityID{UUID: c.UUID("id")}, Code: c.String("code"), IAE: c.String("iae"),
					Category: domain.ActivityCategory(c.Int64("category")), Description: c.String("description"), From: c.Date("valid_from"),
					Until: c.Date("valid_to"), Primary: c.Bool("is_primary"), VATDeductible: c.Bool("vat_deductible"),
					EstimationRegime: c.String("estimation_regime")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			for _, c := range children.Of("obligations") {
				s.Obligations = append(s.Obligations, domain.Obligation{ID: domain.ObligationID{UUID: c.UUID("id")}, Form: domain.Form(c.String("form_code")),
					Periodicity: domain.Periodicity(c.Int64("periodicity")), FromYear: int(c.Int64("from_year")), UntilYear: int(c.Int64("until_year"))})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			// UUID v7: byte order is creation order on every engine.
			slices.SortFunc(s.Activities, func(a, b domain.Activity) int { return bytes.Compare(a.ID.Bytes(), b.ID.Bytes()) })
			slices.SortFunc(s.Obligations, func(a, b domain.Obligation) int { return bytes.Compare(a.ID.Bytes(), b.ID.Bytes()) })
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteTaxpayer(domain.TaxpayerID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Taxpayer]{{
			Name: "activities", Table: "fis_activities", ForeignKey: "taxpayer_id", OrderBy: []string{"id"},
			Columns: []string{"id", "code", "iae", "category", "description", "valid_from", "valid_to", "is_primary", "vat_deductible", "estimation_regime"},
			Dehydrate: func(t *domain.Taxpayer) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, a := range t.Activities() {
					out = append(out, sqlrepo.Values{"id": a.ID, "code": a.Code, "iae": opt(a.IAE), "category": int64(a.Category), "description": a.Description,
						"valid_from": a.From, "valid_to": optDate(a.Until), "is_primary": a.Primary, "vat_deductible": a.VATDeductible,
						"estimation_regime": opt(a.EstimationRegime)})
				}
				return out, nil
			},
		}, {
			Name: "obligations", Table: "fis_obligations", ForeignKey: "taxpayer_id", OrderBy: []string{"id"},
			Columns: []string{"id", "form_code", "periodicity", "from_year", "until_year"},
			Dehydrate: func(t *domain.Taxpayer) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for _, o := range t.Obligations() {
					out = append(out, sqlrepo.Values{"id": o.ID, "form_code": string(o.Form), "periodicity": int64(o.Periodicity),
						"from_year": int64(o.FromYear), "until_year": optInt(o.UntilYear)})
				}
				return out, nil
			},
		}},
	}
}

// FilingMapping maps Filing to fis_filings and its lines.
func FilingMapping() sqlrepo.Mapping[domain.FilingID, *domain.Filing] {
	return sqlrepo.Mapping[domain.FilingID, *domain.Filing]{
		Table: "fis_filings",
		Columns: sqlrepo.WithAuditColumns("declarant", "form_code", "fiscal_year", "filing_period", "status", "declarant_nif", "declarant_name",
			"filing_number", "reference", "submitted_at", "revert_reason"),
		Fields: map[string]string{"form": "form_code", "period": "filing_period"},
		Dehydrate: func(f *domain.Filing) (sqlrepo.Values, error) {
			s := f.State()
			v := sqlrepo.Values{"declarant": s.Declarant, "form_code": string(s.Form), "fiscal_year": int64(s.Year), "filing_period": s.Period.String(),
				"status": int64(s.Status), "declarant_nif": opt(s.Identity.NIF), "declarant_name": opt(s.Identity.Name), "filing_number": nil,
				"reference": opt(s.Reference), "submitted_at": nil, "revert_reason": opt(s.RevertReason)}
			if s.Number > 0 {
				v["filing_number"], v["submitted_at"] = s.Number, s.SubmittedAt
			}
			return sqlrepo.AuditStampValues(v, f.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Filing, error) {
			period, ok := domain.ParsePeriod(r.String("filing_period"))
			if !ok {
				return nil, fmt.Errorf("fiscal: invalid period %q", r.String("filing_period"))
			}
			s := domain.FilingState{Declarant: domain.OrganizationID{UUID: r.UUID("declarant")}, Form: domain.Form(r.String("form_code")),
				Year: int(r.Int64("fiscal_year")), Period: period, Status: domain.FilingStatus(r.Int64("status")),
				Identity: domain.Identity{NIF: r.String("declarant_nif"), Name: r.String("declarant_name")}, Number: r.Int64("filing_number"),
				Reference: r.String("reference"), RevertReason: r.String("revert_reason"), Audit: r.AuditStamp()}
			if t := r.NullTime("submitted_at"); t != nil {
				s.SubmittedAt = *t
			}
			type line struct {
				no int64
				r  domain.Recipient
			}
			var lines []line
			for _, c := range children.Of("lines") {
				lines = append(lines, line{c.Int64("line_no"), domain.Recipient{Party: domain.PartyID{UUID: c.UUID("party")},
					Identity: domain.Identity{NIF: c.String("nif"), Name: c.String("recipient_name"), Province: c.String("province")},
					Key:      c.String("perception_key"), Payments: int(c.Int64("payments")), Perceptions: c.Decimal("perceptions"), Withheld: c.Decimal("withheld")}})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			slices.SortFunc(lines, func(a, b line) int { return int(a.no - b.no) })
			for _, l := range lines {
				s.Recipients = append(s.Recipients, l.r)
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteFiling(domain.FilingID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Filing]{{
			Name: "lines", Table: "fis_filing_lines", ForeignKey: "filing_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "party", "nif", "recipient_name", "province", "perception_key", "payments", "perceptions", "withheld"},
			Dehydrate: func(f *domain.Filing) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for i, r := range f.State().Recipients {
					out = append(out, sqlrepo.Values{"line_no": int64(i + 1), "party": r.Party, "nif": opt(r.Identity.NIF), "recipient_name": opt(r.Identity.Name),
						"province": opt(r.Identity.Province), "perception_key": r.Key, "payments": int64(r.Payments),
						"perceptions": r.Perceptions.StringFixed(2), "withheld": r.Withheld.StringFixed(2)})
				}
				return out, nil
			},
		}},
	}
}

// CounterMapping maps Counter to fis_counters.
func CounterMapping() sqlrepo.Mapping[domain.CounterID, *domain.Counter] {
	return sqlrepo.Mapping[domain.CounterID, *domain.Counter]{
		Table:   "fis_counters",
		Columns: []string{"declarant", "form_code", "fiscal_year", "last_number"},
		Fields:  map[string]string{"form": "form_code"},
		Dehydrate: func(c *domain.Counter) (sqlrepo.Values, error) {
			d, form, year, last := c.Values()
			return sqlrepo.Values{"declarant": d, "form_code": string(form), "fiscal_year": int64(year), "last_number": last}, nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Counter, error) {
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteCounter(domain.CounterID{UUID: r.UUID("id")}, domain.OrganizationID{UUID: r.UUID("declarant")},
				domain.Form(r.String("form_code")), int(r.Int64("fiscal_year")), r.Int64("last_number"))
		},
	}
}

// WithholdingMapping maps Withholding to fis_withholdings.
func WithholdingMapping() sqlrepo.Mapping[domain.WithholdingID, *domain.Withholding] {
	return sqlrepo.Mapping[domain.WithholdingID, *domain.Withholding]{
		Table:   "fis_withholdings",
		Columns: []string{"payer", "recipient", "payment_date", "perception_key", "perceptions", "withheld", "cancelled"},
		Dehydrate: func(w *domain.Withholding) (sqlrepo.Values, error) {
			s := w.State()
			return sqlrepo.Values{"payer": s.Payer, "recipient": s.Recipient, "payment_date": s.PaymentDate, "perception_key": s.Key,
				"perceptions": s.Perceptions.StringFixed(2), "withheld": s.Withheld.StringFixed(2), "cancelled": s.Cancelled}, nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Withholding, error) {
			s := domain.WithholdingState{Payer: domain.OrganizationID{UUID: r.UUID("payer")}, Recipient: domain.PartyID{UUID: r.UUID("recipient")},
				PaymentDate: r.Date("payment_date"), Key: r.String("perception_key"), Perceptions: r.Decimal("perceptions"),
				Withheld: r.Decimal("withheld"), Cancelled: r.Bool("cancelled")}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteWithholding(domain.WithholdingID{UUID: r.UUID("id")}, s)
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("fiscal: unsupported backend %T", b) }

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
func TaxRateRepositoryFactory(b hotswap.Backend) (domain.TaxRateRepository, error) {
	return repository(b, TaxRateMapping())
}

func TreatmentRepositoryFactory(b hotswap.Backend) (domain.TreatmentRepository, error) {
	return repository(b, TreatmentMapping())
}

func TaxpayerRepositoryFactory(b hotswap.Backend) (domain.TaxpayerRepository, error) {
	return repository(b, TaxpayerMapping())
}

func FilingRepositoryFactory(b hotswap.Backend) (domain.FilingRepository, error) {
	return repository(b, FilingMapping())
}

func CounterRepositoryFactory(b hotswap.Backend) (domain.CounterRepository, error) {
	return repository(b, CounterMapping())
}

func WithholdingRepositoryFactory(b hotswap.Backend) (domain.WithholdingRepository, error) {
	return repository(b, WithholdingMapping())
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

// InboxFactory builds the inbox of the integration events Fiscal consumes.
func InboxFactory(b hotswap.Backend) (application.InboxStore, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewInbox(db, TableInbox)
	case *memory.Store:
		return memory.NewInbox(db), nil
	}
	return nil, unsupported(b)
}

// PartiesIdentities adapts the Parties TaxIdentities contract to the Fiscal Identities port (ACL).
type PartiesIdentities struct{ TaxIdentities parties.TaxIdentities }

var _ domain.Identities = PartiesIdentities{}

// Identities implements domain.Identities (in batches of the Parties maximum).
func (p PartiesIdentities) Identities(ctx context.Context, ids []domain.PartyID) (map[domain.PartyID]domain.Identity, error) {
	out := map[domain.PartyID]domain.Identity{}
	var batch []string
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		m, err := p.TaxIdentities.TaxIdentities(ctx, batch)
		if err != nil {
			return err
		}
		for id, ti := range m {
			u, err := fw.ParseUUID(id)
			if err != nil {
				continue
			}
			out[domain.PartyID{UUID: u}] = domain.Identity{NIF: vocab.NormalizeDocumentNumber(ti.Number), Name: ti.Name, Province: ti.Province}
		}
		batch = batch[:0]
		return nil
	}
	seen := map[domain.PartyID]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		batch = append(batch, id.String())
		if len(batch) == parties.MaxDirectoryBatch {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	return out, flush()
}

// Package infrastructure stores the Assets context: SQL mapping, versioned schema of the five
// engines and hot-swap factories.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/assets/domain"
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
const Context = "assets"

// Technical tables of the context.
const (
	TableOutbox            = "assets_outbox"
	TableIntegrationOutbox = "assets_integration_outbox"
	TableAuditLog          = "assets_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var schemaDDL = []string{
	`CREATE TABLE ast_assets (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, company {uuid} NOT NULL, code {str:20} NOT NULL,
	asset_class {str:20} NOT NULL, name {str:120} NOT NULL, serial {str:60}, location {uuid}, supplier {uuid}, document {str:60},
	acquired {date} NOT NULL, in_service {date} NOT NULL, cost {str:30} NOT NULL, residual {str:30} NOT NULL, life_months {int} NOT NULL,
	disposed {bool} NOT NULL, disposed_on {date}, disposal {str:10}, proceeds {str:30}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_ast_assets_code ON ast_assets (company, code)`,
	`CREATE TABLE ast_asset_charges (asset_id {uuid} NOT NULL, line_no {int} NOT NULL, period_year {int} NOT NULL, period_month {int} NOT NULL,
	amount {str:30} NOT NULL, PRIMARY KEY (asset_id, line_no), FOREIGN KEY (asset_id) REFERENCES ast_assets (id))`,
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
		{Version: 1, Name: "asset register and depreciation charges", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
	}}
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lists the tables of the context, children first (drop order).
var Tables = []string{"ast_asset_charges", "ast_assets", TableOutbox, TableIntegrationOutbox, TableAuditLog}

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

// AssetMapping maps Asset to ast_assets and its charges.
func AssetMapping() sqlrepo.Mapping[domain.AssetID, *domain.Asset] {
	return sqlrepo.Mapping[domain.AssetID, *domain.Asset]{
		Table: "ast_assets",
		Columns: sqlrepo.WithAuditColumns("company", "code", "asset_class", "name", "serial", "location", "supplier", "document", "acquired",
			"in_service", "cost", "residual", "life_months", "disposed", "disposed_on", "disposal", "proceeds"),
		Dehydrate: func(a *domain.Asset) (sqlrepo.Values, error) {
			s := a.State()
			v := sqlrepo.Values{"company": s.Company, "code": s.Code, "asset_class": string(s.Class), "name": s.Name, "serial": opt(s.Serial),
				"location": optUUID(s.Location.UUID), "supplier": optUUID(s.Supplier.UUID), "document": opt(s.Document), "acquired": s.Acquired,
				"in_service": s.InService, "cost": s.Cost.StringFixed(2), "residual": s.Residual.StringFixed(2), "life_months": int64(s.LifeMonths),
				"disposed": !s.Disposed.IsZero(), "disposed_on": nil, "disposal": nil, "proceeds": nil}
			if !s.Disposed.IsZero() {
				v["disposed_on"], v["disposal"], v["proceeds"] = s.Disposed, s.Disposal, s.Proceeds.StringFixed(2)
			}
			return sqlrepo.AuditStampValues(v, a.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, children sqlrepo.ChildRows) (*domain.Asset, error) {
			s := domain.AssetState{Company: domain.OrganizationID{UUID: r.UUID("company")}, Code: r.String("code"), Class: domain.Class(r.String("asset_class")),
				Details:  domain.Details{Name: r.String("name"), Serial: r.String("serial"), Location: domain.FacilityID{UUID: r.UUID("location")}},
				Supplier: domain.PartyID{UUID: r.UUID("supplier")}, Document: r.String("document"), Acquired: r.Date("acquired"), InService: r.Date("in_service"),
				Cost: r.Decimal("cost"), Residual: r.Decimal("residual"), LifeMonths: int(r.Int64("life_months")), Proceeds: vocab.DecimalFromInt(0),
				Audit: r.AuditStamp()}
			if r.Bool("disposed") {
				s.Disposed, s.Disposal, s.Proceeds = r.Date("disposed_on"), r.String("disposal"), r.Decimal("proceeds")
			}
			// Engines do not agree on the order of the children: months are sorted here.
			rows := slices.Clone(children.Of("charges"))
			slices.SortFunc(rows, func(a, b *sqlrepo.Row) int { return int(a.Int64("line_no") - b.Int64("line_no")) })
			for _, c := range rows {
				s.Charges = append(s.Charges, domain.Charge{Period: domain.Period{Year: int(c.Int64("period_year")), Month: int(c.Int64("period_month"))},
					Amount: c.Decimal("amount")})
				if err := c.Err(); err != nil {
					return nil, err
				}
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteAsset(domain.AssetID{UUID: r.UUID("id")}, s)
		},
		Children: []sqlrepo.Child[*domain.Asset]{{
			Name: "charges", Table: "ast_asset_charges", ForeignKey: "asset_id", OrderBy: []string{"line_no"},
			Columns: []string{"line_no", "period_year", "period_month", "amount"},
			Dehydrate: func(a *domain.Asset) ([]sqlrepo.Values, error) {
				out := []sqlrepo.Values{}
				for k, c := range a.State().Charges {
					out = append(out, sqlrepo.Values{"line_no": int64(k + 1), "period_year": int64(c.Period.Year), "period_month": int64(c.Period.Month),
						"amount": c.Amount.StringFixed(2)})
				}
				return out, nil
			},
		}},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("assets: unsupported backend %T", b) }

// AssetRepositoryFactory builds the asset repository.
func AssetRepositoryFactory(b hotswap.Backend) (domain.AssetRepository, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, AssetMapping())
	case *memory.Store:
		return memory.NewRepository[domain.AssetID, *domain.Asset](db), nil
	}
	return nil, unsupported(b)
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

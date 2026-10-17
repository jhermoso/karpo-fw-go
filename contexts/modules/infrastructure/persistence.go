// Package infrastructure guarda el contexto de Modules: los mapeos SQL, el esquema versionado de
// los cinco motores y las factorías para el intercambio en caliente.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

	"github.com/jhermoso/karpo-fw-go/contexts/modules/domain"
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

// Context es el nombre del contexto delimitado.
const Context = "modules"

// Tablas técnicas del contexto.
const (
	TableOutbox            = "modules_outbox"
	TableIntegrationOutbox = "modules_integration_outbox"
	TableAuditLog          = "modules_audit_log"
)

const audit = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200}, modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

// Una fila por funcionalidad del catálogo, y una por empresa y funcionalidad: las dos únicas en
// todos los motores (la regla del C# era un índice filtrado que sólo algunos tenían).
var schemaDDL = []string{
	`CREATE TABLE mod_features (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, kind {str:20} NOT NULL, code {str:30} NOT NULL,
	name {str:100} NOT NULL, description {str:500}, retired {bool} NOT NULL, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_mod_features_code ON mod_features (kind, code)`,
	`CREATE TABLE mod_activations (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, organization {uuid} NOT NULL, kind {str:20} NOT NULL,
	code {str:30} NOT NULL, active {bool} NOT NULL, activated_at {ts}, activated_by {str:200}, deactivated_at {ts}, deactivated_by {str:200},
	notes {str:500}, ` + audit + `)`,
	`CREATE UNIQUE INDEX ux_mod_activations_feature ON mod_activations (organization, kind, code)`,
	`CREATE INDEX ix_mod_activations_code ON mod_activations (kind, code)`,
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

// Migrations es el esquema versionado del contexto.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "catalog of features and activations", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
	}}
}

// Migrator devuelve el migrador del esquema del contexto.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// Tables lista las tablas del contexto (orden de borrado).
var Tables = []string{"mod_activations", "mod_features", TableOutbox, TableIntegrationOutbox, TableAuditLog}

// DropAll borra las tablas del contexto y su historial de migraciones (sólo pruebas).
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

// FeatureMapping mapea Feature a mod_features.
func FeatureMapping() sqlrepo.Mapping[domain.FeatureID, *domain.Feature] {
	return sqlrepo.Mapping[domain.FeatureID, *domain.Feature]{
		Table:   "mod_features",
		Columns: sqlrepo.WithAuditColumns("kind", "code", "name", "description", "retired"),
		Dehydrate: func(f *domain.Feature) (sqlrepo.Values, error) {
			s := f.State()
			return sqlrepo.AuditStampValues(sqlrepo.Values{"kind": string(s.Kind), "code": s.Code, "name": s.Name, "description": opt(s.Description),
				"retired": s.Retired}, f.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Feature, error) {
			s := domain.FeatureState{Kind: domain.Kind(r.String("kind")), Code: r.String("code"), Name: r.String("name"),
				Description: r.String("description"), Retired: r.Bool("retired"), Audit: r.AuditStamp()}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteFeature(domain.FeatureID{UUID: r.UUID("id")}, s)
		},
	}
}

// ActivationMapping mapea Activation a mod_activations.
func ActivationMapping() sqlrepo.Mapping[domain.ActivationID, *domain.Activation] {
	return sqlrepo.Mapping[domain.ActivationID, *domain.Activation]{
		Table: "mod_activations",
		Columns: sqlrepo.WithAuditColumns("organization", "kind", "code", "active", "activated_at", "activated_by", "deactivated_at", "deactivated_by",
			"notes"),
		Dehydrate: func(a *domain.Activation) (sqlrepo.Values, error) {
			s := a.State()
			v := sqlrepo.Values{"organization": s.Organization, "kind": string(s.Kind), "code": s.Code, "active": s.Active, "activated_at": nil,
				"activated_by": opt(s.ActivatedBy), "deactivated_at": nil, "deactivated_by": opt(s.DeactivatedBy), "notes": opt(s.Notes)}
			if !s.ActivatedAt.IsZero() {
				v["activated_at"] = s.ActivatedAt
			}
			if !s.DeactivatedAt.IsZero() {
				v["deactivated_at"] = s.DeactivatedAt
			}
			return sqlrepo.AuditStampValues(v, a.AuditStamp()), nil
		},
		Hydrate: func(r *sqlrepo.Row, _ sqlrepo.ChildRows) (*domain.Activation, error) {
			s := domain.ActivationState{Organization: domain.OrganizationID{UUID: r.UUID("organization")}, Kind: domain.Kind(r.String("kind")),
				Code: r.String("code"), Active: r.Bool("active"), ActivatedBy: r.String("activated_by"), DeactivatedBy: r.String("deactivated_by"),
				Notes: r.String("notes"), Audit: r.AuditStamp()}
			if t := r.NullTime("activated_at"); t != nil {
				s.ActivatedAt = *t
			}
			if t := r.NullTime("deactivated_at"); t != nil {
				s.DeactivatedAt = *t
			}
			if err := r.Err(); err != nil {
				return nil, err
			}
			return domain.ReconstituteActivation(domain.ActivationID{UUID: r.UUID("id")}, s)
		},
	}
}

func unsupported(b hotswap.Backend) error { return fmt.Errorf("modules: unsupported backend %T", b) }

func repository[ID fw.Identifier, T fw.AggregateRoot[ID]](b hotswap.Backend, m sqlrepo.Mapping[ID, T]) (fw.Repository[ID, T], error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewRepository(db, m)
	case *memory.Store:
		return memory.NewRepository[ID, T](db), nil
	}
	return nil, unsupported(b)
}

// FeatureRepositoryFactory construye el repositorio del catálogo.
func FeatureRepositoryFactory(b hotswap.Backend) (domain.FeatureRepository, error) {
	return repository(b, FeatureMapping())
}

// ActivationRepositoryFactory construye el repositorio de activaciones.
func ActivationRepositoryFactory(b hotswap.Backend) (domain.ActivationRepository, error) {
	return repository(b, ActivationMapping())
}

// OutboxFactory construye el outbox de eventos de dominio.
func OutboxFactory(b hotswap.Backend) (application.OutboxStore, error) { return outbox(b, TableOutbox) }

// IntegrationOutboxFactory construye el outbox de integración.
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

// AuditLogFactory construye el registro de auditoría.
func AuditLogFactory(b hotswap.Backend) (application.AuditLog, error) {
	switch db := b.(type) {
	case *sqlrepo.DB:
		return sqlrepo.NewAuditLog(db, TableAuditLog)
	case *memory.Store:
		return memory.NewAuditLog(db), nil
	}
	return nil, unsupported(b)
}

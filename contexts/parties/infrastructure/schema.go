// Package infrastructure adapts the Parties context to storage: SQL mappings of its aggregates,
// the versioned schema (migrations for the five engines, with the well-known catalogs as seed),
// the catalog reader and the hot-swap factories.
package infrastructure

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// Context is the name of the bounded context (migration history, integration source).
const Context = "parties"

// Tables owned by the context. The technical tables carry the context prefix so several
// contexts can share a database.
const (
	TablePartiesOutbox     = "parties_outbox"
	TableIntegrationOutbox = "parties_integration_outbox"
	TableAuditLog          = "parties_audit_log"
)

// Dialects the context supports.
var Dialects = []string{"sqlite", "postgres", "sqlserver", "oracle", "mysql"}

var logical = map[string]map[string]string{
	"sqlite":    {"uuid": "TEXT", "str": "TEXT", "bool": "INTEGER", "ts": "TEXT", "date": "TEXT", "bigint": "INTEGER"},
	"postgres":  {"uuid": "UUID", "str": "VARCHAR(%s)", "bool": "BOOLEAN", "ts": "TIMESTAMPTZ", "date": "DATE", "bigint": "BIGINT"},
	"sqlserver": {"uuid": "UNIQUEIDENTIFIER", "str": "NVARCHAR(%s)", "bool": "BIT", "ts": "DATETIME2(7)", "date": "DATE", "bigint": "BIGINT"},
	"oracle":    {"uuid": "RAW(16)", "str": "VARCHAR2(%s)", "bool": "NUMBER(1)", "ts": "TIMESTAMP(6) WITH TIME ZONE", "date": "DATE", "bigint": "NUMBER(19)"},
	"mysql":     {"uuid": "CHAR(36)", "str": "VARCHAR(%s)", "bool": "BOOLEAN", "ts": "DATETIME(6)", "date": "DATE", "bigint": "BIGINT"},
}

var placeholder = regexp.MustCompile(`\{(\w+)(?::(\d+))?\}`)

// render replaces {uuid}, {str:N}, {bool}, {ts}, {date} and {bigint} with the dialect's types.
// Optional text columns are nullable everywhere: Oracle stores "" as NULL.
func render(dialect, ddl string) string {
	types := logical[dialect]
	return placeholder.ReplaceAllStringFunc(ddl, func(m string) string {
		p := placeholder.FindStringSubmatch(m)
		t := types[p[1]]
		if strings.Contains(t, "%s") {
			t = fmt.Sprintf(t, p[2])
		}
		return t
	})
}

const auditCols = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200},
	modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

var initialDDL = []string{
	`CREATE TABLE role_types (id {uuid} NOT NULL PRIMARY KEY, name {str:200} NOT NULL, description {str:500},
	parent_id {uuid}, category {bool} NOT NULL)`,
	`CREATE TABLE relationship_types (id {uuid} NOT NULL PRIMARY KEY, name {str:200} NOT NULL, description {str:500},
	from_role {uuid} NOT NULL, to_role {uuid} NOT NULL,
	FOREIGN KEY (from_role) REFERENCES role_types (id), FOREIGN KEY (to_role) REFERENCES role_types (id))`,
	`CREATE TABLE parties (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, kind {str:20} NOT NULL,
	name {str:300} NOT NULL, given_name {str:60}, first_surname {str:60}, second_surname {str:60},
	gender {str:20}, birth_date {date}, marital_status {str:30}, legal_name {str:200}, trade_name {str:120},
	active {bool} NOT NULL, test {bool} NOT NULL, ` + auditCols + `)`,
	`CREATE INDEX ix_parties_name ON parties (name)`,
	`CREATE TABLE party_roles (party_id {uuid} NOT NULL, id {uuid} NOT NULL, role_type {uuid} NOT NULL,
	valid_from {ts} NOT NULL, valid_to {ts}, PRIMARY KEY (party_id, id),
	FOREIGN KEY (party_id) REFERENCES parties (id), FOREIGN KEY (role_type) REFERENCES role_types (id))`,
	`CREATE INDEX ix_party_roles_type ON party_roles (role_type)`,
	`CREATE TABLE party_relationships (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL,
	rel_type {uuid} NOT NULL, from_party {uuid} NOT NULL, to_party {uuid} NOT NULL,
	from_role {uuid} NOT NULL, to_role {uuid} NOT NULL, valid_from {ts} NOT NULL, valid_to {ts}, remark {str:1000},
	` + auditCols + `,
	FOREIGN KEY (rel_type) REFERENCES relationship_types (id),
	FOREIGN KEY (from_party) REFERENCES parties (id), FOREIGN KEY (to_party) REFERENCES parties (id))`,
	`CREATE INDEX ix_party_relationships_from ON party_relationships (from_party)`,
	`CREATE INDEX ix_party_relationships_to ON party_relationships (to_party)`,
}

// technicalDDL returns the outboxes and audit log of the context for a dialect.
func technicalDDL(dialect string) []string {
	switch dialect {
	case "sqlite":
		return slices.Concat(sqlite.OutboxDDL(TablePartiesOutbox), sqlite.OutboxDDL(TableIntegrationOutbox), sqlite.AuditDDL(TableAuditLog))
	case "postgres":
		return slices.Concat(postgres.OutboxDDL(TablePartiesOutbox), postgres.OutboxDDL(TableIntegrationOutbox), postgres.AuditDDL(TableAuditLog))
	case "sqlserver":
		return slices.Concat(sqlserver.OutboxDDL(TablePartiesOutbox), sqlserver.OutboxDDL(TableIntegrationOutbox), sqlserver.AuditDDL(TableAuditLog))
	case "oracle":
		return slices.Concat(oracle.OutboxDDL(TablePartiesOutbox), oracle.OutboxDDL(TableIntegrationOutbox), oracle.AuditDDL(TableAuditLog))
	case "mysql":
		return slices.Concat(mysql.OutboxDDL(TablePartiesOutbox), mysql.OutboxDDL(TableIntegrationOutbox), mysql.AuditDDL(TableAuditLog))
	}
	return nil
}

// Migrations is the versioned schema of the Parties context.
func Migrations() sqlrepo.MigrationSet {
	initial := map[string][]string{}
	technical := map[string][]string{}
	for _, d := range Dialects {
		for _, s := range initialDDL {
			initial[d] = append(initial[d], render(d, s))
		}
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "parties, roles and relationships", Up: initial},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
		{Version: 3, Name: "well-known role and relationship types", Run: seedCatalogs},
	}}
}

// seedCatalogs inserts the well-known catalogs with the same GUIDs as the C# WellKnownCatalog.
// Parents go first: the order of WellKnownRoleTypes already respects the hierarchy.
func seedCatalogs(ctx context.Context, db *sqlrepo.DB) error {
	for _, t := range domain.WellKnownRoleTypes() {
		var parent any
		if t.Parent != nil {
			parent = *t.Parent
		}
		if err := db.Insert(ctx, "role_types", sqlrepo.Values{"id": t.ID, "name": t.Name.String(),
			"description": t.Description, "parent_id": parent, "category": t.Category}); err != nil {
			return fmt.Errorf("seeding role type %s: %w", t.Name, err)
		}
	}
	for _, t := range domain.WellKnownRelationshipTypes() {
		if err := db.Insert(ctx, "relationship_types", sqlrepo.Values{"id": t.ID, "name": t.Name.String(),
			"description": t.Description, "from_role": t.FromRole, "to_role": t.ToRole}); err != nil {
			return fmt.Errorf("seeding relationship type %s: %w", t.Name, err)
		}
	}
	return nil
}

// Migrator returns the schema migrator of Parties on db.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// DropAll removes every table of the context (tests only).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range []string{"party_relationships", "party_roles", "parties", "relationship_types", "role_types",
		TablePartiesOutbox, TableIntegrationOutbox, TableAuditLog, sqlrepo.DefaultMigrationsTable, sqlrepo.DefaultMigrationsLock} {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
}

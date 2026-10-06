// Package infrastructure stores the Security context: SQL mappings of its aggregates, the
// versioned schema of the five engines (with the system roles and the own permissions as seed),
// the password hasher, the hot-swap factories and the adapters over the Parties contracts.
package infrastructure

import (
	"context"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/security/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// Context is the name of the bounded context.
const Context = "security"

// Technical tables of the context.
const (
	TableOutbox            = "security_outbox"
	TableIntegrationOutbox = "security_integration_outbox"
	TableAuditLog          = "security_audit_log"
)

const auditCols = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200},
	modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

// The seven C# tables become eight: union tables lose their own id (they are children of their
// aggregate, unique by construction), the user gains its external identities, and the columns
// nobody read are gone (see docs/SEGURIDAD.md, section 4).
var schemaDDL = []string{
	`CREATE TABLE sec_permissions (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, code {str:200} NOT NULL,
	description {str:500}, active {bool} NOT NULL)`,
	`CREATE UNIQUE INDEX ux_sec_permissions_code ON sec_permissions (code)`,
	`CREATE TABLE sec_roles (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, name {str:100} NOT NULL,
	name_key {str:100} NOT NULL, description {str:500}, system_role {bool} NOT NULL, ` + auditCols + `)`,
	`CREATE UNIQUE INDEX ux_sec_roles_name ON sec_roles (name_key)`,
	`CREATE TABLE sec_role_permissions (role_id {uuid} NOT NULL, code {str:200} NOT NULL, PRIMARY KEY (role_id, code),
	FOREIGN KEY (role_id) REFERENCES sec_roles (id))`,
	`CREATE TABLE sec_users (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, party {uuid} NOT NULL,
	username {str:100} NOT NULL, username_key {str:100} NOT NULL, password_hash {str:500}, must_change_password {bool} NOT NULL,
	failed_attempts {bigint} NOT NULL, locked_until {ts}, last_login_at {ts}, active {bool} NOT NULL, ` + auditCols + `)`,
	`CREATE UNIQUE INDEX ux_sec_users_username ON sec_users (username_key)`,
	`CREATE INDEX ix_sec_users_party ON sec_users (party)`,
	`CREATE TABLE sec_user_roles (user_id {uuid} NOT NULL, role_id {uuid} NOT NULL, PRIMARY KEY (user_id, role_id),
	FOREIGN KEY (user_id) REFERENCES sec_users (id), FOREIGN KEY (role_id) REFERENCES sec_roles (id))`,
	`CREATE INDEX ix_sec_user_roles_role ON sec_user_roles (role_id)`,
	`CREATE TABLE sec_user_accesses (user_id {uuid} NOT NULL, organization {uuid} NOT NULL, access_level {str:20} NOT NULL,
	include_subsidiaries {bool} NOT NULL, PRIMARY KEY (user_id, organization), FOREIGN KEY (user_id) REFERENCES sec_users (id))`,
	`CREATE INDEX ix_sec_user_accesses_org ON sec_user_accesses (organization)`,
	`CREATE TABLE sec_user_identities (user_id {uuid} NOT NULL, issuer {str:200} NOT NULL, subject {str:200} NOT NULL,
	PRIMARY KEY (user_id, issuer, subject), FOREIGN KEY (user_id) REFERENCES sec_users (id))`,
	`CREATE UNIQUE INDEX ux_sec_user_identities ON sec_user_identities (issuer, subject)`,
	`CREATE TABLE sec_sessions (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, user_id {uuid} NOT NULL,
	family {uuid} NOT NULL, token_hash {str:64} NOT NULL, issued_at {ts} NOT NULL, expires_at {ts} NOT NULL, ended_at {ts},
	reason {str:30}, FOREIGN KEY (user_id) REFERENCES sec_users (id))`,
	`CREATE UNIQUE INDEX ux_sec_sessions_token ON sec_sessions (token_hash)`,
	`CREATE INDEX ix_sec_sessions_user ON sec_sessions (user_id)`,
	`CREATE INDEX ix_sec_sessions_family ON sec_sessions (family)`,
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

// Migrations is the versioned schema of the context. The users of each installation are business
// data; the seed is the catalog: the permissions Security declares and the five system roles
// (same GUIDs as the C# WellKnownSecurityCatalog) with the permissions of the standard rule. The
// permissions of the other contexts enter with Catalog.Sync when the host starts.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "users, roles, permission catalog and sessions", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
		{Version: 3, Name: "own permissions and system roles", Run: seedCatalog},
	}}
}

func seedCatalog(ctx context.Context, db *sqlrepo.DB) error {
	var perms [][]any
	var codes []domain.Permission
	for _, d := range domain.OwnPermissions() {
		perms = append(perms, []any{domain.PermissionIDOf(d.Code), int64(1), string(d.Code), d.Description, true})
		codes = append(codes, d.Code)
	}
	if err := db.InsertMany(ctx, "sec_permissions", []string{"id", "version", "code", "description", "active"}, perms); err != nil {
		return err
	}
	catalog := domain.NewCatalog(codes...)
	now := fw.Now()
	var roles, grants [][]any
	for _, r := range domain.SystemRoles() {
		roles = append(roles, []any{r.ID, int64(1), r.Name, strings.ToLower(r.Name), r.Description, true, now, vocab.SystemActor.Name})
		for _, p := range domain.StandardPermissions(r.ID, catalog) {
			grants = append(grants, []any{r.ID, string(p)})
		}
	}
	if err := db.InsertMany(ctx, "sec_roles", []string{"id", "version", "name", "name_key", "description", "system_role",
		"created_at", "created_by_name"}, roles); err != nil {
		return err
	}
	return db.InsertMany(ctx, "sec_role_permissions", []string{"role_id", "code"}, grants)
}

// Migrator returns the schema migrator of the context.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// DropAll removes the tables of the context and its migration history (tests only).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range []string{"sec_sessions", "sec_user_identities", "sec_user_accesses", "sec_user_roles", "sec_users",
		"sec_role_permissions", "sec_roles", "sec_permissions", TableOutbox, TableIntegrationOutbox, TableAuditLog} {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
	_, _ = db.ExecContext(ctx, "DELETE FROM "+sqlrepo.DefaultMigrationsTable+" WHERE context = '"+Context+"'")
}

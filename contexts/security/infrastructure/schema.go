// Package infrastructure guarda el contexto Security: los mapeos SQL de sus agregados, el esquema
// versionado de los cinco motores (con los roles del sistema y los permisos propios como semilla),
// el hasher de contraseñas, las factorías intercambiables en caliente y los adaptadores sobre los contratos de
// Parties.
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

// Context es el nombre del contexto delimitado.
const Context = "security"

// Tablas técnicas del contexto.
const (
	TableOutbox            = "security_outbox"
	TableIntegrationOutbox = "security_integration_outbox"
	TableAuditLog          = "security_audit_log"
)

const auditCols = `created_at {ts}, created_by_id {str:64}, created_by_name {str:200},
	modified_at {ts}, modified_by_id {str:64}, modified_by_name {str:200}`

// Las siete tablas de C# pasan a ser ocho: las tablas de unión pierden su id propio (son hijas de
// su agregado, únicas por construcción), el usuario gana sus identidades externas, y desaparecen
// las columnas que nadie leía (ver docs/SEGURIDAD.md, sección 4).
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

// Migrations es el esquema versionado del contexto. Los usuarios de cada instalación son datos de
// negocio; la semilla es el catálogo: los permisos que declara Security y los cinco roles del
// sistema (los mismos GUID que WellKnownSecurityCatalog de C#) con los permisos de la regla
// estándar. Los permisos de los otros contextos entran con Catalog.Sync al arrancar el anfitrión.
func Migrations() sqlrepo.MigrationSet {
	technical := map[string][]string{}
	for _, d := range sqlrepo.Dialects {
		technical[d] = technicalDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "users, roles, permission catalog and sessions", Up: sqlrepo.RenderDDLAll(schemaDDL...)},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
		{Version: 3, Name: "own permissions and system roles", Run: seedCatalog},
		{Version: 4, Name: "permissions the standard rule gave each system role", Up: sqlrepo.RenderDDLAll(grantedDDL...)},
	}}
}

// grantedDDL guarda lo que la regla estándar ya ha dado a cada rol del sistema, para concederlo una
// sola vez (D-98). En una base existente, lo concedido arranca como los permisos que cada rol del
// sistema lleva ahora; en una nueva, como lo que sembró la migración 3.
var grantedDDL = []string{
	`CREATE TABLE sec_role_granted_permissions (role_id {uuid} NOT NULL, code {str:200} NOT NULL, PRIMARY KEY (role_id, code),
	FOREIGN KEY (role_id) REFERENCES sec_roles (id))`,
	`INSERT INTO sec_role_granted_permissions (role_id, code) SELECT p.role_id, p.code FROM sec_role_permissions p
	WHERE p.role_id IN (SELECT r.id FROM sec_roles r WHERE r.system_role <> {false})`,
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

// Migrator devuelve el migrador del esquema del contexto.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// DropAll quita las tablas del contexto y su historial de migraciones (sólo pruebas).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range []string{"sec_sessions", "sec_user_identities", "sec_user_accesses", "sec_user_roles", "sec_users",
		"sec_role_permissions", "sec_role_granted_permissions", "sec_roles", "sec_permissions", TableOutbox, TableIntegrationOutbox, TableAuditLog} {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
	_, _ = db.ExecContext(ctx, "DELETE FROM "+sqlrepo.DefaultMigrationsTable+" WHERE context = '"+Context+"'")
}

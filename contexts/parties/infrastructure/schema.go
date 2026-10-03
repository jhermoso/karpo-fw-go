// Package infrastructure adapts the Parties context to storage: SQL mappings of its aggregates,
// the versioned schema (migrations for the five engines, with the well-known catalogs as seed),
// the catalog reader and the hot-swap factories.
package infrastructure

import (
	"context"
	"fmt"
	"slices"

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
	TableInbox             = "parties_inbox"
)

// Dialects the context supports.
var Dialects = sqlrepo.Dialects

func render(dialect, ddl string) string { return sqlrepo.RenderDDL(dialect, ddl) }

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

var phase2DDL = []string{
	`CREATE TABLE document_types (id {uuid} NOT NULL PRIMARY KEY, code {str:10} NOT NULL, name {str:200} NOT NULL,
	default_pattern {str:300}, requires_expiry {bool} NOT NULL, requires_authority {bool} NOT NULL, active {bool} NOT NULL)`,
	`CREATE TABLE country_document_rules (id {str:64} NOT NULL PRIMARY KEY, country {str:2} NOT NULL, doc_type {uuid} NOT NULL,
	available {bool} NOT NULL, is_default {bool} NOT NULL, display_order {bigint} NOT NULL, pattern {str:300},
	min_length {bigint}, max_length {bigint}, check_digit {str:40}, requires_expiry {bool}, requires_authority {bool},
	active {bool} NOT NULL, FOREIGN KEY (doc_type) REFERENCES document_types (id))`,
	`CREATE TABLE classification_types (id {uuid} NOT NULL PRIMARY KEY, name {str:200} NOT NULL, description {str:500},
	family_id {uuid}, active {bool} NOT NULL, applies_to {str:20} NOT NULL, exclusive_family {bool} NOT NULL)`,
	`CREATE TABLE party_identifications (party_id {uuid} NOT NULL, id {uuid} NOT NULL, doc_type {uuid} NOT NULL,
	country {str:2} NOT NULL, doc_number {str:60} NOT NULL, issuing_authority {str:200}, issued_on {date}, expires_on {date},
	is_primary {bool} NOT NULL, PRIMARY KEY (party_id, id),
	FOREIGN KEY (party_id) REFERENCES parties (id), FOREIGN KEY (doc_type) REFERENCES document_types (id))`,
	`CREATE UNIQUE INDEX ux_party_identifications_doc ON party_identifications (doc_type, country, doc_number)`,
	`CREATE INDEX ix_party_identifications_number ON party_identifications (doc_number)`,
	`CREATE TABLE party_contacts (party_id {uuid} NOT NULL, id {uuid} NOT NULL, kind {str:10} NOT NULL,
	contact_value {str:320}, street_type {str:10}, line1 {str:200}, line2 {str:200}, directions {str:500},
	postal_code {str:20}, locality {str:100}, region {str:100}, country {str:2}, geo_postal_code {uuid}, geo_boundary {uuid},
	purposes {str:100}, non_solicitation {bool} NOT NULL, valid_from {ts} NOT NULL, valid_to {ts},
	PRIMARY KEY (party_id, id), FOREIGN KEY (party_id) REFERENCES parties (id))`,
	`CREATE INDEX ix_party_contacts_value ON party_contacts (contact_value)`,
	`CREATE TABLE party_classifications (party_id {uuid} NOT NULL, id {uuid} NOT NULL, class_type {uuid} NOT NULL,
	valid_from {ts} NOT NULL, valid_to {ts}, PRIMARY KEY (party_id, id),
	FOREIGN KEY (party_id) REFERENCES parties (id), FOREIGN KEY (class_type) REFERENCES classification_types (id))`,
	`CREATE INDEX ix_party_classifications_type ON party_classifications (class_type)`,
}

// phase3DDL adds legal forms, shared catalog entries, affiliations (visibility, decision P1)
// and hierarchical relationship types. {add:x} renders the dialect's ADD COLUMN clause.
var phase3DDL = []string{
	`ALTER TABLE parties {add:legal_form} {str:30}{addEnd}`,
	`ALTER TABLE parties {add:shared} {bool} DEFAULT {false} NOT NULL{addEnd}`,
	`ALTER TABLE relationship_types {add:hierarchical} {bool} DEFAULT {false} NOT NULL{addEnd}`,
	`CREATE TABLE party_affiliations (party_id {uuid} NOT NULL, relationship_id {uuid} NOT NULL, organization {uuid} NOT NULL,
	valid_from {ts} NOT NULL, valid_to {ts}, PRIMARY KEY (party_id, relationship_id),
	FOREIGN KEY (party_id) REFERENCES parties (id), FOREIGN KEY (organization) REFERENCES parties (id))`,
	`CREATE INDEX ix_party_affiliations_org ON party_affiliations (organization)`,
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

// inboxDDL returns the inbox of the context (the integration events it consumes) for a dialect.
func inboxDDL(dialect string) []string {
	switch dialect {
	case "sqlite":
		return sqlite.InboxDDL(TableInbox)
	case "postgres":
		return postgres.InboxDDL(TableInbox)
	case "sqlserver":
		return sqlserver.InboxDDL(TableInbox)
	case "oracle":
		return oracle.InboxDDL(TableInbox)
	case "mysql":
		return mysql.InboxDDL(TableInbox)
	}
	return nil
}

// Migrations is the versioned schema of the Parties context.
func Migrations() sqlrepo.MigrationSet {
	initial := map[string][]string{}
	technical := map[string][]string{}
	inbox := map[string][]string{}
	phase2 := map[string][]string{}
	phase3 := map[string][]string{}
	for _, d := range Dialects {
		for _, s := range phase3DDL {
			phase3[d] = append(phase3[d], render(d, s))
		}
		for _, s := range initialDDL {
			initial[d] = append(initial[d], render(d, s))
		}
		for _, s := range phase2DDL {
			phase2[d] = append(phase2[d], render(d, s))
		}
		technical[d] = technicalDDL(d)
		inbox[d] = inboxDDL(d)
	}
	return sqlrepo.MigrationSet{Context: Context, Migrations: []sqlrepo.Migration{
		{Version: 1, Name: "parties, roles and relationships", Up: initial},
		{Version: 2, Name: "outboxes and audit log", Up: technical},
		{Version: 3, Name: "well-known role and relationship types", Run: seedCatalogs},
		{Version: 4, Name: "identifications, contacts and classifications", Up: phase2},
		{Version: 5, Name: "document types, country document rules and classification types", Run: seedPhase2},
		{Version: 6, Name: "legal forms, shared parties, affiliations and hierarchical relationships", Up: phase3},
		{Version: 7, Name: "organization rollup: any unit to any organization, hierarchical", Run: generalizeRollup},
		{Version: 8, Name: "facility roles (the party side of Facilities)", Up: sqlrepo.RenderDDLAll(
			`CREATE TABLE facility_role_types (id {uuid} NOT NULL PRIMARY KEY, name {str:100} NOT NULL, description {str:500}, active {bool} NOT NULL)`,
			`CREATE TABLE party_facility_roles (party_id {uuid} NOT NULL, id {uuid} NOT NULL, facility {uuid} NOT NULL, role_type {uuid} NOT NULL,
	valid_from {ts} NOT NULL, valid_to {ts}, PRIMARY KEY (party_id, id), FOREIGN KEY (party_id) REFERENCES parties (id),
	FOREIGN KEY (role_type) REFERENCES facility_role_types (id))`,
			`CREATE INDEX ix_party_facility_roles_facility ON party_facility_roles (facility)`)},
		{Version: 9, Name: "facility role types", Run: func(ctx context.Context, db *sqlrepo.DB) error {
			var rows [][]any
			for _, t := range domain.WellKnownFacilityRoleTypes() {
				rows = append(rows, []any{t.ID, t.Name, t.Description, t.Active})
			}
			return db.InsertMany(ctx, "facility_role_types", []string{"id", "name", "description", "active"}, rows)
		}},
		{Version: 10, Name: "inbox (integration events consumed from HR)", Up: inbox},
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
		if t.ID == domain.RelOrganizationRollup { // seeded as in C#; migration 7 generalizes it
			t.FromRole, t.ToRole, t.Description = domain.RoleDepartment, domain.RoleDivision, "Department belongs to a division"
		}
		if err := db.Insert(ctx, "relationship_types", sqlrepo.Values{"id": t.ID, "name": t.Name.String(),
			"description": t.Description, "from_role": t.FromRole, "to_role": t.ToRole}); err != nil {
			return fmt.Errorf("seeding relationship type %s: %w", t.Name, err)
		}
	}
	return nil
}

// seedPhase2 inserts the document and classification catalogs (C# GUIDs). Families go first.
func seedPhase2(ctx context.Context, db *sqlrepo.DB) error {
	optBool := func(b *bool) any {
		if b == nil {
			return nil
		}
		return *b
	}
	optInt := func(n int) any {
		if n == 0 {
			return nil
		}
		return int64(n)
	}
	for _, t := range domain.WellKnownDocumentTypes() {
		if err := db.Insert(ctx, "document_types", sqlrepo.Values{"id": t.ID, "code": t.Code, "name": t.Name.String(),
			"default_pattern": nullable(t.DefaultPattern), "requires_expiry": t.RequiresExpiry,
			"requires_authority": t.RequiresIssuingAuthority, "active": t.Active}); err != nil {
			return fmt.Errorf("seeding document type %s: %w", t.Code, err)
		}
	}
	for _, r := range domain.WellKnownCountryDocumentRules() {
		if err := db.Insert(ctx, "country_document_rules", sqlrepo.Values{"id": r.ID, "country": r.Country.String(),
			"doc_type": r.DocumentType, "available": r.Available, "is_default": r.Default, "display_order": int64(r.DisplayOrder),
			"pattern": nullable(r.Pattern), "min_length": optInt(r.MinLength), "max_length": optInt(r.MaxLength),
			"check_digit": nullable(r.CheckDigit), "requires_expiry": optBool(r.RequiresExpiry),
			"requires_authority": optBool(r.RequiresIssuingAuthority), "active": r.Active}); err != nil {
			return fmt.Errorf("seeding country document rule %s: %w", r.ID, err)
		}
	}
	for _, t := range domain.WellKnownClassificationTypes() {
		var family any
		if t.Family != nil {
			family = *t.Family
		}
		if err := db.Insert(ctx, "classification_types", sqlrepo.Values{"id": t.ID, "name": t.Name.String(),
			"description": nullable(t.Description), "family_id": family, "active": t.Active,
			"applies_to": string(t.AppliesTo), "exclusive_family": t.Exclusive}); err != nil {
			return fmt.Errorf("seeding classification %s: %w", t.Name, err)
		}
	}
	return nil
}

// generalizeRollup updates the seeded Organization Rollup type (see domain.WellKnownRelationshipTypes).
func generalizeRollup(ctx context.Context, db *sqlrepo.DB) error {
	for _, t := range domain.WellKnownRelationshipTypes() {
		if t.ID != domain.RelOrganizationRollup {
			continue
		}
		_, err := db.Update(ctx, "relationship_types", sqlrepo.Values{"from_role": t.FromRole, "to_role": t.ToRole,
			"description": t.Description, "hierarchical": t.Hierarchical}, sqlrepo.Values{"id": t.ID})
		return err
	}
	return nil
}

// Migrator returns the schema migrator of Parties on db.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// DropAll removes every table of the context (tests only).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range []string{"party_relationships", "party_roles", "party_identifications", "party_contacts", "party_affiliations",
		"party_facility_roles", "facility_role_types",
		"party_classifications", "parties", "relationship_types", "role_types", "country_document_rules",
		"document_types", "classification_types",
		TablePartiesOutbox, TableIntegrationOutbox, TableAuditLog, sqlrepo.DefaultMigrationsTable, sqlrepo.DefaultMigrationsLock} {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
}

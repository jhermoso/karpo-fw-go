// Package infrastructure adapta el contexto Parties al almacenamiento: mapeos SQL de sus
// agregados, el esquema versionado (migraciones para los cinco motores, con los catálogos
// conocidos como siembra), el lector de catálogos y las factorías intercambiables en caliente.
package infrastructure

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
)

// Context es el nombre del contexto delimitado (historial de migraciones, origen de integración).
const Context = "parties"

// Tablas propias del contexto. Las tablas técnicas llevan el prefijo del contexto para que varios
// contextos puedan compartir una base de datos.
const (
	TablePartiesOutbox     = "parties_outbox"
	TableIntegrationOutbox = "parties_integration_outbox"
	TableAuditLog          = "parties_audit_log"
	TableInbox             = "parties_inbox"
)

// Dialects son los dialectos que admite el contexto.
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

// phase3DDL añade formas jurídicas, entradas de catálogo compartidas, afiliaciones (visibilidad,
// decisión P1) y tipos de relación jerárquicos. {add:x} genera la cláusula ADD COLUMN del dialecto.
var phase3DDL = []string{
	`ALTER TABLE parties {add:legal_form} {str:30}{addEnd}`,
	`ALTER TABLE parties {add:shared} {bool} DEFAULT {false} NOT NULL{addEnd}`,
	`ALTER TABLE relationship_types {add:hierarchical} {bool} DEFAULT {false} NOT NULL{addEnd}`,
	`CREATE TABLE party_affiliations (party_id {uuid} NOT NULL, relationship_id {uuid} NOT NULL, organization {uuid} NOT NULL,
	valid_from {ts} NOT NULL, valid_to {ts}, PRIMARY KEY (party_id, relationship_id),
	FOREIGN KEY (party_id) REFERENCES parties (id), FOREIGN KEY (organization) REFERENCES parties (id))`,
	`CREATE INDEX ix_party_affiliations_org ON party_affiliations (organization)`,
}

// technicalDDL devuelve los outboxes y el registro de auditoría del contexto para un dialecto.
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

// inboxDDL devuelve el inbox del contexto (los eventos de integración que consume) para un
// dialecto.
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

// Migrations es el esquema versionado del contexto Parties.
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
		{Version: 11, Name: "relationship type codes and the prospect relationship type", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE relationship_types {add:code} {str:40}{addEnd}`), Run: seedRelationshipCodes},
		// Los detalles por tipo de relación son columnas anulables de party_relationships, como los
		// detalles de personas y organizaciones son columnas de parties.
		{Version: 12, Name: "prospect relationship details: trial", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE party_relationships {add:trial_until} {ts}{addEnd}`)},
		{Version: 13, Name: "ownership relationship details: share", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE party_relationships {add:share_percent} {str:10}{addEnd}`)},
		{Version: 14, Name: "collaborator relationship details: promotion code", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE party_relationships {add:promotion_code} {str:15}{addEnd}`,
			`CREATE INDEX ix_party_rel_promotion ON party_relationships (promotion_code)`)},
		// Dónde está una dirección dentro de la calle, pieza a pieza (docs/GEOGRAFIA-NUTS.md, N-5).
		{Version: 15, Name: "postal address: number, block, portal, staircase, floor and door", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE party_contacts {add:addr_number} {str:10}{addEnd}`,
			`ALTER TABLE party_contacts {add:addr_qualifier} {str:10}{addEnd}`,
			`ALTER TABLE party_contacts {add:addr_block} {str:10}{addEnd}`,
			`ALTER TABLE party_contacts {add:addr_portal} {str:10}{addEnd}`,
			`ALTER TABLE party_contacts {add:addr_staircase} {str:10}{addEnd}`,
			`ALTER TABLE party_contacts {add:addr_floor} {str:10}{addEnd}`,
			`ALTER TABLE party_contacts {add:addr_door} {str:10}{addEnd}`)},
		// Un código estable para los tipos de rol, como el que tienen los tipos de relación
		// (docs/EXTENSIBILIDAD.md, X-3).
		{Version: 16, Name: "role type codes", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE role_types {add:code} {str:40}{addEnd}`), Run: seedRoleCodes},
		// El número con el que una organización conoce a su cliente pertenece a la pareja
		// (docs/PARTIES-MODELO-COMUN.md, D-10).
		{Version: 17, Name: "customer relationship details: customer number", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE party_relationships {add:customer_number} {str:20}{addEnd}`,
			`CREATE INDEX ix_party_rel_customer_no ON party_relationships (to_party, customer_number)`)},
		// La marca de una party cuyo documento de identidad fue rechazado
		// (docs/PARTIES-MODELO-COMUN.md, D-9).
		{Version: 18, Name: "mark of a refused identity document", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE parties {add:doc_issue} {bool} DEFAULT {false} NOT NULL{addEnd}`,
			`ALTER TABLE parties {add:doc_issue_number} {str:60}{addEnd}`,
			`ALTER TABLE parties {add:doc_issue_reason} {str:200}{addEnd}`)},
		// El tipo de rol que era «Other Organization Unit» se llama «Team» en todos los proyectos
		// (docs/DECISIONES-COMUNES.md, D-54).
		{Version: 19, Name: "role type Team", Run: renameTeamRole},
		// Una persona puede ser proveedor, y también organización interna: los dos tipos de rol
		// salen de la rama de las organizaciones (docs/DECISIONES-COMUNES.md, D-53).
		{Version: 20, Name: "supplier and internal organization are roles of any party", Run: moveRolesOfAnyParty},
		// Todos los proyectos siembran los mismos tipos: los que le faltaban a este catálogo, y
		// Team entre las organizaciones informales (docs/DECISIONES-COMUNES.md, D-62).
		{Version: 21, Name: "the common catalog of role and relationship types", Run: seedCommonCatalog},
		// La marca de un rol dado por error que no se ha podido quitar
		// (docs/DECISIONES-COMUNES.md, D-8).
		{Version: 22, Name: "annulled roles", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE party_roles {add:annulled} {bool} DEFAULT {false} NOT NULL{addEnd}`)},
		// Prospect Relationship deja …0016 al Duplicate Of de Trench, que siembran todos los
		// proyectos (docs/DECISIONES-COMUNES.md, D-62). La migración 21 hace lo mismo en una base
		// que encuentra por primera vez; aquí llega a las que ya la habían pasado.
		{Version: 23, Name: "prospect relationship moves to …0017; duplicate of", Run: seedCommonCatalog},
		// La marca de una party que es el duplicado de otra (docs/DECISIONES-COMUNES.md, D-64).
		{Version: 24, Name: "mark of a duplicate party", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE parties {add:duplicate} {bool} DEFAULT {false} NOT NULL{addEnd}`)},
		// El perfil de actividad del autónomo, en su rol Self Employed (docs/DECISIONES-COMUNES.md,
		// D-85). Como los detalles de las relaciones, son columnas anulables de la fila del rol.
		{Version: 25, Name: "self-employed role details: trade name, activity code and registration", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE party_roles {add:trade_name} {str:120}{addEnd}`,
			`ALTER TABLE party_roles {add:activity_code} {str:10}{addEnd}`,
			`ALTER TABLE party_roles {add:registered_on} {date}{addEnd}`)},
		{Version: 26, Name: "decimal columns", Run: decimalColumnsV26.Run},
		// De qué relación viene una relación de negocio que pasó a otra party al fusionar, o volvió
		// al deshacer la fusión (docs/DECISIONES-COMUNES.md, D-93).
		{Version: 27, Name: "business relationships transferred by a merge", Up: sqlrepo.RenderDDLAll(
			`ALTER TABLE party_relationships {add:transferred_from} {uuid}{addEnd}`,
			`CREATE INDEX ix_party_rel_transferred ON party_relationships (transferred_from)`)},
		// Las incidencias de calidad de datos de una ficha: un dato guardado con formato erróneo, que
		// avisa sin impedir guardar (docs/DECISIONES-COMUNES.md, D-92). Una tabla genérica por ficha,
		// como la party_data_quality_issue de Trench; subject_id es el objeto de la ficha (hoy, el
		// contacto) y field, su campo. El filtro de la búsqueda entra por la clave (party_id).
		{Version: 28, Name: "data quality issues of a party", Up: sqlrepo.RenderDDLAll(
			`CREATE TABLE party_data_quality_issues (party_id {uuid} NOT NULL, id {uuid} NOT NULL, subject_id {uuid},
	field {str:60} NOT NULL, code {str:40} NOT NULL, severity {str:10} NOT NULL, message {str:200} NOT NULL,
	issue_value {str:320}, raised_at {ts} NOT NULL, PRIMARY KEY (party_id, id), FOREIGN KEY (party_id) REFERENCES parties (id))`)},
		// Los códigos no se repiten aunque haya más de un proceso (BACKLOG.md §2d, docs/PARTIES.md
		// §22): un índice único sobre el código de los tipos de rol y de relación, como los
		// uq_role_type_code y uq_party_relationship_type_code de Trench, y otro sobre el código de
		// promoción de los colaboradores vigentes de cada organización (promotionKey). Varios tipos
		// sin código conviven en los cinco motores ({whereNotNull}); un código vacío de una base
		// antigua pasa a NULL. Si ya hay códigos repetidos, repeatedCodes detiene la migración antes
		// de tocar nada y dice cuáles.
		{Version: 29, Name: "unique codes of role types, relationship types and promotions", Check: repeatedCodes,
			Up: sqlrepo.RenderDDLAll(
				`UPDATE role_types SET code = NULL WHERE code = ''`,
				`UPDATE relationship_types SET code = NULL WHERE code = ''`,
				`UPDATE party_relationships SET promotion_code = NULL WHERE promotion_code = ''`,
				`ALTER TABLE party_relationships {add:promotion_key} {str:52}{addEnd}`,
				`CREATE UNIQUE INDEX `+uniqueRoleTypeCode+` ON role_types (code){whereNotNull:code}`,
				`CREATE UNIQUE INDEX `+uniqueRelationshipTypeCode+` ON relationship_types (code){whereNotNull:code}`,
				`CREATE UNIQUE INDEX `+uniquePromotionKey+` ON party_relationships (promotion_key){whereNotNull:promotion_key}`),
			Run: fillPromotionKeys},
	}}
}

// openPromotion es una relación sin fin que lleva código de promoción.
type openPromotion struct {
	id, to fw.UUID
	code   string
}

// openPromotions lee las relaciones sin fin que llevan código de promoción.
func openPromotions(ctx context.Context, db *sqlrepo.DB) ([]openPromotion, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, to_party, promotion_code FROM party_relationships
	WHERE promotion_code IS NOT NULL AND valid_to IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []openPromotion
	for rows.Next() {
		var id, to any
		var code sql.NullString
		if err := rows.Scan(&id, &to, &code); err != nil {
			return nil, err
		}
		p := openPromotion{code: code.String}
		if p.id, err = db.Dialect().ParseUUID(id); err != nil {
			return nil, err
		}
		if p.to, err = db.Dialect().ParseUUID(to); err != nil {
			return nil, err
		}
		if p.code != "" {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// sameCode es la forma en que se comparan dos códigos al buscar repetidos: sin espacios en los
// extremos y sin distinguir mayúsculas, como los comparan las intercalaciones habituales de SQL
// Server y MySQL ("Sol-1" y "SOL-1", "abc" y "abc "), que el índice único rechazaría. En
// PostgreSQL, SQLite y Oracle la comprobación es así algo más estricta que el índice.
func sameCode(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }

// repeatedCodes se niega a crear los índices únicos de la migración 29 si ya hay códigos
// repetidos, y dice cuáles y en qué filas: hay que dar a cada uno el suyo (no se borra nada) y
// migrar de nuevo.
func repeatedCodes(ctx context.Context, db *sqlrepo.DB) error {
	var found []string
	report := func(what string, ids map[string][]string) {
		keys := make([]string, 0, len(ids))
		for k, v := range ids {
			if len(v) > 1 {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			found = append(found, fmt.Sprintf("%s %s: %s", what, k, strings.Join(ids[k], ", ")))
		}
	}
	for _, table := range []string{"role_types", "relationship_types"} {
		rows, err := db.Select(ctx, table, []string{"id", "code"}, "id")
		if err != nil {
			return err
		}
		ids := map[string][]string{}
		for _, r := range rows {
			if code := r.String("code"); sameCode(code) != "" {
				k := fmt.Sprintf("%q", sameCode(code))
				ids[k] = append(ids[k], fmt.Sprintf("%s (%q)", r.UUID("id"), code))
			}
		}
		report(table+" code", ids)
	}
	open, err := openPromotions(ctx, db)
	if err != nil {
		return err
	}
	ids := map[string][]string{}
	for _, p := range open {
		if sameCode(p.code) == "" {
			continue
		}
		k := fmt.Sprintf("%q of the organization %s", sameCode(p.code), p.to)
		ids[k] = append(ids[k], fmt.Sprintf("%s (%q)", p.id, p.code))
	}
	report("promotion code", ids)
	if len(found) == 0 {
		return nil
	}
	return fmt.Errorf("these codes must be unique and are repeated (give each its own and migrate again; nothing was changed): %s",
		strings.Join(found, "; "))
}

// fillPromotionKeys da su promotion_key a las relaciones sin fin que ya tenían código de
// promoción.
func fillPromotionKeys(ctx context.Context, db *sqlrepo.DB) error {
	open, err := openPromotions(ctx, db)
	if err != nil {
		return err
	}
	for _, p := range open {
		if _, err := db.Update(ctx, "party_relationships", sqlrepo.Values{"promotion_key": promotionKey(p.to, p.code, true)},
			sqlrepo.Values{"id": p.id}); err != nil {
			return err
		}
	}
	return nil
}

// seedCommonCatalog añade los tipos conocidos que no tiene una base sembrada antes, y cuelga Team
// de donde dice ahora la siembra. Una base sembrada con esta versión ya los tiene todos.
func seedCommonCatalog(ctx context.Context, db *sqlrepo.DB) error {
	if err := moveProspect(ctx, db); err != nil {
		return err
	}
	has := func(table string) (map[string]bool, error) {
		rows, err := db.Select(ctx, table, []string{"id"})
		if err != nil {
			return nil, err
		}
		ids := make(map[string]bool, len(rows))
		for _, r := range rows {
			ids[r.UUID("id").String()] = true
		}
		return ids, nil
	}
	roles, err := has("role_types")
	if err != nil {
		return err
	}
	for _, t := range domain.WellKnownRoleTypes() { // los padres van primero
		switch {
		case t.ID == domain.RoleTeam:
			if _, err := db.Update(ctx, "role_types", sqlrepo.Values{"parent_id": *t.Parent}, sqlrepo.Values{"id": t.ID}); err != nil {
				return fmt.Errorf("moving role type %s: %w", t.Name, err)
			}
		case !roles[t.ID.String()]:
			if err := codeTaken(ctx, db, "role_types", t.Code, t.ID.String()); err != nil {
				return err
			}
			var parent any
			if t.Parent != nil {
				parent = *t.Parent
			}
			if err := db.Insert(ctx, "role_types", sqlrepo.Values{"id": t.ID, "name": t.Name.String(), "description": t.Description,
				"parent_id": parent, "category": t.Category, "code": t.Code}); err != nil {
				return fmt.Errorf("seeding role type %s: %w", t.Name, err)
			}
		}
	}
	rels, err := has("relationship_types")
	if err != nil {
		return err
	}
	for _, t := range domain.WellKnownRelationshipTypes() {
		if rels[t.ID.String()] {
			continue
		}
		if err := codeTaken(ctx, db, "relationship_types", t.Code, t.ID.String()); err != nil {
			return err
		}
		if err := db.Insert(ctx, "relationship_types", sqlrepo.Values{"id": t.ID, "name": t.Name.String(), "description": t.Description,
			"from_role": t.FromRole, "to_role": t.ToRole, "hierarchical": t.Hierarchical, "code": t.Code}); err != nil {
			return fmt.Errorf("seeding relationship type %s: %w", t.Name, err)
		}
	}
	return nil
}

// oldProspect es donde estaba Prospect Relationship hasta que Duplicate Of tomó su id.
const oldProspect = "10000000-0000-0000-0002-000000000016"

// moveProspect da a Prospect Relationship su id nuevo donde una base aún lo tiene en el antiguo:
// se copia el tipo, sus relaciones lo siguen y la fila antigua se va. No pasa nada donde el id
// antiguo tiene otro tipo (una base sembrada con Duplicate Of ahí).
func moveProspect(ctx context.Context, db *sqlrepo.DB) error {
	rows, err := db.Select(ctx, "relationship_types", []string{"id", "name", "description", "from_role", "to_role", "hierarchical", "code"})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.UUID("id").String() != oldProspect || r.String("code") != domain.CodeProspect {
			continue
		}
		if err := db.Insert(ctx, "relationship_types", sqlrepo.Values{"id": domain.RelProspect, "name": r.String("name"),
			"description": r.String("description"), "from_role": r.UUID("from_role"), "to_role": r.UUID("to_role"),
			"hierarchical": r.Bool("hierarchical")}); err != nil {
			return fmt.Errorf("moving the prospect relationship type: %w", err)
		}
		old, err := domain.ParseRelationshipTypeID(oldProspect)
		if err != nil {
			return err
		}
		if _, err := db.Update(ctx, "party_relationships", sqlrepo.Values{"rel_type": domain.RelProspect}, sqlrepo.Values{"rel_type": old}); err != nil {
			return fmt.Errorf("moving the prospect relationships: %w", err)
		}
		if _, err := db.Delete(ctx, "relationship_types", sqlrepo.Values{"id": old}); err != nil {
			return fmt.Errorf("removing the old prospect relationship type: %w", err)
		}
		// El código pasa al tipo nuevo cuando el viejo ya no está, porque con el índice único de
		// la migración 29 los dos no pueden tenerlo a la vez. Sólo lo nota la prueba que vuelve a
		// sembrar sobre una base ya migrada (TestSeedCommonCatalog_CompletesAnEarlierSeed): en las
		// bases reales este Run ya se aplicó antes de la 29, no se repite y no entra en la suma de
		// control, así que el cambio no altera nada.
		if _, err := db.Update(ctx, "relationship_types", sqlrepo.Values{"code": domain.CodeProspect}, sqlrepo.Values{"id": domain.RelProspect}); err != nil {
			return fmt.Errorf("moving the prospect relationship type code: %w", err)
		}
	}
	return nil
}

// moveRolesOfAnyParty cuelga Supplier e Internal Organization de donde dice ahora la siembra.
func moveRolesOfAnyParty(ctx context.Context, db *sqlrepo.DB) error {
	for _, t := range domain.WellKnownRoleTypes() {
		if t.ID != domain.RoleSupplier && t.ID != domain.RoleInternalOrganization {
			continue
		}
		if _, err := db.Update(ctx, "role_types", sqlrepo.Values{"parent_id": *t.Parent, "description": t.Description},
			sqlrepo.Values{"id": t.ID}); err != nil {
			return fmt.Errorf("moving role type %s: %w", t.Name, err)
		}
	}
	return nil
}

// codeTaken se niega a dar a un tipo conocido un código que ya tiene un tipo añadido al catálogo:
// dos tipos con un mismo código dejarían el catálogo ilegible, y el contexto con él.
func codeTaken(ctx context.Context, db *sqlrepo.DB, table, code, id string) error {
	rows, err := db.Select(ctx, table, []string{"id", "code"})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.String("code") == code && r.UUID("id").String() != id {
			return fmt.Errorf("%s: the code %q that the program needs is used by %s: give that one another code and migrate again",
				table, code, r.UUID("id"))
		}
	}
	return nil
}

// renameTeamRole da al tipo de rol su nombre y su código comunes donde una siembra anterior dejó
// los antiguos.
func renameTeamRole(ctx context.Context, db *sqlrepo.DB) error {
	for _, t := range domain.WellKnownRoleTypes() {
		if t.ID != domain.RoleTeam {
			continue
		}
		if err := codeTaken(ctx, db, "role_types", t.Code, t.ID.String()); err != nil {
			return err
		}
		_, err := db.Update(ctx, "role_types", sqlrepo.Values{"name": t.Name.String(), "description": t.Description, "code": t.Code},
			sqlrepo.Values{"id": t.ID})
		return err
	}
	return nil
}

// seedRoleCodes da su código a los tipos de rol conocidos.
func seedRoleCodes(ctx context.Context, db *sqlrepo.DB) error {
	for _, t := range domain.WellKnownRoleTypes() {
		if _, err := db.Update(ctx, "role_types", sqlrepo.Values{"code": t.Code}, sqlrepo.Values{"id": t.ID}); err != nil {
			return fmt.Errorf("coding role type %s: %w", t.Name, err)
		}
	}
	return nil
}

// seedRelationshipCodes da su código a los tipos de relación conocidos y añade el tipo de relación
// de prospect (docs/PARTIES-UDM.md). El código es único entre los tipos que tienen uno; el
// catálogo lo comprueba al cargarse, porque un índice único sobre una columna anulable no se
// comporta igual en todos los motores.
func seedRelationshipCodes(ctx context.Context, db *sqlrepo.DB) error {
	for _, t := range domain.WellKnownRelationshipTypes() {
		if t.ID == domain.RelProspect {
			if err := db.Insert(ctx, "relationship_types", sqlrepo.Values{"id": t.ID, "name": t.Name.String(),
				"description": t.Description, "from_role": t.FromRole, "to_role": t.ToRole, "hierarchical": t.Hierarchical,
				"code": t.Code}); err != nil {
				return fmt.Errorf("seeding relationship type %s: %w", t.Name, err)
			}
			continue
		}
		if _, err := db.Update(ctx, "relationship_types", sqlrepo.Values{"code": t.Code}, sqlrepo.Values{"id": t.ID}); err != nil {
			return fmt.Errorf("coding relationship type %s: %w", t.Name, err)
		}
	}
	return nil
}

// seedCatalogs inserta los catálogos conocidos con los mismos GUID que el WellKnownCatalog de C#.
// Los padres van primero: el orden de WellKnownRoleTypes ya respeta la jerarquía.
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
		if t.ID == domain.RelProspect { // lo añade la migración 11, con los códigos
			continue
		}
		if t.ID == domain.RelOrganizationRollup { // se siembra como en C#; la migración 7 lo generaliza
			t.FromRole, t.ToRole, t.Description = domain.RoleDepartment, domain.RoleDivision, "Department belongs to a division"
		}
		if err := db.Insert(ctx, "relationship_types", sqlrepo.Values{"id": t.ID, "name": t.Name.String(),
			"description": t.Description, "from_role": t.FromRole, "to_role": t.ToRole}); err != nil {
			return fmt.Errorf("seeding relationship type %s: %w", t.Name, err)
		}
	}
	return nil
}

// seedPhase2 inserta los catálogos de documentos y de clasificaciones (GUID de C#). Las familias
// van primero.
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

// generalizeRollup actualiza el tipo Organization Rollup sembrado (ver
// domain.WellKnownRelationshipTypes).
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

// Migrator devuelve el migrador del esquema de Parties sobre db.
func Migrator(db *sqlrepo.DB) (*sqlrepo.Migrator, error) {
	return sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{Migrations()})
}

// DropAll elimina todas las tablas del contexto (sólo para pruebas).
func DropAll(ctx context.Context, db *sqlrepo.DB) {
	for _, t := range []string{"party_relationships", "party_roles", "party_identifications", "party_contacts", "party_affiliations",
		"party_facility_roles", "facility_role_types", "party_data_quality_issues",
		"party_classifications", "parties", "relationship_types", "role_types", "country_document_rules",
		"document_types", "classification_types",
		TablePartiesOutbox, TableIntegrationOutbox, TableAuditLog, sqlrepo.DefaultMigrationsTable, sqlrepo.DefaultMigrationsLock} {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+t)
	}
}

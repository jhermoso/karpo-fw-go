package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	_ "modernc.org/sqlite"
)

// openSQLite abre una base SQLite nueva y vacía.
func openSQLite(t *testing.T) (*sql.DB, *sqlrepo.DB) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "codes.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	return raw, sqlite.Open(raw, sqlrepo.WithName("sqlite"))
}

func violation(err error, code string) bool {
	var v *fw.RuleViolationError
	return errors.As(err, &v) && v.Code == code
}

// Los índices únicos de la migración 29 detienen lo que la comprobación previa del caso de uso no
// ve (otro proceso que da de alta lo mismo a la vez): un código repetido es la misma violación de
// regla (422) que da el caso de uso, no un error del servidor. Varios tipos sin código conviven.
func TestUniqueCodes_TheIndexesStopWhatTheCheckDoesNotSee(t *testing.T) {
	ctx := context.Background()
	_, db := openSQLite(t)
	m, err := Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c := SQLCatalogs{db: db}

	roleType := func(name, code string) domain.RoleType {
		t.Helper()
		rt, err := domain.NewRoleType(domain.RoleTypeID{UUID: fw.NewUUID()}, name, "", code, domain.RolePersonCategory)
		if err != nil {
			t.Fatal(err)
		}
		return rt
	}
	if err := c.AddRoleType(ctx, roleType("Guarantor", "guarantor")); err != nil {
		t.Fatal(err)
	}
	if err := c.AddRoleType(ctx, roleType("Avalista", "guarantor")); !violation(err, "parties.role_type_code_taken") {
		t.Fatalf("a role type code already taken: %v", err)
	}
	for _, n := range []string{"Fiador", "Garante"} {
		if err := c.AddRoleType(ctx, roleType(n, "")); err != nil {
			t.Fatalf("role types without a code: %v", err)
		}
	}

	relType := func(name, code string) domain.RelationshipType {
		t.Helper()
		rt, err := domain.NewRelationshipType(domain.RelationshipTypeID{UUID: fw.NewUUID()}, name, "", code, domain.RoleEmployee, domain.RoleCustomer)
		if err != nil {
			t.Fatal(err)
		}
		return rt
	}
	if err := c.AddRelationshipType(ctx, relType("Guarantee", "guarantee")); err != nil {
		t.Fatal(err)
	}
	if err := c.AddRelationshipType(ctx, relType("Aval", "guarantee")); !violation(err, "parties.relationship_type_code_taken") {
		t.Fatalf("a relationship type code already taken: %v", err)
	}
	if err := c.AddRelationshipType(ctx, relType("Aval", "prospect")); !violation(err, "parties.relationship_type_code_taken") {
		t.Fatalf("the code of a relationship type of the program: %v", err)
	}
	for _, n := range []string{"Fianza", "Garantía"} {
		if err := c.AddRelationshipType(ctx, relType(n, "")); err != nil {
			t.Fatalf("relationship types without a code: %v", err)
		}
	}
}

// El código de promoción no se repite entre los colaboradores vigentes de una organización; los que
// no tienen código conviven, y el de una relación que termina queda libre.
func TestUniqueCodes_PromotionCodeOfOpenCollaborators(t *testing.T) {
	ctx := context.Background()
	raw, db := openSQLite(t)
	m, err := Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	ids := insertCollaborators(t, raw, db.Dialect(), "", "")
	repo, err := RelationshipRepositoryFactory(db)
	if err != nil {
		t.Fatal(err)
	}
	collaborator := wellKnownRelationshipType(t, domain.RelCollaborator)
	update := func(id domain.RelationshipID, fn func(r *domain.Relationship) error) error {
		r, err := repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := fn(r); err != nil {
			return err
		}
		return repo.Save(ctx, r)
	}
	give := func(id domain.RelationshipID, code string) error {
		return update(id, func(r *domain.Relationship) error { return r.SetPromotionCode(collaborator, code) })
	}
	if err := give(ids[0], "SOL-1"); err != nil {
		t.Fatal(err)
	}
	if err := give(ids[1], "SOL-1"); !violation(err, "parties.duplicate_promotion_code") {
		t.Fatalf("the code of another open collaborator: %v", err)
	}
	if err := update(ids[0], func(r *domain.Relationship) error { return r.Terminate(fw.Now()) }); err != nil {
		t.Fatal(err)
	}
	if err := give(ids[1], "SOL-1"); err != nil {
		t.Fatalf("the code of an ended relationship is free: %v", err)
	}
}

// La migración 29 no crea los índices si ya hay códigos repetidos: se detiene sin tocar nada y dice
// cuáles. Corregidos, migra; un código vacío pasa a NULL y las relaciones vigentes con código
// reciben su clave.
func TestUniqueCodes_TheMigrationStopsOnRepeatedCodes(t *testing.T) {
	ctx := context.Background()
	raw, db := openSQLite(t)
	set := Migrations()
	before, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{{Context: set.Context, Migrations: set.Migrations[:28]}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := before.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO role_types (id, name, category, code) VALUES ('0f0f0f0f-0000-0000-0000-000000000001', 'Guarantor', 0, 'guarantor')`,
		`INSERT INTO role_types (id, name, category, code) VALUES ('0f0f0f0f-0000-0000-0000-000000000002', 'Avalista', 0, 'guarantor')`,
		`INSERT INTO role_types (id, name, category, code) VALUES ('0f0f0f0f-0000-0000-0000-000000000003', 'Fiador', 0, '')`,
		`INSERT INTO role_types (id, name, category, code) VALUES ('0f0f0f0f-0000-0000-0000-000000000004', 'Garante', 0, '')`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rels := insertCollaborators(t, raw, db.Dialect(), "SOL-1", "SOL-1")

	m, err := Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), `"guarantor"`) || !strings.Contains(err.Error(), "0f0f0f0f-0000-0000-0000-000000000002") ||
		!strings.Contains(err.Error(), `"SOL-1"`) || !strings.Contains(err.Error(), rels[1].String()) {
		t.Fatalf("repeated codes must stop the migration and be named: %v", err)
	}
	count := func(q string) (n int) {
		if err := raw.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM role_types WHERE code = ''`); n != 2 {
		t.Fatalf("a failed check changed the data: %d empty codes left", n)
	}
	if err := m.Verify(ctx); err == nil {
		t.Fatal("the migration was recorded")
	}

	for _, q := range []string{
		`UPDATE role_types SET code = 'avalista' WHERE name = 'Avalista'`,
		`UPDATE party_relationships SET promotion_code = 'SOL-2' WHERE promotion_code = 'SOL-1' AND id <> '` + rels[0].String() + `'`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT COUNT(*) FROM role_types WHERE code IS NULL`); n != 2 {
		t.Fatalf("empty codes become NULL: %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM party_relationships WHERE promotion_key LIKE '%/SOL-_'`); n != 2 {
		t.Fatalf("the open collaborators with a code get their key: %d", n)
	}
}

// insertCollaborators da de alta, como filas, una organización y dos colaboradores suyos con los
// códigos de promoción dados ("" sin código), y devuelve las dos relaciones.
func insertCollaborators(t *testing.T, raw *sql.DB, d sqlrepo.Dialect, code1, code2 string) [2]domain.RelationshipID {
	t.Helper()
	rt := wellKnownRelationshipType(t, domain.RelCollaborator)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := raw.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	const party = `INSERT INTO parties (id, version, kind, name, active, test) VALUES (?, 1, 'organization', ?, 1, 0)`
	org := fw.NewUUID()
	exec(party, d.UUIDValue(org), "Acme")
	var out [2]domain.RelationshipID
	for i, code := range []string{code1, code2} {
		agency := fw.NewUUID()
		exec(party, d.UUIDValue(agency), "Agencia "+string(rune('A'+i)))
		out[i] = domain.RelationshipID{UUID: fw.NewUUID()}
		var promo any
		if code != "" {
			promo = code
		}
		exec(`INSERT INTO party_relationships (id, version, rel_type, from_party, to_party, from_role, to_role, valid_from, promotion_code)
			VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?)`, d.UUIDValue(out[i].UUID), d.UUIDValue(rt.ID.UUID), d.UUIDValue(agency), d.UUIDValue(org),
			d.UUIDValue(rt.FromRole.UUID), d.UUIDValue(rt.ToRole.UUID), d.TimeValue(time.Now().Add(-time.Hour)), promo)
	}
	return out
}

func wellKnownRelationshipType(t *testing.T, id domain.RelationshipTypeID) domain.RelationshipType {
	t.Helper()
	for _, rt := range domain.WellKnownRelationshipTypes() {
		if rt.ID == id {
			return rt
		}
	}
	t.Fatalf("no well-known relationship type %s", id)
	return domain.RelationshipType{}
}

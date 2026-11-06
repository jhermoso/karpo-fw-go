package infrastructure

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/contexts/parties/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// La migración 33 no crea el índice de una sola principal si alguna party tiene ya dos: se detiene
// sin tocar nada y dice cuáles. Corregido, migra.
func TestIdentificationValidity_TheMigrationStopsOnTwoPrimaries(t *testing.T) {
	ctx := context.Background()
	raw, db := openSQLite(t)
	set := Migrations()
	before, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{{Context: set.Context, Migrations: set.Migrations[:32]}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := before.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	const party = "0f0f0f0f-0000-0000-0000-000000000001"
	for _, q := range []string{
		`INSERT INTO parties (id, version, kind, name, active, test) VALUES ('` + party + `', 1, 'person', 'Ana', 1, 0)`,
		`INSERT INTO party_identifications (party_id, id, doc_type, country, doc_number, is_primary)
		VALUES ('` + party + `', '0f0f0f0f-0000-0000-0000-00000000000a', '` + domain.DocNationalID.String() + `', 'ES', '12345678Z', 1)`,
		`INSERT INTO party_identifications (party_id, id, doc_type, country, doc_number, is_primary)
		VALUES ('` + party + `', '0f0f0f0f-0000-0000-0000-00000000000b', '` + domain.DocPassport.String() + `', 'ES', 'PAA123456', 1)`,
	} {
		if _, err := raw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err == nil || !strings.Contains(err.Error(), party) || !strings.Contains(err.Error(), "0f0f0f0f-0000-0000-0000-00000000000b") {
		t.Fatalf("two primaries must stop the migration and be named: %v", err)
	}
	if err := m.Verify(ctx); !errors.Is(err, application.ErrSchemaOutdated) || errors.Is(err, application.ErrSchemaDirty) {
		t.Fatalf("the migration must stay pending, not recorded nor dirty: %v", err)
	}
	if _, err := raw.Exec(`UPDATE party_identifications SET is_primary = 0 WHERE doc_number = 'PAA123456'`); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Verify(ctx); err != nil {
		t.Fatal(err)
	}
}

// Los índices de la migración 33 cuentan sólo las identificaciones vigentes, y quien choca con
// ellos recibe la violación del caso de uso, no un error del servidor: el documento vigente de otra
// party es parties.document_taken, y dos principales vigentes, parties.duplicate_primary_identification.
// Una histórica deja pasar las dos cosas, y su valid_to se guarda y se relee.
func TestIdentificationValidity_TheIndexesCountOnlyCurrentIdentifications(t *testing.T) {
	ctx := context.Background()
	raw, db := openSQLite(t)
	m, err := Migrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := PartyRepositoryFactory(db)
	if err != nil {
		t.Fatal(err)
	}
	es := vocab.MustCountryCode("ES")
	closedAt := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	doc := func(typ domain.DocumentTypeID, number string, primary bool, validTo time.Time) domain.Identification {
		return domain.Identification{ID: domain.NewIdentificationID(), Type: typ, Country: es, Number: number, Primary: primary, ValidTo: validTo}
	}
	person := func(docs ...domain.Identification) *domain.Party {
		t.Helper()
		name, err := domain.NewPersonalName("Ana", "García", "")
		if err != nil {
			t.Fatal(err)
		}
		p, err := domain.Reconstitute(domain.NewPartyID(), domain.PartyState{Kind: domain.KindPerson, Person: domain.PersonDetails{Name: name},
			Identities: docs, Active: true})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	first := person(doc(domain.DocNationalID, "12345678Z", true, time.Time{}))
	if err := repo.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, person(doc(domain.DocNationalID, "12345678Z", true, time.Time{}))); !violation(err, domain.ErrDocumentTaken) {
		t.Fatalf("the current document of another party: %v", err)
	}
	if _, err := raw.Exec(`UPDATE party_identifications SET valid_to = ? WHERE party_id = ?`, db.Dialect().TimeValue(closedAt),
		db.Dialect().UUIDValue(first.ID().UUID)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, person(doc(domain.DocNationalID, "12345678Z", true, time.Time{}))); err != nil {
		t.Fatalf("the document of a historical identification of another party: %v", err)
	}
	again, err := repo.Get(ctx, first.ID())
	if err != nil {
		t.Fatal(err)
	}
	if ids := again.Identifications(); len(ids) != 1 || ids[0].Current() || !ids[0].ValidTo.Equal(closedAt) {
		t.Fatalf("valid_to read back: %+v", ids)
	}

	if err := repo.Save(ctx, person(doc(domain.DocNationalID, "87654321X", true, time.Time{}), doc(domain.DocPassport, "PAA123456", true, time.Time{}))); !violation(err, domain.ErrDuplicatePrimary) {
		t.Fatalf("two current primaries: %v", err)
	}
	if err := repo.Save(ctx, person(doc(domain.DocNationalID, "87654321X", true, closedAt), doc(domain.DocPassport, "PAA123456", true, time.Time{}))); err != nil {
		t.Fatalf("a historical primary and a current one: %v", err)
	}
	if err := repo.Save(ctx, person(doc(domain.DocNationalID, "11111111H", false, closedAt), doc(domain.DocNationalID, "11111111H", true, time.Time{}))); err != nil {
		t.Fatalf("the same document, historical and current, in one party: %v", err)
	}
}

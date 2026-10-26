package sqlconformance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/application"
	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
)

// RunUniqueIndexes comprueba en db un índice único de una columna anulable ({whereNotNull}):
// admite varias filas sin valor y rechaza dos con el mismo; DB.ViolatesUnique reconoce ese choque
// y no lo confunde con el de la clave primaria ni con el de otro índice. Comprueba también que una
// migración cuyo Check falla no se aplica ni queda sucia, y que se aplica cuando Check pasa.
func RunUniqueIndexes(t *testing.T, db *sqlrepo.DB) {
	ctx := context.Background()
	const table, index = "t_uq", "uq_t_uq_code"
	execDDL(t, db, "DROP TABLE "+table,
		`CREATE TABLE t_uq (id {int} NOT NULL PRIMARY KEY, code {str:20})`,
		`CREATE UNIQUE INDEX uq_t_uq_code ON t_uq (code){whereNotNull:code}`)

	insert := func(id int64, code any) error {
		return db.Insert(ctx, table, sqlrepo.Values{"id": id, "code": code})
	}
	for id := int64(1); id <= 3; id++ {
		if err := insert(id, nil); err != nil {
			t.Fatalf("several rows without a code: %v", err)
		}
	}
	if err := insert(4, "A-1"); err != nil {
		t.Fatal(err)
	}
	err := insert(5, "A-1")
	if err == nil {
		t.Fatal("two rows with the same code")
	}
	if !db.ViolatesUnique(err, table, index, "code") {
		t.Fatalf("the clash with the index not recognised: %v", err)
	}
	if db.ViolatesUnique(err, table, "uq_t_uq_other", "other") {
		t.Fatalf("the clash taken for another index: %v", err)
	}
	if db.ViolatesUnique(err, table, "uq_t_uq", "other") || db.ViolatesUnique(err, table, "t_uq_code", "other") {
		t.Fatalf("the clash taken for an index whose name is part of this one's: %v", err)
	}
	if _, err := db.Update(ctx, table, sqlrepo.Values{"code": "A-1"}, sqlrepo.Values{"id": int64(1)}); !db.ViolatesUnique(err, table, index, "code") {
		t.Fatalf("an update that takes a code already used: %v", err)
	}
	err = insert(4, "B-1")
	if err == nil || !db.Dialect().IsUniqueViolation(err) {
		t.Fatalf("a repeated primary key: %v", err)
	}
	if db.ViolatesUnique(err, table, index, "code") {
		t.Fatalf("a repeated primary key taken for the clash with the index: %v", err)
	}

	uniqueRepository(t, db)

	// Un Check que falla no toca nada: la migración sigue pendiente, sin marca de sucia.
	const history, lock = "t_uq_migrations", "t_uq_migrations_lock"
	for _, s := range []string{"t_uq_mig", history, lock} {
		_, _ = db.ExecContext(ctx, "DROP TABLE "+s)
	}
	ready := errors.New("there are repeated codes")
	mg := sqlrepo.Migration{Version: 1, Name: "create t_uq_mig", Up: sqlrepo.Portable(`CREATE TABLE t_uq_mig (id INT NOT NULL PRIMARY KEY)`),
		Check: func(context.Context, *sqlrepo.DB) error { return ready }}
	migrate := func() error {
		t.Helper()
		m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{{Context: "uq", Migrations: []sqlrepo.Migration{mg}}},
			sqlrepo.WithMigrationTables(history, lock))
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.Migrate(ctx)
		if err == nil {
			return nil
		}
		st, serr := m.Status(ctx)
		if serr != nil || len(st) != 1 || st[0].State != application.MigrationPending {
			t.Fatalf("after a failed check: %+v %v", st, serr)
		}
		return err
	}
	if err := migrate(); !errors.Is(err, ready) {
		t.Fatalf("a failed check: %v", err)
	}
	var n int
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM t_uq_mig").Scan(&n); err == nil {
		t.Fatal("a failed check ran the statements")
	}
	ready = nil
	if err := migrate(); err != nil {
		t.Fatalf("once the check passes: %v", err)
	}
	if err := db.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM t_uq_mig").Scan(&n); err != nil {
		t.Fatalf("the statements did not run: %v", err)
	}
}

// errColorTaken es el error que declara el mapeo de uniqueRepository para su índice único.
var errColorTaken = domain.Violation("widgets.color_taken", "another widget has that color")

// uniqueRepository comprueba Mapping.Unique al insertar: el choque con el índice declarado es su
// error, varias filas sin valor conviven y una identidad repetida sigue siendo domain.Conflict.
func uniqueRepository(t *testing.T, db *sqlrepo.DB) {
	t.Helper()
	ctx := context.Background()
	execDDL(t, db, "DROP TABLE uq_widgets",
		`CREATE TABLE uq_widgets (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, name {str:200} NOT NULL,
	price {bigint} NOT NULL, active {bool} NOT NULL, color {str:50}, created_at {ts} NOT NULL,
	weight {dec:28,8} NOT NULL, launch {date} NOT NULL)`,
		`CREATE UNIQUE INDEX uq_uq_widgets_color ON uq_widgets (color){whereNotNull:color}`)
	m := DecimalWidgetMapping()
	m.Table = "uq_widgets"
	m.Unique = []sqlrepo.UniqueIndex{{Name: "uq_uq_widgets_color", Columns: []string{"color"}, Err: errColorTaken}}
	repo := sqlrepo.MustRepository(db, m)
	created := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	widget := func(id repotest.WidgetID, color *string) *repotest.Widget {
		t.Helper()
		w, err := repotest.Reconstitute(id, "w", 1, true, color, created, vocab.MustDecimal("1"), vocab.DateOf(created), nil)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	red := "red"
	first := repotest.NewWidgetID()
	for _, w := range []*repotest.Widget{widget(first, &red), widget(repotest.NewWidgetID(), nil), widget(repotest.NewWidgetID(), nil)} {
		if err := repo.Save(ctx, w); err != nil {
			t.Fatalf("a red widget and two without a color: %v", err)
		}
	}
	if err := repo.Save(ctx, widget(repotest.NewWidgetID(), &red)); !errors.Is(err, errColorTaken) {
		t.Fatalf("a second red widget: %v", err)
	}
	blue := "blue"
	if err := repo.Save(ctx, widget(first, &blue)); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("a repeated identity: %v", err)
	}
}

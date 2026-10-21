package sqlconformance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/memory"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
)

// RunDecimals comprueba las columnas {dec:p,s} de sqlrepo.RenderDDL en db: ida y vuelta exacta
// (28 cifras, negativos, escala completa, cero y NULL) sin pasar por float64; comparaciones y
// orden numéricos que fallarían comparando texto ("10" frente a "9", "-1" frente a "-10", "2.50"
// frente a "2.5") y que dan lo mismo que el repositorio en memoria; en Oracle, todo ello con coma
// decimal en la sesión; y el paso de una columna de texto a decimal con
// sqlrepo.AlterColumnToDecimal.
func RunDecimals(t *testing.T, db *sqlrepo.DB) {
	t.Run("round trip", func(t *testing.T) { decimalRoundTrip(t, db) })
	t.Run("compare and order", func(t *testing.T) { decimalQueries(t, db, nil) })
	t.Run("write limits", func(t *testing.T) { decimalWriteLimits(t, db) })
	t.Run("oracle decimal comma", func(t *testing.T) {
		if db.Dialect().Name() != "oracle" {
			t.Skip("only Oracle converts text to NUMBER with the session's NLS_NUMERIC_CHARACTERS")
		}
		decimalOracleComma(t, db)
	})
	t.Run("alter text column", func(t *testing.T) { decimalAlter(t, db) })
}

func execDDL(t *testing.T, db *sqlrepo.DB, drop string, ddl ...string) {
	t.Helper()
	ctx := context.Background()
	_, _ = db.ExecContext(ctx, drop)
	for _, s := range ddl {
		s = sqlrepo.RenderDDL(db.Dialect().Name(), s)
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("ddl: %v\n%s", err, s)
		}
	}
}

var roundTrip = []struct {
	amount, fraction any // vocab.Decimal o nil
}{
	{vocab.MustDecimal("12345678901234567890.12345678"), vocab.MustDecimal("0.1234567890123456789012345678")},
	{vocab.MustDecimal("-99999999999999999999.99999999"), vocab.MustDecimal("-0.9999999999999999999999999999")},
	{vocab.MustDecimal("0"), vocab.MustDecimal("0")},
	{vocab.MustDecimal("0.00000001"), vocab.MustDecimal("0.0000000000000000000000000001")},
	{vocab.MustDecimal("-0.00000001"), nil},
	{nil, vocab.MustDecimal("0.5")},
	{nil, nil},
}

func decimalRoundTrip(t *testing.T, db *sqlrepo.DB) {
	ctx := context.Background()
	execDDL(t, db, "DROP TABLE t_dec",
		`CREATE TABLE t_dec (id {int} NOT NULL PRIMARY KEY, amount {dec:28,8}, fraction {dec:28,28})`)
	for i, c := range roundTrip {
		if err := db.Insert(ctx, "t_dec", sqlrepo.Values{"id": int64(i + 1), "amount": c.amount, "fraction": c.fraction}); err != nil {
			t.Fatalf("case %d: insert: %v", i+1, err)
		}
	}
	rows, err := db.Select(ctx, "t_dec", []string{"id", "amount", "fraction"}, "id")
	if err != nil || len(rows) != len(roundTrip) {
		t.Fatalf("select: %d rows, %v", len(rows), err)
	}
	for i, c := range roundTrip {
		for _, col := range []struct {
			name string
			want any
		}{{"amount", c.amount}, {"fraction", c.fraction}} {
			got := rows[i].NullDecimal(col.name)
			switch want := col.want.(type) {
			case nil:
				if got != nil {
					t.Errorf("case %d %s: want NULL, got %s", i+1, col.name, got)
				}
			case vocab.Decimal:
				if got == nil || !got.Equal(want) {
					t.Errorf("case %d %s: want %s, got %v (raw %#v)", i+1, col.name, want, got, rows[i].Raw(col.name))
				}
			}
		}
		if err := rows[i].Err(); err != nil {
			t.Fatalf("case %d: %v", i+1, err)
		}
	}
}

// x30 está entre los dos pesos de 28 cifras y tiene 30 decimales.
const x30 = "12345678901234567890.123456775000000000000000000001"

// Pesos que se ordenan de forma distinta como texto y como número, y dos que sólo se distinguen en
// la cifra 28 (iguales como float64).
var decimalWeights = []string{
	"9", "10", "100", "-1", "-10", "2.5", "2.50000000", "2.49999999", "0", "0.1", "-0.00000001",
	"12345678901234567890.12345678", "12345678901234567890.12345677", "-12345678901234567890.12345678",
}

// DecimalWidgetMapping es WidgetMapping sobre la tabla dec_widgets, cuyo peso es {dec:28,8}, sin
// colecciones hijas.
func DecimalWidgetMapping() sqlrepo.Mapping[repotest.WidgetID, *repotest.Widget] {
	m := WidgetMapping()
	m.Table = "dec_widgets"
	m.Decimals = map[string]sqlrepo.DecimalColumn{"weight": sqlrepo.Dec(28, 8)}
	m.Children = nil
	m.Custom = nil
	return m
}

// decimalWriteLimits comprueba que el repositorio rechaza, sin redondear, un valor que no cabe en
// la columna, y que una columna decimal sólo admite vocab.Decimal.
func decimalWriteLimits(t *testing.T, db *sqlrepo.DB) {
	execDDL(t, db, "DROP TABLE dec_widgets", decWidgetsDDL)
	ctx := context.Background()
	created := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	widget := func(weight string) *repotest.Widget {
		w, err := repotest.Reconstitute(repotest.NewWidgetID(), "w", 1, true, nil, created, vocab.MustDecimal(weight), vocab.DateOf(created), nil)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	repo := sqlrepo.MustRepository(db, DecimalWidgetMapping())
	for _, w := range []string{"0.000000001", "123456789012345678901", "-123456789012345678901.5"} {
		// Un fallo del programa, no un dato del usuario: no es ErrValidation (no sale como 400).
		if err := repo.Save(ctx, widget(w)); !errors.Is(err, sqlrepo.ErrDecimalDoesNotFit) || errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s does not fit {dec:28,8}: err = %v, want ErrDecimalDoesNotFit and not ErrValidation", w, err)
		}
	}
	if err := repo.Save(ctx, widget("99999999999999999999.99999999")); err != nil {
		t.Errorf("the largest value must fit: %v", err)
	}

	asText := DecimalWidgetMapping()
	dehydrate := asText.Dehydrate
	asText.Dehydrate = func(w *repotest.Widget) (sqlrepo.Values, error) {
		v, err := dehydrate(w)
		v["weight"] = w.Weight().String()
		return v, err
	}
	if err := sqlrepo.MustRepository(db, asText).Save(ctx, widget("1.5")); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("text for a decimal column: err = %v, want ErrUnsupported", err)
	}
	undeclared := DecimalWidgetMapping()
	undeclared.Decimals = nil
	if err := sqlrepo.MustRepository(db, undeclared).Save(ctx, widget("1.5")); !errors.Is(err, domain.ErrUnsupported) {
		t.Errorf("a decimal for an undeclared column: err = %v, want ErrUnsupported", err)
	}
	// Un *vocab.Decimal nil en Eq es IS NULL: el peso nunca es nulo, no hay ninguno.
	isNull := spec.New(spec.Compare{Field: "weight", Op: spec.OpEq, Value: (*vocab.Decimal)(nil)},
		func(*repotest.Widget) bool { return false })
	if got, err := repo.Find(ctx, isNull); err != nil || len(got) != 0 {
		t.Errorf("Eq(nil): %d widgets, %v", len(got), err)
	}
}

const decWidgetsDDL = `CREATE TABLE dec_widgets (id {uuid} NOT NULL PRIMARY KEY, version {bigint} NOT NULL, name {str:200} NOT NULL,
	price {bigint} NOT NULL, active {bool} NOT NULL, color {str:50}, created_at {ts} NOT NULL,
	weight {dec:28,8} NOT NULL, launch {date} NOT NULL)`

// decimalQueries guarda los pesos en SQL y en memoria y exige los mismos resultados en ambos.
// Todo ocurre en una unidad de trabajo que prepare, si se da, ajusta antes (la sesión de Oracle
// con coma decimal); la función que devuelve deshace el ajuste al final.
func decimalQueries(t *testing.T, db *sqlrepo.DB, prepare func(ctx context.Context) (func(), error)) {
	execDDL(t, db, "DROP TABLE dec_widgets", decWidgetsDDL)
	repo, err := sqlrepo.NewRepository(db, DecimalWidgetMapping())
	if err != nil {
		t.Fatal(err)
	}
	mem := memory.NewRepository[repotest.WidgetID, *repotest.Widget](memory.NewStore("decimals"),
		memory.WithClone((*repotest.Widget).Clone))

	created := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	widget := func(id repotest.WidgetID, i int, w string) *repotest.Widget {
		x, err := repotest.Reconstitute(id, fmt.Sprintf("w%02d", i), int64(i), true, nil, created,
			vocab.MustDecimal(w), vocab.DateOf(created), nil)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}

	d := vocab.MustDecimal
	in := func(vs ...string) spec.Spec[*repotest.Widget] {
		vals := make([]any, len(vs))
		for i, v := range vs {
			vals[i] = d(v)
		}
		return spec.New(spec.Compare{Field: "weight", Op: spec.OpIn, Value: vals}, func(w *repotest.Widget) bool {
			return slices.ContainsFunc(vals, func(v any) bool { return w.Weight().Equal(v.(vocab.Decimal)) })
		})
	}
	w := repotest.FieldWeight
	specs := []struct {
		name string
		s    spec.Specification[*repotest.Widget]
	}{
		{"gt 9 (text: 10 < 9)", w.Gt(d("9"))},
		{"lt -1 (text: -10 > -1)", w.Lt(d("-1"))},
		{"eq 2.5 (text: 2.50 != 2.5)", w.Eq(d("2.5"))},
		{"ne 2.5", w.Ne(d("2.5"))},
		{"ge 28 digits (float: equal)", w.Ge(d("12345678901234567890.12345678"))},
		{"gt between the last digits", w.Gt(d("12345678901234567890.123456775"))},
		{"le -0.00000001", w.Le(d("-0.00000001"))},
		{"between 0 and 2.5", w.Between(d("0"), d("2.5"))},
		{"eq 0", w.Eq(d("0.000"))},
		{"in", in("10", "2.5", "-12345678901234567890.12345678", "7")},
		{"not in", spec.Not[*repotest.Widget](in("10", "2.5"))},
		// 30 decimales frente a una columna de 20 cifras enteras y 8 decimales: 50 cifras, más de
		// las que caben en DECIMAL(38); se reescriben sobre la escala de la columna.
		{"gt 30 decimals", w.Gt(d(x30))},
		{"ge 30 decimals", w.Ge(d(x30))},
		{"lt 30 decimals", w.Lt(d(x30))},
		{"le 30 decimals", w.Le(d(x30))},
		{"eq 30 decimals", w.Eq(d(x30))},
		{"ne 30 decimals", w.Ne(d(x30))},
		{"gt negative 30 decimals", w.Gt(d("-" + x30))},
		{"le negative 30 decimals", w.Le(d("-" + x30))},
		{"ge 2.49999999 plus 30 decimals", w.Ge(d("2.499999990000000000000000000001"))},
		{"in with 30 decimals", in(x30, "2.5")},
	}

	ctx := context.Background()
	err = db.Do(ctx, func(ctx context.Context) error {
		if prepare != nil {
			undo, err := prepare(ctx)
			if undo != nil {
				defer undo()
			}
			if err != nil {
				return err
			}
		}
		for i, weight := range decimalWeights {
			id := repotest.NewWidgetID()
			if err := repo.Save(ctx, widget(id, i, weight)); err != nil {
				return fmt.Errorf("save %s: %w", weight, err)
			}
			if err := mem.Save(ctx, widget(id, i, weight)); err != nil {
				return err
			}
		}
		for _, c := range specs {
			got, err := repo.Find(ctx, c.s)
			if err != nil {
				return fmt.Errorf("%s: %w", c.name, err)
			}
			want, err := mem.Find(ctx, c.s)
			if err != nil {
				return err
			}
			if g, w := names(got), names(want); !slices.Equal(g, w) {
				t.Errorf("%s: SQL %v, memory %v", c.name, g, w)
			}
		}
		for _, order := range []spec.Order[*repotest.Widget]{w.Asc(), w.Desc()} {
			got, err := repo.Find(ctx, nil, order)
			if err != nil {
				return err
			}
			want, err := mem.Find(ctx, nil, order)
			if err != nil {
				return err
			}
			// Se comparan los pesos, no los widgets: los empates (2.5 y 2.50) se deshacen por
			// identidad, que no se ordena igual en todos los motores.
			if g, w := weights(got), weights(want); !slices.Equal(g, w) {
				t.Errorf("order desc=%v: SQL %v, memory %v", order.Desc, g, w)
			}
		}
		for _, x := range must(repo.Find(ctx, nil)) {
			if !slices.ContainsFunc(decimalWeights, func(s string) bool { return d(s).Equal(x.Weight()) }) {
				t.Errorf("weight %s does not round-trip", x.Weight())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func names(ws []*repotest.Widget) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Name()
	}
	slices.Sort(out)
	return out
}

func weights(ws []*repotest.Widget) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Weight().String()
	}
	return out
}

// decimalOracleComma repite la ida y vuelta y las consultas con la sesión de Oracle en coma
// decimal (NLS_NUMERIC_CHARACTERS = ',.'), donde la conversión implícita de "12.5" a NUMBER
// fallaría con ORA-01722. La sesión se ajusta dentro de la unidad de trabajo, que fija la conexión,
// y se restaura al terminar.
func decimalOracleComma(t *testing.T, db *sqlrepo.DB) {
	comma := func(ctx context.Context) error {
		if _, err := db.ExecContext(ctx, "ALTER SESSION SET NLS_NUMERIC_CHARACTERS = ',.'"); err != nil {
			return err
		}
		rows, err := db.QueryContext(ctx, "SELECT TO_CHAR(1.5) FROM DUAL")
		if err != nil {
			return err
		}
		defer rows.Close()
		var s string
		if rows.Next() {
			if err := rows.Scan(&s); err != nil {
				return err
			}
		}
		if s != "1,5" {
			return fmt.Errorf("the session did not switch to a decimal comma: TO_CHAR(1.5) = %q", s)
		}
		return rows.Err()
	}
	execDDL(t, db, "DROP TABLE t_dec",
		`CREATE TABLE t_dec (id {int} NOT NULL PRIMARY KEY, amount {dec:28,8}, fraction {dec:28,28})`)

	decimalQueries(t, db, func(ctx context.Context) (func(), error) {
		if err := comma(ctx); err != nil {
			return nil, err
		}
		// La conexión de la unidad de trabajo vuelve al pool: se deja como estaba.
		restore := func() { _, _ = db.ExecContext(ctx, "ALTER SESSION SET NLS_NUMERIC_CHARACTERS = '.,'") }

		// El problema existe: sin formato ni NLS explícitos, el texto con punto no se convierte.
		var implicit any
		if rows, err := db.QueryContext(ctx, "SELECT CAST(:1 AS NUMBER) FROM DUAL", "12.5"); err == nil {
			for rows.Next() {
				_ = rows.Scan(&implicit)
			}
			if rows.Err() == nil && implicit != nil {
				t.Logf("this Oracle converted '12.5' implicitly with a decimal comma: %v", implicit)
			}
			_ = rows.Close()
		}

		ins := sqlrepo.Values{"id": int64(1), "amount": vocab.MustDecimal("-12345678901234567890.12345678"),
			"fraction": vocab.MustDecimal("0.1234567890123456789012345678")}
		if err := db.Insert(ctx, "t_dec", ins); err != nil {
			return restore, fmt.Errorf("insert with a decimal comma: %w", err)
		}
		rows, err := db.Select(ctx, "t_dec", []string{"amount", "fraction"})
		if err != nil {
			return restore, err
		}
		if len(rows) != 1 || !rows[0].Decimal("amount").Equal(ins["amount"].(vocab.Decimal)) ||
			!rows[0].Decimal("fraction").Equal(ins["fraction"].(vocab.Decimal)) || rows[0].Err() != nil {
			return restore, fmt.Errorf("decimal comma round trip: %d rows, %v", len(rows), rows[0].Err())
		}
		return restore, nil
	})
}

// alterRun ejecuta el Run de AlterColumnToDecimal en una unidad de trabajo, como el migrador.
func alterRun(db *sqlrepo.DB, column string, notNull bool) error {
	run, err := sqlrepo.AlterColumnToDecimal("t_dec_alter", column, 18, 4, notNull)
	if err != nil {
		return err
	}
	return db.Do(context.Background(), func(ctx context.Context) error { return run(ctx, db) })
}

// decimalAlter comprueba sqlrepo.AlterColumnToDecimal: con un valor que no es un decimal con punto
// o que no cabe en {dec:18,4} la migración falla y los datos quedan como estaban; con valores
// válidos (también ".5", "-.5", "002.5" y NULL) los conserva, en una columna NOT NULL y en otra que
// admite nulos, se puede repetir, y la columna se ordena por valor (en SQLite, que la deja en
// TEXT, el texto queda en forma canónica).
func decimalAlter(t *testing.T, db *sqlrepo.DB) {
	ctx := context.Background()
	dialect := db.Dialect().Name()
	create := `CREATE TABLE t_dec_alter (id {int} NOT NULL PRIMARY KEY, amount {str:40} NOT NULL, opt {str:40})`

	bad := []string{"abc", " 1", "1 ", "+1", "1e3", "NaN", "1.", "-", ".", "1,5", "123456789012345", "0.12345", "-999999999999999.5"}
	if dialect != "oracle" { // en Oracle, "" es NULL
		bad = append(bad, "")
	}
	for _, b := range bad {
		execDDL(t, db, "DROP TABLE t_dec_alter", create)
		for i, v := range []string{"1.5", b} {
			if err := db.Insert(ctx, "t_dec_alter", sqlrepo.Values{"id": int64(i + 1), "amount": "7", "opt": v}); err != nil {
				t.Fatal(err)
			}
		}
		if err := alterRun(db, "opt", false); err == nil {
			t.Errorf("%q: the change must fail", b)
			continue
		}
		rows, err := db.Select(ctx, "t_dec_alter", []string{"id", "opt"}, "id")
		if err != nil || len(rows) != 2 || rows[0].String("opt") != "1.5" || rows[1].String("opt") != b {
			t.Errorf("%q: the data changed after a failed change: %d rows, %v", b, len(rows), err)
		}
	}

	execDDL(t, db, "DROP TABLE t_dec_alter", create)
	type row struct {
		amount string
		opt    any // string o nil
	}
	data := []row{{"10", nil}, {"9", ".5"}, {"-1", "-.5"}, {"-10", nil}, {"2.50", "002.5"}, {"0.125", "-0.0001"},
		{".5", nil}, {"-.5", "0"}, {"002.5", nil}, {"12345678901234.1234", "-12345678901234.1234"}}
	for i, r := range data {
		if err := db.Insert(ctx, "t_dec_alter", sqlrepo.Values{"id": int64(i + 1), "amount": r.amount, "opt": r.opt}); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 { // la segunda vez no cambia nada
		if err := alterRun(db, "amount", true); err != nil {
			t.Fatal(err)
		}
		if err := alterRun(db, "opt", false); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Select(ctx, "t_dec_alter", []string{"id", "amount", "opt"}, "id")
	if err != nil || len(rows) != len(data) {
		t.Fatalf("select: %d rows, %v", len(rows), err)
	}
	for i, r := range data {
		if got := rows[i].Decimal("amount"); !got.Equal(vocab.MustDecimal(r.amount)) {
			t.Errorf("%s became %s", r.amount, got)
		}
		got := rows[i].NullDecimal("opt")
		switch want := r.opt.(type) {
		case nil:
			if got != nil {
				t.Errorf("NULL became %s", got)
			}
		case string:
			if got == nil || !got.Equal(vocab.MustDecimal(want)) {
				t.Errorf("%s became %v", want, got)
			}
		}
		if dialect == "sqlite" {
			if raw := rows[i].String("amount"); raw != vocab.MustDecimal(r.amount).String() {
				t.Errorf("sqlite keeps %q, want the canonical %q", raw, vocab.MustDecimal(r.amount).String())
			}
		}
		if err := rows[i].Err(); err != nil {
			t.Fatal(err)
		}
	}
	if dialect == "sqlite" {
		return
	}
	ordered, err := db.Select(ctx, "t_dec_alter", []string{"amount"}, "amount", "id")
	if err != nil {
		t.Fatal(err)
	}
	want := make([]string, len(data))
	for i, r := range data {
		want[i] = r.amount
	}
	slices.SortStableFunc(want, func(a, b string) int { return vocab.MustDecimal(a).Cmp(vocab.MustDecimal(b)) })
	for i, s := range want {
		if got := ordered[i].Decimal("amount"); !got.Equal(vocab.MustDecimal(s)) {
			t.Fatalf("order after the change: position %d is %s, want %s", i, got, s)
		}
	}
	// La columna NOT NULL lo sigue siendo y la otra admite nulos; un decimal nuevo se escribe ya
	// en la columna numérica.
	if err := db.Insert(ctx, "t_dec_alter", sqlrepo.Values{"id": int64(100), "amount": nil, "opt": nil}); err == nil {
		t.Error("amount must still be NOT NULL")
	}
	if err := db.Insert(ctx, "t_dec_alter", sqlrepo.Values{"id": int64(101), "amount": vocab.MustDecimal("-3.1416"), "opt": nil}); err != nil {
		t.Fatal(err)
	}
}

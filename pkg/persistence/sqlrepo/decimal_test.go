package sqlrepo_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlconformance"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
	rt "github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
)

// TestExplain_Decimal fija cómo se compara y se ordena una columna decimal en cada motor: el valor
// viaja siempre como texto con punto (nunca float64) y se convierte a número en SQL, salvo en
// SQLite, que compara claves de orden numérico.
func TestExplain_Decimal(t *testing.T) {
	s := rt.FieldWeight.Gt(vocab.MustDecimal("-12.50"))
	page := domain.NewPageRequest(1, 10, rt.FieldWeight.Desc())
	cases := []struct {
		d       sqlrepo.Dialect
		where   string
		order   string
		wantArg string
	}{
		{postgres.New(), `t0."weight" > $1`, `ORDER BY t0."weight" DESC`, "-12.5"},
		{sqlserver.New(), `t0.[weight] > CAST(@p1 AS DECIMAL(3,1))`, `ORDER BY t0.[weight] DESC`, "-12.5"},
		{oracle.New(), `t0."WEIGHT" > TO_NUMBER(:1, '99D9', 'NLS_NUMERIC_CHARACTERS=''.,''')`, `ORDER BY t0."WEIGHT" DESC`, "-12.5"},
		{mysql.New(), "t0.`weight` > CAST(? AS DECIMAL(3,1))", "ORDER BY t0.`weight` DESC", "-12.5"},
		{sqlite.New(), `END) > ?`, `END) DESC`, sqlite.New().DecimalKey(vocab.MustDecimal("-12.5"))},
	}
	for _, tc := range cases {
		t.Run(tc.d.Name(), func(t *testing.T) {
			got, args, err := explain(t, tc.d, s, page)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, tc.where) || !strings.Contains(got, tc.order) {
				t.Fatalf("want %q and %q in\n%s", tc.where, tc.order, got)
			}
			if len(args) != 1 || args[0] != tc.wantArg {
				t.Fatalf("args %#v, want the text %q", args, tc.wantArg)
			}
		})
	}
}

func TestExplain_DecimalInAndShapes(t *testing.T) {
	s := spec.New[*rt.Widget](spec.Compare{Field: "weight", Op: spec.OpIn,
		Value: []any{vocab.MustDecimal("0.005"), vocab.MustDecimal("1200")}}, nil)
	got, args, err := explain(t, oracle.New(), s, domain.PageRequest[*rt.Widget]{})
	if err != nil {
		t.Fatal(err)
	}
	for _, frag := range []string{`TO_NUMBER(:1, '9D999'`, `TO_NUMBER(:2, '9999'`} {
		if !strings.Contains(got, frag) {
			t.Fatalf("want %q in\n%s", frag, got)
		}
	}
	if args[0] != "0.005" || args[1] != "1200" {
		t.Fatalf("args %#v", args)
	}
}

// TestExplain_DecimalBeyondScale fija la reescritura de una comparación con más decimales que la
// columna ({dec:18,4}): se compara con el valor redondeado hacia abajo a 4 decimales.
func TestExplain_DecimalBeyondScale(t *testing.T) {
	x := vocab.MustDecimal("2.123456")
	cases := []struct {
		s     spec.Specification[*rt.Widget]
		where string
		arg   any
	}{
		{rt.FieldWeight.Gt(x), `t0."weight" > $1`, "2.1234"},
		{rt.FieldWeight.Ge(x), `t0."weight" > $1`, "2.1234"},
		{rt.FieldWeight.Lt(x), `t0."weight" <= $1`, "2.1234"},
		{rt.FieldWeight.Le(x), `t0."weight" <= $1`, "2.1234"},
		{rt.FieldWeight.Eq(x), `WHERE 1=0`, nil},
		{rt.FieldWeight.Ne(x), `WHERE t0."weight" IS NOT NULL`, nil},
		{rt.FieldWeight.Gt(vocab.MustDecimal("-2.123456")), `t0."weight" > $1`, "-2.1235"},
		{spec.New[*rt.Widget](spec.Compare{Field: "weight", Op: spec.OpEq, Value: (*vocab.Decimal)(nil)}, nil), `WHERE t0."weight" IS NULL`, nil},
	}
	for _, c := range cases {
		got, args, err := explain(t, postgres.New(), c.s, domain.PageRequest[*rt.Widget]{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, c.where) {
			t.Errorf("want %q in\n%s", c.where, got)
		}
		if c.arg == nil && len(args) != 0 || c.arg != nil && (len(args) != 1 || args[0] != c.arg) {
			t.Errorf("%s: args %#v, want %v", c.where, args, c.arg)
		}
	}
}

func TestExplain_DecimalErrors(t *testing.T) {
	cases := map[string]spec.Specification[*rt.Widget]{
		// Un decimal comparado con una columna que no está en Mapping.Decimals.
		"undeclared column": spec.OrderedBy[*rt.Widget, vocab.Decimal]("price", nil, vocab.CompareDecimal).Gt(vocab.MustDecimal("1")),
		// Una columna decimal comparada con algo que no es un decimal.
		"not a decimal": spec.New[*rt.Widget](spec.Compare{Field: "weight", Op: spec.OpGt, Value: "1"}, nil),
		// Más cifras enteras de las que caben en DECIMAL(38) / NUMBER (los decimales de más se
		// reescriben sobre la escala de la columna; las cifras enteras, no).
		"too many digits": rt.FieldWeight.Gt(vocab.MustDecimal(strings.Repeat("9", 39))),
		// Un NULL en una comparación de orden.
		"null gt": spec.New[*rt.Widget](spec.Compare{Field: "weight", Op: spec.OpGt, Value: (*vocab.Decimal)(nil)}, nil),
		// Operadores de texto sobre una columna decimal.
		"text operator": spec.New[*rt.Widget](spec.Compare{Field: "weight", Op: spec.OpContains, Value: "1"}, nil),
	}
	for name, s := range cases {
		for _, d := range []sqlrepo.Dialect{sqlite.New(), postgres.New(), oracle.New()} {
			if _, _, err := explain(t, d, s, domain.PageRequest[*rt.Widget]{}); !errors.Is(err, domain.ErrUnsupported) {
				t.Errorf("%s on %s: err = %v, want ErrUnsupported", name, d.Name(), err)
			}
		}
	}
}

func TestMapping_DecimalsMustBeColumns(t *testing.T) {
	m := sqlconformance.WidgetMapping()
	m.Decimals = map[string]sqlrepo.DecimalColumn{"nope": sqlrepo.Dec(18, 2)}
	if _, err := sqlrepo.NewRepository(sqlrepo.New(nil, sqlite.New()), m); err == nil {
		t.Fatal("a decimal column outside Columns was accepted")
	}
	m.Decimals = map[string]sqlrepo.DecimalColumn{"weight": sqlrepo.Dec(29, 2)}
	if _, err := sqlrepo.NewRepository(sqlrepo.New(nil, sqlite.New()), m); err == nil {
		t.Fatal("a decimal column with precision 29 was accepted")
	}
}

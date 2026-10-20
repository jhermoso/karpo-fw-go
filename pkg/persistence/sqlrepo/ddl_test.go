package sqlrepo_test

import (
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

func TestRenderDDL_Decimal(t *testing.T) {
	ddl := `CREATE TABLE t (id {int} NOT NULL PRIMARY KEY, amount {dec:28,8} NOT NULL, rate {dec: 5, 4}, name {str:10})`
	want := map[string]string{
		"sqlite":    `CREATE TABLE t (id INTEGER NOT NULL PRIMARY KEY, amount TEXT NOT NULL, rate TEXT, name TEXT)`,
		"postgres":  `CREATE TABLE t (id INTEGER NOT NULL PRIMARY KEY, amount NUMERIC(28,8) NOT NULL, rate NUMERIC(5,4), name VARCHAR(10))`,
		"sqlserver": `CREATE TABLE t (id INT NOT NULL PRIMARY KEY, amount DECIMAL(28,8) NOT NULL, rate DECIMAL(5,4), name NVARCHAR(10))`,
		"oracle":    `CREATE TABLE t (id NUMBER(10) NOT NULL PRIMARY KEY, amount NUMBER(28,8) NOT NULL, rate NUMBER(5,4), name VARCHAR2(10 CHAR))`,
		"mysql":     `CREATE TABLE t (id INT NOT NULL PRIMARY KEY, amount DECIMAL(28,8) NOT NULL, rate DECIMAL(5,4), name VARCHAR(10))`,
	}
	for _, d := range sqlrepo.Dialects {
		got, err := sqlrepo.RenderDDLChecked(d, ddl)
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		if got != want[d] {
			t.Errorf("%s:\nwant %s\ngot  %s", d, want[d], got)
		}
		if again := sqlrepo.RenderDDL(d, ddl); again != got {
			t.Errorf("%s: RenderDDL and RenderDDLChecked differ", d)
		}
	}
	all := sqlrepo.RenderDDLAll(ddl)
	if all["postgres"][0] != want["postgres"] {
		t.Fatalf("RenderDDLAll: %v", all["postgres"])
	}
}

func TestRenderDDL_DecimalLimits(t *testing.T) {
	for _, ok := range []string{"{dec:1,0}", "{dec:1,1}", "{dec:28,0}", "{dec:28,28}"} {
		if _, err := sqlrepo.RenderDDLChecked("postgres", ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"{dec}", "{dec:18}", "{dec:0,0}", "{dec:29,2}", "{dec:5,6}", "{dec:5,-1}", "{dec:a,b}", "{dec:1,2,3}"} {
		for _, d := range sqlrepo.Dialects {
			if _, err := sqlrepo.RenderDDLChecked(d, "CREATE TABLE t (x "+bad+")"); err == nil {
				t.Errorf("%s %s: malformed marker accepted", d, bad)
			}
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("RenderDDL(%s) did not panic", bad)
				}
			}()
			sqlrepo.RenderDDL("sqlite", bad)
		}()
	}
	// Los marcadores desconocidos se dejan como están, igual que antes.
	if got := sqlrepo.RenderDDL("postgres", "{other:1,2}"); got != "{other:1,2}" {
		t.Fatalf("unknown marker rewritten: %s", got)
	}
}

func TestAlterColumnToDecimal_Arguments(t *testing.T) {
	if _, err := sqlrepo.AlterColumnToDecimal("lines", "amount", 18, 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlrepo.AlterColumnToDecimal("lines", "amount", 29, 2, true); err == nil {
		t.Error("precision 29 accepted")
	}
	if _, err := sqlrepo.AlterColumnToDecimal("lines; DROP", "amount", 18, 2, true); err == nil {
		t.Error("unsafe identifier accepted")
	}
}

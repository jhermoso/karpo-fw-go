package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

var keyTexts = []string{
	"0", "-0", "0.000", "-0.00", "1", "-1", "9", "10", "-10", "-9", "2.5", "2.50", "002.5", ".5", "-.5",
	"0.5", "0.05", "-0.05", "0.0000000000000000000000000001", "-0.0000000000000000000000000001",
	"9999999999999999999999999999", "-9999999999999999999999999999", "1234567890123456789.012345678",
	"1234567890123456789.012345677", "-1234567890123456789.012345678", "-1234567890123456789.012345677",
	"100", "99.999", "-99.999", "-100", "1.5", "1.54", "-1.5", "-1.54",
}

// TestDecimalKey_OrderIsNumeric comprueba que el orden de las claves es el numérico, para todos
// los pares (incluidos los que como texto o como float64 se ordenan mal).
func TestDecimalKey_OrderIsNumeric(t *testing.T) {
	d := sqlite.New()
	for _, a := range keyTexts {
		for _, b := range keyTexts {
			da, db := vocab.MustDecimal(a), vocab.MustDecimal(b)
			if got, want := strings.Compare(d.DecimalKey(da), d.DecimalKey(db)), da.Cmp(db); got != want {
				t.Errorf("%s vs %s: key order %d, numeric order %d (%q, %q)", a, b, got, want, d.DecimalKey(da), d.DecimalKey(db))
			}
		}
	}
}

// TestDecimalKey_SQLMatchesGo comprueba que la clave que calcula SQLite sobre el texto guardado
// (también el no canónico: "2.50", "002.5", ".5", "-0") es la misma que calcula Go sobre el valor.
func TestDecimalKey_SQLMatchesGo(t *testing.T) {
	db := openTemp(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE k (v TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, s := range keyTexts {
		if _, err := db.ExecContext(ctx, `INSERT INTO k (v) VALUES (?)`, s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO k (v) VALUES (NULL)`); err != nil {
		t.Fatal(err)
	}
	d := sqlite.New()
	rows, err := db.QueryContext(ctx, `SELECT v, `+d.DecimalKeyExpr(`v`)+` FROM k`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var v, key *string
		if err := rows.Scan(&v, &key); err != nil {
			t.Fatal(err)
		}
		n++
		if v == nil {
			if key != nil {
				t.Errorf("NULL has key %q", *key)
			}
			continue
		}
		if want := d.DecimalKey(vocab.MustDecimal(*v)); key == nil || *key != want {
			t.Errorf("%s: SQL key %v, Go key %q", *v, key, want)
		}
	}
	if err := rows.Err(); err != nil || n != len(keyTexts)+1 {
		t.Fatalf("%d rows, %v", n, err)
	}
}

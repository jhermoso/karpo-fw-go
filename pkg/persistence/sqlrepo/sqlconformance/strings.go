package sqlconformance

import (
	"context"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
)

// RunStrings comprueba que una columna {str:N} de sqlrepo.RenderDDL admite N caracteres en db,
// ocupen los bytes que ocupen: el dominio valida las longitudes en runas, así que un texto de N
// caracteres acentuados debe caber (el VARCHAR2(N) de Oracle cuenta bytes salvo que se declare en
// CHAR).
func RunStrings(t *testing.T, db *sqlrepo.DB) {
	ctx := context.Background()
	dialect := db.Dialect().Name()
	_, _ = db.ExecContext(ctx, "DROP TABLE t_str")
	ddl := sqlrepo.RenderDDL(dialect, `CREATE TABLE t_str (id {int} NOT NULL PRIMARY KEY, short {str:10}, body {str:1000})`)
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		t.Fatalf("ddl: %v\n%s", err, ddl)
	}

	cases := []struct{ short, body string }{
		{"áéíóúñüç€ß", strings.Repeat("ñ", 1000)},            // 2 bytes cada uno en UTF-8
		{strings.Repeat("€", 10), strings.Repeat("€", 1000)}, // 3 bytes cada uno
		{strings.Repeat("語", 10), strings.Repeat("añ€語", 250)},
	}
	// Fuera del plano multilingüe básico (4 bytes en UTF-8): 1.000 de ellos son justo los 4.000
	// bytes de un VARCHAR2 de Oracle. El NVARCHAR de SQL Server los cuenta como dos unidades de
	// código.
	if dialect != "sqlserver" {
		cases = append(cases, struct{ short, body string }{strings.Repeat("😀", 10), strings.Repeat("😀", 1000)})
	}
	for i, c := range cases {
		if err := db.Insert(ctx, "t_str", sqlrepo.Values{"id": int64(i + 1), "short": c.short, "body": c.body}); err != nil {
			t.Fatalf("case %d: %d characters (%d bytes) must fit in {str:1000}: %v", i+1, len([]rune(c.body)), len(c.body), err)
		}
	}
	rows, err := db.Select(ctx, "t_str", []string{"id", "short", "body"}, "id")
	if err != nil || len(rows) != len(cases) {
		t.Fatalf("select: %d rows, %v", len(rows), err)
	}
	for i, c := range cases {
		if rows[i].String("short") != c.short || rows[i].String("body") != c.body {
			t.Fatalf("case %d does not round-trip: short %q, body of %d bytes (want %d)",
				i+1, rows[i].String("short"), len(rows[i].String("body")), len(c.body))
		}
	}
}

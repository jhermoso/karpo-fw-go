package sqlrepo_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/spec"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/mysql"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/oracle"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlconformance"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlserver"
	rt "github.com/jhermoso/karpo-fw-go/pkg/testing/repotest"
)

func explain(t *testing.T, d sqlrepo.Dialect, s spec.Specification[*rt.Widget], page domain.PageRequest[*rt.Widget]) (string, []any, error) {
	t.Helper()
	repo := sqlrepo.MustRepository(sqlrepo.New(nil, d), sqlconformance.WidgetMapping())
	return repo.Explain(s, page)
}

// TestExplain_Dialects freezes the SQL generated for a specification that exercises boolean
// conversion, case-insensitive LIKE with escaping, EXISTS on a child collection, a custom
// specification, NOT IN and paging: filtering, ordering and paging all happen in the database.
func TestExplain_Dialects(t *testing.T) {
	s := rt.FieldActive.Eq(true).And(
		rt.FieldName.ContainsFold("a%b"),
		rt.FieldParts.Any(rt.PartQty.Gt(5)),
		rt.Premium(10),
		rt.FieldColor.In("red", "blue").Not(),
	)
	page := domain.NewPageRequest(3, 10, rt.FieldPrice.Desc())

	cases := []struct {
		d    sqlrepo.Dialect
		want string
	}{
		{sqlite.New(), `SELECT t0."id", t0."version", t0."name", t0."price", t0."active", t0."color", t0."created_at" FROM "widgets" t0 WHERE (t0."active" = ?) AND (LOWER(t0."name") LIKE LOWER(?) ESCAPE '\') AND (EXISTS (SELECT 1 FROM "widget_parts" t1 WHERE t1."widget_id" = t0."id" AND (t1."qty" > ?))) AND ((t0."active" = ? AND t0."price" >= ?)) AND (NOT (t0."color" IN (?, ?))) ORDER BY t0."price" DESC, t0."id" ASC LIMIT 10 OFFSET 20`},
		{postgres.New(), `SELECT t0."id", t0."version", t0."name", t0."price", t0."active", t0."color", t0."created_at" FROM "widgets" t0 WHERE (t0."active" = $1) AND (LOWER(t0."name") LIKE LOWER($2) ESCAPE '\') AND (EXISTS (SELECT 1 FROM "widget_parts" t1 WHERE t1."widget_id" = t0."id" AND (t1."qty" > $3))) AND ((t0."active" = $4 AND t0."price" >= $5)) AND (NOT (t0."color" IN ($6, $7))) ORDER BY t0."price" DESC, t0."id" ASC LIMIT 10 OFFSET 20`},
		{sqlserver.New(), `SELECT t0.[id], t0.[version], t0.[name], t0.[price], t0.[active], t0.[color], t0.[created_at] FROM [widgets] t0 WHERE (t0.[active] = @p1) AND (LOWER(t0.[name]) LIKE LOWER(@p2) ESCAPE '\') AND (EXISTS (SELECT 1 FROM [widget_parts] t1 WHERE t1.[widget_id] = t0.[id] AND (t1.[qty] > @p3))) AND ((t0.[active] = @p4 AND t0.[price] >= @p5)) AND (NOT (t0.[color] IN (@p6, @p7))) ORDER BY t0.[price] DESC, t0.[id] ASC OFFSET 20 ROWS FETCH NEXT 10 ROWS ONLY`},
		{oracle.New(), `SELECT t0."ID", t0."VERSION", t0."NAME", t0."PRICE", t0."ACTIVE", t0."COLOR", t0."CREATED_AT" FROM "WIDGETS" t0 WHERE (t0."ACTIVE" = :1) AND (LOWER(t0."NAME") LIKE LOWER(:2) ESCAPE '\') AND (EXISTS (SELECT 1 FROM "WIDGET_PARTS" t1 WHERE t1."WIDGET_ID" = t0."ID" AND (t1."QTY" > :3))) AND ((t0."ACTIVE" = :4 AND t0."PRICE" >= :5)) AND (NOT (t0."COLOR" IN (:6, :7))) ORDER BY t0."PRICE" DESC, t0."ID" ASC OFFSET 20 ROWS FETCH NEXT 10 ROWS ONLY`},
		{mysql.New(), "SELECT t0.`id`, t0.`version`, t0.`name`, t0.`price`, t0.`active`, t0.`color`, t0.`created_at` FROM `widgets` t0 WHERE (t0.`active` = ?) AND (LOWER(t0.`name`) LIKE LOWER(?) ESCAPE '\\\\') AND (EXISTS (SELECT 1 FROM `widget_parts` t1 WHERE t1.`widget_id` = t0.`id` AND (t1.`qty` > ?))) AND ((t0.`active` = ? AND t0.`price` >= ?)) AND (NOT (t0.`color` IN (?, ?))) ORDER BY t0.`price` DESC, t0.`id` ASC LIMIT 10 OFFSET 20"},
	}
	for _, tc := range cases {
		t.Run(tc.d.Name(), func(t *testing.T) {
			got, args, err := explain(t, tc.d, s, page)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("SQL mismatch\nwant %s\ngot  %s", tc.want, got)
			}
			if len(args) != 7 || args[1] != `%a\%b%` {
				t.Fatalf("unexpected args %#v", args)
			}
		})
	}
}

func TestExplain_BooleanAndUUIDRepresentation(t *testing.T) {
	id := rt.NewWidgetID()
	s := rt.FieldID.Eq(id).And(rt.FieldActive.Eq(true))
	check := func(d sqlrepo.Dialect, wantID, wantBool any) {
		t.Helper()
		_, args, err := explain(t, d, s, domain.PageRequest[*rt.Widget]{})
		if err != nil {
			t.Fatal(err)
		}
		if b, ok := wantID.([]byte); ok {
			if string(args[0].([]byte)) != string(b) {
				t.Fatalf("%s: id arg %x, want %x", d.Name(), args[0], b)
			}
		} else if args[0] != wantID {
			t.Fatalf("%s: id arg %#v, want %#v", d.Name(), args[0], wantID)
		}
		if args[1] != wantBool {
			t.Fatalf("%s: bool arg %#v, want %#v", d.Name(), args[1], wantBool)
		}
	}
	check(sqlite.New(), id.String(), int64(1))
	check(postgres.New(), id.String(), true)
	check(sqlserver.New(), id.String(), true)
	check(mysql.New(), id.String(), true)
	check(oracle.New(), id.Bytes(), int64(1))
	swapped := sqlrepo.SwapGUIDBytes([16]byte(id.UUID))
	check(oracle.New(oracle.WithDotNetGUIDs()), swapped[:], int64(1))

	// Round trip of the .NET byte order.
	u, err := oracle.New(oracle.WithDotNetGUIDs()).ParseUUID(swapped[:])
	if err != nil || u != id.UUID {
		t.Fatalf(".NET GUID round trip failed: %v %v", u, err)
	}
	// SQL Server returns UNIQUEIDENTIFIER bytes in mixed-endian order.
	u, err = sqlserver.New().ParseUUID(swapped[:])
	if err != nil || u != id.UUID {
		t.Fatalf("sqlserver GUID parse failed: %v %v", u, err)
	}
}

func TestExplain_OracleSplitsLongInLists(t *testing.T) {
	names := make([]string, 1500)
	for i := range names {
		names[i] = "n"
	}
	q, args, err := explain(t, oracle.New(), rt.FieldName.In(names...), domain.PageRequest[*rt.Widget]{})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 1500 || !strings.Contains(q, `:1000) OR t0."NAME" IN (:1001`) {
		t.Fatalf("IN list not split at 1000 items: %d args\n%s", len(args), q[:200])
	}
}

func TestExplain_UnmappedFieldAndCustomFail(t *testing.T) {
	unmapped := spec.Text[*rt.Widget]("nickname", (*rt.Widget).Name).Eq("x")
	if _, _, err := explain(t, postgres.New(), unmapped, domain.PageRequest[*rt.Widget]{}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("unmapped field must fail with ErrUnsupported, got %v", err)
	}
	if _, _, err := explain(t, postgres.New(), rt.Unknown(), domain.PageRequest[*rt.Widget]{}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("unknown custom spec must fail with ErrUnsupported, got %v", err)
	}
}

func TestMapping_RejectsUnsafeIdentifiers(t *testing.T) {
	m := sqlconformance.WidgetMapping()
	m.Columns = append(m.Columns, "name; DROP TABLE x")
	if _, err := sqlrepo.NewRepository(sqlrepo.New(nil, postgres.New()), m); err == nil {
		t.Fatal("expected invalid identifier to be rejected")
	}
}

func TestEscapeLike(t *testing.T) {
	if got := sqlrepo.EscapeLike(`50%_off[x]\`); got != `50\%\_off\[x]\\` {
		t.Fatalf("unexpected escape: %s", got)
	}
}

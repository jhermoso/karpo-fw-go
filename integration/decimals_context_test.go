package integration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	odomain "github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	oinfra "github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	ydomain "github.com/jhermoso/karpo-fw-go/contexts/payroll/domain"
	yinfra "github.com/jhermoso/karpo-fw-go/contexts/payroll/infrastructure"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
)

// oldText es cómo escribía cada columna decimal el código anterior a la migración (D-94), por
// "tabla.columna": .String() o StringFixed(n). Las que no están se escribían con .String().
var oldText = map[string]func(vocab.Decimal) string{
	"ord_orders.customer_discount": fixed(2),
	"ord_order_lines.unit_price":   fixed(4),
	"ord_order_lines.discount":     fixed(2),
	"ord_order_lines.net_price":    fixed(4),
	"ord_order_lines.amount":       fixed(2),
	"bil_invoices.net":             fixed(2),
	"bil_invoice_lines.net":        fixed(2),
}

func fixed(n int32) func(vocab.Decimal) string {
	return func(d vocab.Decimal) string { return d.StringFixed(n) }
}

// TestContextDecimalColumns comprueba, en los cuatro motores servidores, la migración que pasa a
// {dec:p,s} las columnas decimales que los contextos guardaban como texto (D-94), con datos: un
// pedido de Orders (importes, precios, cantidades y descuentos), una factura rectificativa de
// Billing (cantidad e importes negativos) y un perfil de Payroll (tipo de IRPF, salario y un
// reparto) se escriben como texto con el esquema anterior, tal como lo escribía el código de
// entonces; se migra, el catálogo del motor dice que cada columna es numérica con su (p,s) y
// conserva su NOT NULL, y los agregados se releen iguales, se vuelven a guardar y se releen otra
// vez. Como cambia tipos de columna en el mismo proceso, PostgreSQL se abre como el proceso que
// migra: sin caché de sentencias.
func TestContextDecimalColumns(t *testing.T) {
	for _, e := range engines {
		if e.name == "oracle-dotnet-guids" {
			continue
		}
		var adjust func(string) string
		if e.name == "postgres" {
			adjust = postgres.WithoutStatementCache
		}
		t.Run(e.name, func(t *testing.T) {
			db := openWith(t, e, adjust)
			ctx := context.Background()
			oinfra.DropAll(ctx, db)
			binfra.DropAll(ctx, db)
			yinfra.DropAll(ctx, db)
			sets := []sqlrepo.MigrationSet{oinfra.Migrations(), binfra.Migrations(), yinfra.Migrations()}
			before := make([]sqlrepo.MigrationSet, len(sets))
			for i, s := range sets {
				last := s.Migrations[len(s.Migrations)-1]
				if last.Name != "decimal columns" {
					t.Fatalf("%s: the last migration is %q, not the decimal columns", s.Context, last.Name)
				}
				before[i] = sqlrepo.MigrationSet{Context: s.Context, Migrations: s.Migrations[:len(s.Migrations)-1]}
			}
			migrate(t, ctx, db, before)

			d := vocab.MustDecimal
			orderID := odomain.NewOrderID()
			order, err := odomain.ReconstituteOrder(orderID, odomain.OrderState{Company: odomain.OrganizationID{UUID: fw.NewUUID()},
				Customer: odomain.PartyID{UUID: fw.NewUUID()}, Number: "P-2026-000001", Date: vocab.MustDate(2026, time.October, 21),
				CustomerDiscount: d("5.25"), Status: odomain.Confirmed, Lines: []odomain.Line{
					{No: 1, Product: odomain.ProductID{UUID: fw.NewUUID()}, SKU: "A-1", Description: "Tornillo", UoM: "u", TaxCode: "G", Stocked: true,
						Quantity: d("12345678901234.5678"), UnitPrice: d("0.0125"), Discount: d("12.5"), NetPrice: d("0.0104"), Amount: d("128395061.37"),
						Reserved: d("0.0001"), Delivered: d("2")},
					{No: 2, Product: odomain.ProductID{UUID: fw.NewUUID()}, SKU: "B-2", Description: "Servicio", UoM: "h",
						Quantity: d("1"), UnitPrice: d("99999999999999.9999"), Discount: d("0"), NetPrice: d("94749999999999.9999"),
						Amount: d("94750000000000.00"), Reserved: d("0"), Delivered: d("0")}}})
			if err != nil {
				t.Fatal(err)
			}
			insertAsText(t, ctx, db, oinfra.OrderMapping(), orderID.UUID, order)

			seller, customer := bdomain.OrganizationID{UUID: fw.NewUUID()}, bdomain.PartyID{UUID: fw.NewUUID()}
			originalID := bdomain.NewInvoiceID()
			original, err := bdomain.ReconstituteInvoice(originalID, bdomain.InvoiceState{Seller: seller, Customer: customer, Kind: bdomain.Ordinary,
				Status: bdomain.Draft, Currency: vocab.MustCurrencyCode("EUR"), Lines: []bdomain.Line{
					{ID: bdomain.LineID{UUID: fw.NewUUID()}, Description: "Venta", Quantity: d("2.5"), UnitPrice: d("10.1234"), TaxCode: "G21",
						Net: d("25.31")}}})
			if err != nil {
				t.Fatal(err)
			}
			insertAsText(t, ctx, db, binfra.InvoiceMapping(), originalID.UUID, original)
			invoiceID := bdomain.NewInvoiceID()
			invoice, err := bdomain.ReconstituteInvoice(invoiceID, bdomain.InvoiceState{Seller: seller, Customer: customer, Kind: bdomain.Corrective,
				Status: bdomain.Draft, Currency: vocab.MustCurrencyCode("EUR"), Corrects: originalID, Reason: bdomain.CorrectionReasons[0],
				Lines: []bdomain.Line{
					{ID: bdomain.LineID{UUID: fw.NewUUID()}, Description: "Abono", Quantity: d("-2.5"), UnitPrice: d("10.1234"), Discount: d("7.125"),
						TaxCode: "G21", Net: d("-23.51")}}})
			if err != nil {
				t.Fatal(err)
			}
			insertAsText(t, ctx, db, binfra.InvoiceMapping(), invoiceID.UUID, invoice)

			profileID := ydomain.ProfileID{UUID: fw.NewUUID()}
			iban, err := vocab.NewIBAN("ES7921000813610123456789")
			if err != nil {
				t.Fatal(err)
			}
			profile, err := ydomain.ReconstituteProfile(profileID, ydomain.ProfileState{Employment: ydomain.EmploymentID{UUID: fw.NewUUID()},
				Person: ydomain.PersonID{UUID: fw.NewUUID()}, Employer: ydomain.OrganizationID{UUID: fw.NewUUID()}, ContributionGroup: 3,
				IncomeTaxRate: d("15.25"), Salary: ydomain.Salary{Amount: d("2100.5"), Periodicity: ydomain.Monthly, PaymentsPerYear: 14},
				Splits: []ydomain.Split{{ID: ydomain.SplitID{UUID: fw.NewUUID()}, IBAN: iban, Percent: d("33.33"), Priority: 1,
					From: vocab.MustDate(2026, time.January, 1)}}})
			if err != nil {
				t.Fatal(err)
			}
			insertAsText(t, ctx, db, yinfra.ProfileMapping(), profileID.UUID, profile)

			columns := map[string]map[string]sqlrepo.DecimalColumn{}
			for _, m := range []map[string]map[string]sqlrepo.DecimalColumn{decimalsOf(oinfra.OrderMapping()), decimalsOf(binfra.InvoiceMapping()),
				decimalsOf(yinfra.ProfileMapping())} {
				for table, cols := range m {
					columns[table] = cols
				}
			}
			nullable := map[string]bool{}
			for table, cols := range columns {
				for c := range cols {
					nullable[table+"."+c] = catalog(t, ctx, db, table, c).nullable
				}
			}

			migrate(t, ctx, db, sets)

			for table, cols := range columns {
				for c, dc := range cols {
					got := catalog(t, ctx, db, table, c)
					if !got.numeric || got.precision != dc.Precision || got.scale != dc.Scale {
						t.Errorf("%s.%s is %s(%d,%d), want a numeric %s", table, c, got.typ, got.precision, got.scale, dc)
					}
					if got.nullable != nullable[table+"."+c] {
						t.Errorf("%s.%s: nullable %v, before the migration %v", table, c, got.nullable, nullable[table+"."+c])
					}
				}
			}
			roundTrip(t, ctx, db, oinfra.OrderMapping(), orderID, order)
			roundTrip(t, ctx, db, binfra.InvoiceMapping(), invoiceID, invoice)
			roundTrip(t, ctx, db, yinfra.ProfileMapping(), profileID, profile)
		})
	}
}

func migrate(t *testing.T, ctx context.Context, db *sqlrepo.DB, sets []sqlrepo.MigrationSet) {
	t.Helper()
	m, err := sqlrepo.NewMigrator(db, sets)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

// roundTrip relee el agregado migrado y lo compara con el original; después lo vuelve a guardar,
// ya con las columnas decimales, y lo compara otra vez.
func roundTrip[ID fw.Identifier, T fw.AggregateRoot[ID]](t *testing.T, ctx context.Context, db *sqlrepo.DB, m sqlrepo.Mapping[ID, T], id ID, want T) {
	t.Helper()
	repo, err := sqlrepo.NewRepository(db, m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sameRows(t, m, want, got)
	if err := repo.Save(ctx, got); err != nil {
		t.Fatalf("%s: save after the migration: %v", m.Table, err)
	}
	again, err := repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	sameRows(t, m, want, again)
}

// decimalsOf devuelve las columnas decimales de un mapeo y de sus hijos, por tabla.
func decimalsOf[ID fw.Identifier, T fw.AggregateRoot[ID]](m sqlrepo.Mapping[ID, T]) map[string]map[string]sqlrepo.DecimalColumn {
	out := map[string]map[string]sqlrepo.DecimalColumn{m.Table: m.Decimals}
	for _, c := range m.Children {
		out[c.Table] = c.Decimals
	}
	return out
}

// column es lo que dice el catálogo del motor de una columna.
type column struct {
	typ              string
	numeric          bool
	precision, scale int
	nullable         bool
}

// catalog lee el tipo, la precisión, la escala y si admite nulos de una columna en el catálogo del
// motor.
func catalog(t *testing.T, ctx context.Context, db *sqlrepo.DB, table, name string) column {
	t.Helper()
	var q string
	switch db.Dialect().Name() {
	case "postgres":
		q = `SELECT data_type, COALESCE(numeric_precision, 0), COALESCE(numeric_scale, 0), is_nullable FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`
	case "sqlserver":
		q = `SELECT DATA_TYPE, COALESCE(NUMERIC_PRECISION, 0), COALESCE(NUMERIC_SCALE, 0), IS_NULLABLE FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_NAME = @p1 AND COLUMN_NAME = @p2`
	case "mysql":
		q = `SELECT DATA_TYPE, COALESCE(NUMERIC_PRECISION, 0), COALESCE(NUMERIC_SCALE, 0), IS_NULLABLE FROM information_schema.columns
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`
	case "oracle":
		q = `SELECT data_type, NVL(data_precision, 0), NVL(data_scale, 0), nullable FROM user_tab_columns WHERE table_name = :1 AND column_name = :2`
		table, name = strings.ToUpper(table), strings.ToUpper(name)
	default:
		t.Fatalf("no catalog query for %s", db.Dialect().Name())
	}
	rows, err := db.QueryContext(ctx, q, table, name)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("%s.%s is not in the catalog (%v)", table, name, rows.Err())
	}
	var typ, nullable string
	var p, s sql.NullInt64
	if err := rows.Scan(&typ, &p, &s, &nullable); err != nil {
		t.Fatal(err)
	}
	typ = strings.ToLower(typ)
	return column{typ: typ, numeric: typ == "numeric" || typ == "decimal" || typ == "number", precision: int(p.Int64), scale: int(s.Int64),
		nullable: nullable == "YES" || nullable == "Y"}
}

// insertAsText escribe un agregado y sus hijos como lo hacían los mapeos antes de la migración:
// cada decimal, como texto con .String() o con StringFixed(n), según oldText.
func insertAsText[ID fw.Identifier, T fw.AggregateRoot[ID]](t *testing.T, ctx context.Context, db *sqlrepo.DB, m sqlrepo.Mapping[ID, T], id fw.UUID,
	agg T) {
	t.Helper()
	text := func(table string, v sqlrepo.Values) {
		for c, x := range v {
			if dec, ok := x.(vocab.Decimal); ok {
				if f, ok := oldText[table+"."+c]; ok {
					v[c] = f(dec)
				} else {
					v[c] = dec.String()
				}
			}
		}
	}
	v, err := m.Dehydrate(agg)
	if err != nil {
		t.Fatal(err)
	}
	v["id"], v["version"] = id, int64(1)
	text(m.Table, v)
	if err := db.Insert(ctx, m.Table, v); err != nil {
		t.Fatal(err)
	}
	for _, c := range m.Children {
		rows, err := c.Dehydrate(agg)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			r[c.ForeignKey] = id
			text(c.Table, r)
			if err := db.Insert(ctx, c.Table, r); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// sameRows comprueba que dos agregados dan las mismas filas: los decimales, por valor. No mira la
// auditoría, que el repositorio puede sellar al guardar.
func sameRows[ID fw.Identifier, T fw.AggregateRoot[ID]](t *testing.T, m sqlrepo.Mapping[ID, T], want, got T) {
	t.Helper()
	same := func(table string, w, g sqlrepo.Values) {
		for c, x := range w {
			if strings.HasPrefix(c, "created_") || strings.HasPrefix(c, "modified_") {
				continue
			}
			if dw, ok := x.(vocab.Decimal); ok {
				if dg, ok := g[c].(vocab.Decimal); !ok || !dg.Equal(dw) {
					t.Errorf("%s.%s = %v, want %s", table, c, g[c], dw)
				}
			} else if fmt.Sprint(x) != fmt.Sprint(g[c]) {
				t.Errorf("%s.%s = %v, want %v", table, c, g[c], x)
			}
		}
	}
	w, err := m.Dehydrate(want)
	if err != nil {
		t.Fatal(err)
	}
	g, err := m.Dehydrate(got)
	if err != nil {
		t.Fatal(err)
	}
	same(m.Table, w, g)
	for _, c := range m.Children {
		wr, err := c.Dehydrate(want)
		if err != nil {
			t.Fatal(err)
		}
		gr, err := c.Dehydrate(got)
		if err != nil {
			t.Fatal(err)
		}
		if len(wr) != len(gr) {
			t.Fatalf("%s: %d rows, want %d", c.Table, len(gr), len(wr))
		}
		for i := range wr {
			same(c.Table, wr[i], gr[i])
		}
	}
}

package infrastructure_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/orders/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/orders/infrastructure"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// migration8 devuelve el relleno de la migración 8 (el descuento encadenado, D-149).
func migration8(all sqlrepo.MigrationSet) func(context.Context, *sqlrepo.DB) error {
	for _, m := range all.Migrations {
		if m.Version == 8 {
			return m.Run
		}
	}
	panic("there is no migration 8")
}

// TestDecimalColumnsInSQLite comprueba la migración 5 (D-94) en SQLite, que no está entre los
// motores de integration: {dec} sigue siendo TEXT, y la migración reescribe en forma canónica el
// texto que escribía el código anterior ("5.00" con StringFixed(2) pasa a "5", "12.5000" con
// StringFixed(4) a "12.5"), para que la clave de orden que calcula SQLite coincida con la de Go. El
// pedido se relee igual, se vuelve a guardar y se relee otra vez. También la migración 8 (D-149):
// las líneas del pedido y del presupuesto toman su descuento encadenado y las del albarán, su precio
// neto como precio y descuento cero; los borradores (pedido y presupuesto) recalculan su importe con
// la regla nueva (926,30 → 926,25) y lo confirmado o enviado lo conserva. Una línea sin descuento
// encadenado (NULL) se lee con el de la línea y el del cliente, no con cero.
func TestDecimalColumnsInSQLite(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "orders.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	all := infrastructure.Migrations()
	// La de las columnas decimales ya no es la última (después llegó el motivo del bloqueo, D-122).
	k := slices.IndexFunc(all.Migrations, func(m sqlrepo.Migration) bool { return m.Name == "decimal columns" })
	if k < 0 {
		t.Fatal("there is no decimal columns migration")
	}
	migrate := func(set sqlrepo.MigrationSet) {
		t.Helper()
		m, err := sqlrepo.NewMigrator(db, []sqlrepo.MigrationSet{set})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	migrate(sqlrepo.MigrationSet{Context: all.Context, Migrations: all.Migrations[:k]})

	// El texto de antes, como lo escribía OrderMapping.
	id := fw.NewUUID()
	audit := sqlrepo.Values{"created_at": nil, "created_by_id": nil, "created_by_name": nil, "modified_at": nil, "modified_by_id": nil,
		"modified_by_name": nil}
	order := sqlrepo.Values{"id": id, "version": int64(1), "company": fw.NewUUID(), "customer": fw.NewUUID(), "order_number": "P-1",
		"order_date": vocab.MustDate(2026, time.October, 21), "customer_discount": "5.00", "status": int64(domain.Confirmed)}
	for k, v := range audit {
		order[k] = v
	}
	if err := db.Insert(ctx, "ord_orders", order); err != nil {
		t.Fatal(err)
	}
	for _, l := range []sqlrepo.Values{
		{"line_no": int64(1), "quantity": "10", "unit_price": "12.5000", "discount": "0.00", "net_price": "11.8750", "amount": "118.75", "reserved": "0",
			"delivered": "2"},
		{"line_no": int64(2), "quantity": "9.5", "unit_price": "100.0000", "discount": "12.50", "net_price": "83.1250", "amount": "789.69",
			"reserved": "1.25", "delivered": "0"},
	} {
		l["order_id"], l["product"], l["sku"], l["description"], l["uom"], l["stocked"] = id, fw.NewUUID(), "A", "Tornillo", "u", true
		if err := db.Insert(ctx, "ord_order_lines", l); err != nil {
			t.Fatal(err)
		}
	}
	delivery := sqlrepo.Values{"id": fw.NewUUID(), "version": int64(1), "company": order["company"], "customer": order["customer"], "order_id": id,
		"order_number": "P-1", "delivery_number": "A-1", "delivered_on": vocab.MustDate(2026, time.October, 22)}
	for k, v := range audit {
		delivery[k] = v
	}
	if err := db.Insert(ctx, "ord_deliveries", delivery); err != nil {
		t.Fatal(err)
	}
	if err := db.Insert(ctx, "ord_delivery_lines", sqlrepo.Values{"delivery_id": delivery["id"], "line_no": int64(1), "product": fw.NewUUID(),
		"sku": "A", "description": "Tornillo", "uom": "u", "stocked": true, "quantity": "2", "net_price": "11.8750", "amount": "23.75"}); err != nil {
		t.Fatal(err)
	}

	// Un pedido en borrador y otro confirmado, y un presupuesto en borrador y otro enviado, todos de
	// 1000 × 1 € con un 5 % y un 2,5 %, con el importe de antes (0,9263 × 1000 = 926,30).
	old := map[string]fw.UUID{}
	for name, status := range map[string]domain.Status{"draft": domain.Draft, "confirmed": domain.Confirmed} {
		oid := fw.NewUUID()
		old["order "+name] = oid
		head := sqlrepo.Values{"id": oid, "version": int64(1), "company": fw.NewUUID(), "customer": fw.NewUUID(),
			"order_date": vocab.MustDate(2026, time.October, 21), "customer_discount": "2.50", "status": int64(status)}
		for k, v := range audit {
			head[k] = v
		}
		if err := db.Insert(ctx, "ord_orders", head); err != nil {
			t.Fatal(err)
		}
		if err := db.Insert(ctx, "ord_order_lines", sqlrepo.Values{"order_id": oid, "line_no": int64(1), "product": fw.NewUUID(), "sku": "A",
			"description": "Montaje", "uom": "h", "stocked": false, "quantity": "1000", "unit_price": "1.0000", "discount": "5.00", "net_price": "0.9263",
			"amount": "926.30", "reserved": "0", "delivered": "0"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"draft", "sent"} {
		qid := fw.NewUUID()
		old["quote "+status] = qid
		head := sqlrepo.Values{"id": qid, "version": int64(1), "company": fw.NewUUID(), "customer": fw.NewUUID(),
			"quote_date": vocab.MustDate(2026, time.October, 21), "valid_until": vocab.MustDate(2026, time.November, 20), "customer_discount": "2.50",
			"status": status}
		if status == "sent" {
			head["quote_number"] = "PRE-2026-000001"
		}
		for k, v := range audit {
			head[k] = v
		}
		if err := db.Insert(ctx, "ord_quotes", head); err != nil {
			t.Fatal(err)
		}
		if err := db.Insert(ctx, "ord_quote_lines", sqlrepo.Values{"quote_id": qid, "line_no": int64(1), "product": fw.NewUUID(), "sku": "A",
			"description": "Montaje", "uom": "h", "stocked": false, "quantity": "1000", "unit_price": "1.0000", "discount": "5.00", "net_price": "0.9263",
			"amount": "926.30"}); err != nil {
			t.Fatal(err)
		}
	}

	migrate(all)

	for table, key := range map[string]string{"ord_order_lines": "order_id", "ord_quote_lines": "quote_id"} {
		rows, err := db.Select(ctx, table, []string{key, "chained_discount", "amount"})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			id := r.UUID(key)
			want := ""
			switch id {
			case old["order draft"], old["quote draft"]:
				want = "926.25"
			case old["order confirmed"], old["quote sent"]:
				want = "926.3"
			default:
				continue
			}
			if !r.Decimal("chained_discount").Equal(vocab.MustDecimal("7.375")) || !r.Decimal("amount").Equal(vocab.MustDecimal(want)) {
				t.Errorf("%s %s: chained %s, amount %s, want 7.375 and %s", table, id, r.Decimal("chained_discount"), r.Decimal("amount"), want)
			}
		}
	}
	// Una línea sin descuento encadenado se lee con el de la línea y el del cliente.
	for table, key := range map[string]string{"ord_order_lines": "order_id", "ord_quote_lines": "quote_id"} {
		id := old["order draft"]
		if table == "ord_quote_lines" {
			id = old["quote draft"]
		}
		if _, err := db.Update(ctx, table, sqlrepo.Values{"chained_discount": nil}, sqlrepo.Values{key: id}); err != nil {
			t.Fatal(err)
		}
	}
	orders, err := sqlrepo.NewRepository(db, infrastructure.OrderMapping())
	if err != nil {
		t.Fatal(err)
	}
	if o, err := orders.Get(ctx, domain.OrderID{UUID: old["order draft"]}); err != nil || !o.State().Lines[0].ChainedDiscount.Equal(vocab.MustDecimal("7.375")) {
		t.Fatalf("an order line without its chained discount: %v %v", o, err)
	}
	quotes, err := sqlrepo.NewRepository(db, infrastructure.QuoteMapping())
	if err != nil {
		t.Fatal(err)
	}
	if q, err := quotes.Get(ctx, domain.QuoteID{UUID: old["quote draft"]}); err != nil || !q.State().Lines[0].ChainedDiscount.Equal(vocab.MustDecimal("7.375")) {
		t.Fatalf("a quote line without its chained discount: %v %v", q, err)
	}
	// La migración 8 es idempotente: otra pasada no toca lo que ya tiene descuento.
	if err := migration8(all)(ctx, db); err != nil {
		t.Fatal(err)
	}

	dl, err := db.Select(ctx, "ord_delivery_lines", []string{"unit_price", "discount", "net_price", "amount"})
	if err != nil || len(dl) != 1 || !dl[0].Decimal("unit_price").Equal(vocab.MustDecimal("11.875")) || !dl[0].Decimal("discount").IsZero() ||
		dl[0].IsNull("discount") || !dl[0].Decimal("amount").Equal(vocab.MustDecimal("23.75")) {
		t.Fatalf("delivery line after the chained discount migration: %v %v", dl, err)
	}

	all8, err := db.Select(ctx, "ord_order_lines", []string{"order_id", "unit_price", "discount", "net_price", "quantity"}, "line_no")
	if err != nil {
		t.Fatal(err)
	}
	var rows []*sqlrepo.Row
	for _, r := range all8 {
		if r.UUID("order_id") == id {
			rows = append(rows, r)
		}
	}
	want := [][]string{{"12.5", "0", "11.875", "10"}, {"100", "12.5", "83.125", "9.5"}}
	if len(rows) != len(want) {
		t.Fatalf("lines of the first order: %d", len(rows))
	}
	for i, r := range rows {
		for j, c := range []string{"unit_price", "discount", "net_price", "quantity"} {
			if got := r.String(c); got != want[i][j] {
				t.Errorf("line %d: %s = %q, want the canonical %q", i+1, c, got, want[i][j])
			}
		}
	}
	heads, err := db.Select(ctx, "ord_orders", []string{"id", "customer_discount"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range heads {
		if h.UUID("id") == id && h.String("customer_discount") != "5" {
			t.Fatalf("customer discount: %s", h.String("customer_discount"))
		}
	}

	repo, err := sqlrepo.NewRepository(db, infrastructure.OrderMapping())
	if err != nil {
		t.Fatal(err)
	}
	o, err := repo.Get(ctx, domain.OrderID{UUID: id})
	if err != nil {
		t.Fatal(err)
	}
	s := o.State()
	if !s.CustomerDiscount.Equal(vocab.MustDecimal("5")) || len(s.Lines) != 2 || !s.Lines[0].UnitPrice.Equal(vocab.MustDecimal("12.5")) ||
		!s.Lines[1].Quantity.Equal(vocab.MustDecimal("9.5")) || !s.Lines[1].Amount.Equal(vocab.MustDecimal("789.69")) ||
		!s.Lines[1].Reserved.Equal(vocab.MustDecimal("1.25")) {
		t.Fatalf("reread: %+v", s)
	}
	// 5 % del cliente sobre 0 % y sobre 12,5 % de la tarifa: 5 y 16,875 (no 17,5); el importe se queda.
	if !s.Lines[0].ChainedDiscount.Equal(vocab.MustDecimal("5")) || !s.Lines[1].ChainedDiscount.Equal(vocab.MustDecimal("16.875")) ||
		!s.Lines[0].Amount.Equal(vocab.MustDecimal("118.75")) {
		t.Fatalf("chained discounts filled by the migration: %+v", s.Lines)
	}
	if err := repo.Save(ctx, o); err != nil {
		t.Fatalf("save after the migration: %v", err)
	}
	if o, err = repo.Get(ctx, domain.OrderID{UUID: id}); err != nil || !o.State().Lines[0].NetPrice.Equal(vocab.MustDecimal("11.875")) {
		t.Fatalf("reread after saving: %v", err)
	}
}

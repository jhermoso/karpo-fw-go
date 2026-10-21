package infrastructure_test

import (
	"context"
	"database/sql"
	"path/filepath"
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

// TestDecimalColumnsInSQLite comprueba la migración 5 (D-94) en SQLite, que no está entre los
// motores de integration: {dec} sigue siendo TEXT, y la migración reescribe en forma canónica el
// texto que escribía el código anterior ("5.00" con StringFixed(2) pasa a "5", "12.5000" con
// StringFixed(4) a "12.5"), para que la clave de orden que calcula SQLite coincida con la de Go. El
// pedido se relee igual, se vuelve a guardar y se relee otra vez.
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
	last := all.Migrations[len(all.Migrations)-1]
	if last.Name != "decimal columns" {
		t.Fatalf("the last migration is %q", last.Name)
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
	migrate(sqlrepo.MigrationSet{Context: all.Context, Migrations: all.Migrations[:len(all.Migrations)-1]})

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

	migrate(all)

	rows, err := db.Select(ctx, "ord_order_lines", []string{"unit_price", "discount", "net_price", "quantity"}, "line_no")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"12.5", "0", "11.875", "10"}, {"100", "12.5", "83.125", "9.5"}}
	for i, r := range rows {
		for j, c := range []string{"unit_price", "discount", "net_price", "quantity"} {
			if got := r.String(c); got != want[i][j] {
				t.Errorf("line %d: %s = %q, want the canonical %q", i+1, c, got, want[i][j])
			}
		}
	}
	head, err := db.Select(ctx, "ord_orders", []string{"customer_discount"})
	if err != nil || len(head) != 1 || head[0].String("customer_discount") != "5" {
		t.Fatalf("customer discount: %v %v", head, err)
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
	if err := repo.Save(ctx, o); err != nil {
		t.Fatalf("save after the migration: %v", err)
	}
	if o, err = repo.Get(ctx, domain.OrderID{UUID: id}); err != nil || !o.State().Lines[0].NetPrice.Equal(vocab.MustDecimal("11.875")) {
		t.Fatalf("reread after saving: %v", err)
	}
}

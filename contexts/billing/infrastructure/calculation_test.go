package infrastructure_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	"github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/sqlite"
)

// La migración 9 con datos en SQLite (los cuatro motores servidores, en integration): una factura
// guardada antes conserva el precio de sus líneas, también el de 14 cifras enteras, y queda con el
// cálculo de siempre (una vez al final, 4 decimales); después cabe un precio de 6 decimales.
func TestCalculationMigrationInSQLite(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "billing.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db := sqlite.Open(raw, sqlrepo.WithName("sqlite"))
	all := infrastructure.Migrations()
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
	if all.Migrations[8].Version != 9 {
		t.Fatal("migration 9 is not the ninth")
	}
	migrate(sqlrepo.MigrationSet{Context: all.Context, Migrations: all.Migrations[:8]})

	id := fw.NewUUID()
	if err := db.Insert(ctx, "bil_invoices", sqlrepo.Values{"id": id, "version": int64(1), "seller": fw.NewUUID(), "customer": fw.NewUUID(),
		"kind": int64(domain.Ordinary), "status": int64(domain.Draft), "currency": "EUR", "equivalence_surcharge": false, "net": "1246.91"}); err != nil {
		t.Fatal(err)
	}
	prices := map[string]string{"12.3456": "1234.56", "99999999999999.9999": "10"}
	for price, net := range prices {
		if err := db.Insert(ctx, "bil_invoice_lines", sqlrepo.Values{"id": fw.NewUUID(), "invoice_id": id, "description": "Línea", "quantity": "1",
			"unit_price": price, "tax_code": "G21", "net": net}); err != nil {
			t.Fatal(err)
		}
	}

	migrate(all)

	repo, err := infrastructure.InvoiceRepositoryFactory(db)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := repo.Get(ctx, domain.InvoiceID{UUID: id})
	if err != nil {
		t.Fatal(err)
	}
	st := inv.State()
	if st.Calculation != domain.DefaultCalculation() || len(st.Lines) != 2 {
		t.Fatalf("after the migration: %+v", st)
	}
	for _, l := range st.Lines {
		if _, ok := prices[l.UnitPrice.String()]; !ok {
			t.Fatalf("a price changed: %s", l.UnitPrice)
		}
	}
	draft, err := domain.DraftInvoice(domain.NewInvoiceID(), domain.InvoiceState{Seller: st.Seller, Customer: st.Customer, Kind: domain.Ordinary,
		Calculation: domain.Calculation{Rounding: domain.RoundOnce, PriceDecimals: 6}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.AddLine(domain.LineInput{Description: "Kilovatio", Quantity: vocab.MustDecimal("1000"), UnitPrice: vocab.MustDecimal("0.123456"),
		TaxCode: "G21"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, draft); err != nil {
		t.Fatalf("a price of 6 decimals after the migration: %v", err)
	}
	back, err := repo.Get(ctx, draft.ID())
	if err != nil || back.State().Lines[0].UnitPrice.String() != "0.123456" || back.State().Calculation.PriceDecimals != 6 {
		t.Fatalf("read back: %+v %v", back, err)
	}
}

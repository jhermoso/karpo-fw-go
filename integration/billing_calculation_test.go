package integration

import (
	"context"
	"testing"

	bdomain "github.com/jhermoso/karpo-fw-go/contexts/billing/domain"
	binfra "github.com/jhermoso/karpo-fw-go/contexts/billing/infrastructure"
	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"
	"github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo/postgres"
)

// TestBillingCalculationMigration comprueba la migración 9 de Billing (D-144) con datos, en los
// cuatro motores: una factura escrita con el esquema de la migración 8 (unit_price (18,4), uno de
// sus precios con las 14 cifras enteras) conserva sus precios cuando unit_price pasa a (20,6) (en
// Oracle, ampliando una columna NUMBER con datos), queda con el cálculo de siempre, el catálogo
// dice (20,6) y NOT NULL, y después cabe un precio de 6 decimales.
func TestBillingCalculationMigration(t *testing.T) {
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
			binfra.DropAll(ctx, db)
			all := binfra.Migrations()
			if all.Migrations[8].Version != 9 {
				t.Fatal("migration 9 is not the ninth")
			}
			migrate(t, ctx, db, []sqlrepo.MigrationSet{{Context: all.Context, Migrations: all.Migrations[:8]}})
			nullable := catalog(t, ctx, db, "bil_invoice_lines", "unit_price").nullable

			d := vocab.MustDecimal
			id := bdomain.NewInvoiceID()
			inv, err := bdomain.ReconstituteInvoice(id, bdomain.InvoiceState{Seller: bdomain.OrganizationID{UUID: fw.NewUUID()},
				Customer: bdomain.PartyID{UUID: fw.NewUUID()}, Kind: bdomain.Ordinary, Status: bdomain.Draft, Currency: vocab.MustCurrencyCode("EUR"),
				Lines: []bdomain.Line{
					{ID: bdomain.LineID{UUID: fw.NewUUID()}, Description: "Tornillo", Quantity: d("2.5"), UnitPrice: d("10.1234"), TaxCode: "G21", Net: d("25.31")},
					{ID: bdomain.LineID{UUID: fw.NewUUID()}, Description: "Barco", Quantity: d("0.0001"), UnitPrice: d("99999999999999.9999"), TaxCode: "G21",
						Net: d("10000000000")},
				}})
			if err != nil {
				t.Fatal(err)
			}
			insertAsText(t, ctx, db, binfra.InvoiceMapping(), id.UUID, inv)

			migrate(t, ctx, db, []sqlrepo.MigrationSet{all})

			got := catalog(t, ctx, db, "bil_invoice_lines", "unit_price")
			if !got.numeric || got.precision != 20 || got.scale != 6 || got.nullable != nullable {
				t.Fatalf("unit_price after the migration: %+v", got)
			}
			roundTrip(t, ctx, db, binfra.InvoiceMapping(), id, inv) // con el cálculo de siempre: once y 4

			repo, err := binfra.InvoiceRepositoryFactory(db)
			if err != nil {
				t.Fatal(err)
			}
			draft, err := bdomain.DraftInvoice(bdomain.NewInvoiceID(), bdomain.InvoiceState{Seller: bdomain.OrganizationID{UUID: fw.NewUUID()},
				Customer: bdomain.PartyID{UUID: fw.NewUUID()}, Kind: bdomain.Ordinary,
				Calculation: bdomain.Calculation{Rounding: bdomain.RoundGrossThenDiscount, PriceDecimals: 6}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := draft.AddLine(bdomain.LineInput{Description: "Kilovatio", Quantity: d("1000"), UnitPrice: d("0.123456"), TaxCode: "G21"}); err != nil {
				t.Fatal(err)
			}
			if err := repo.Save(ctx, draft); err != nil {
				t.Fatalf("a price of 6 decimals: %v", err)
			}
			back, err := repo.Get(ctx, draft.ID())
			if err != nil || back.State().Lines[0].UnitPrice.String() != "0.123456" || back.State().Calculation != draft.State().Calculation {
				t.Fatalf("read back: %+v %v", back, err)
			}
		})
	}
}
